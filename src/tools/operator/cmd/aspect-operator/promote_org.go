package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/jackc/pgx/v5"
	opch "github.com/verself/operator-runtime/clickhouse"
	oppg "github.com/verself/operator-runtime/postgres"
	opruntime "github.com/verself/operator-runtime/runtime"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// promote-org flips an existing organization's billing trust tier. The org is
// created like any other (normal signup); promotion to the privileged
// "platform" tier is the one and only special, operator-gated step. Trust tier
// lives in billing-service Postgres; the DB enforces a single platform-tier
// holder via a partial unique index, so a second promotion fails loudly.
//
// Scope is deliberately narrow: this sets trust_tier and nothing else. It does
// not touch overage policy, identity, or SpiceDB policy — those are owned by
// signup and billing on their own boundaries.

var promoteOrgTrustTiers = map[string]struct{}{
	"new":         {},
	"established": {},
	"enterprise":  {},
	"platform":    {},
}

type promoteOrgOptions struct {
	operatorRuntimeOptions
	secretVarsFile string
	pgUser         string
	pgRemotePort   int
	slug           string
	tier           string
	dryRun         bool
}

func cmdPromoteOrg(args []string) error {
	fs := flagSet("promote-org")
	opts := &promoteOrgOptions{tier: "platform"}
	addOperatorRuntimeFlags(&opts.operatorRuntimeOptions)
	fs.StringVar(&opts.site, "site", opts.site, "Deploy site")
	fs.StringVar(&opts.repoRoot, "repo-root", "", "verself-sh checkout root (defaults to cwd)")
	fs.StringVar(&opts.secretVarsFile, "secret-vars-file", os.Getenv("VERSELF_SITE_SECRET_VARS_FILE"), "generated site secret vars file")
	fs.StringVar(&opts.pgUser, "pg-user", envOr("PG_USER", oppg.DefaultUser), "PostgreSQL user")
	fs.IntVar(&opts.pgRemotePort, "pg-remote-port", envIntOr("PG_PORT", oppg.DefaultPort), "Remote PostgreSQL port on the worker loopback")
	fs.StringVar(&opts.slug, "slug", "", "Organization slug to promote")
	fs.StringVar(&opts.tier, "tier", opts.tier, "Target trust tier (new|established|enterprise|platform)")
	fs.BoolVar(&opts.dryRun, "dry-run", false, "Resolve and report the change without writing")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("promote-org: unexpected positional args: %s", strings.Join(fs.Args(), " "))
	}
	opts.slug = strings.TrimSpace(opts.slug)
	opts.tier = strings.TrimSpace(opts.tier)
	if opts.slug == "" {
		return fmt.Errorf("promote-org: --slug is required")
	}
	if _, ok := promoteOrgTrustTiers[opts.tier]; !ok {
		return fmt.Errorf("promote-org: --tier must be one of new|established|enterprise|platform, got %q", opts.tier)
	}
	return runOperatorRuntime("operator.promote_org", opts.operatorRuntimeOptions, 0, opch.Config{Database: "verself"}, func(rt *opruntime.Runtime, _ *opch.Client) error {
		return promoteOrg(rt, opts)
	})
}

func promoteOrg(rt *opruntime.Runtime, opts *promoteOrgOptions) (err error) {
	ctx, span := rt.Tracer.Start(rt.Ctx, "operator.promote_org", trace.WithAttributes(
		attribute.String("verself.org_slug", opts.slug),
		attribute.String("billing.trust_tier", opts.tier),
		attribute.Bool("operator.dry_run", opts.dryRun),
	))
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		} else {
			span.SetStatus(codes.Ok, "")
		}
		span.End()
	}()

	orgID, displayName, err := promoteResolveOrgID(ctx, rt, opts)
	if err != nil {
		return err
	}
	span.SetAttributes(attribute.String("verself.org_id", orgID))

	conn, err := promoteOpenPG(ctx, rt, opts, "billing")
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close(context.Background()) }()
	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("promote-org: billing begin: %w", err)
	}
	defer rollbackTx(ctx, tx)

	if opts.tier == "platform" {
		rows, qErr := tx.Query(ctx, `SELECT org_id FROM orgs WHERE trust_tier = 'platform' FOR UPDATE`)
		if qErr != nil {
			return fmt.Errorf("promote-org: query platform-tier holders: %w", qErr)
		}
		var holders []string
		for rows.Next() {
			var id string
			if scanErr := rows.Scan(&id); scanErr != nil {
				rows.Close()
				return scanErr
			}
			holders = append(holders, id)
		}
		rows.Close()
		if rows.Err() != nil {
			return fmt.Errorf("promote-org: scan platform-tier holders: %w", rows.Err())
		}
		for _, id := range holders {
			if id != orgID {
				return fmt.Errorf("promote-org: refusing to promote %s (%s); platform tier already held by %s", opts.slug, orgID, id)
			}
		}
	}

	var current string
	err = tx.QueryRow(ctx, `SELECT trust_tier FROM orgs WHERE org_id = $1`, orgID).Scan(&current)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		current = ""
	case err != nil:
		return fmt.Errorf("promote-org: read current trust tier: %w", err)
	}

	if current == opts.tier {
		_, _ = fmt.Fprintf(os.Stdout, "promote-org: %s (%s) already at trust_tier=%s\n", opts.slug, orgID, opts.tier)
		return nil
	}
	if opts.dryRun {
		_, _ = fmt.Fprintf(os.Stdout, "promote-org: DRY RUN %s (%s) trust_tier %q -> %q\n", opts.slug, orgID, current, opts.tier)
		return nil
	}

	if _, err = tx.Exec(ctx, `
INSERT INTO orgs (org_id, display_name, trust_tier)
VALUES ($1, $2, $3)
ON CONFLICT (org_id) DO UPDATE
SET trust_tier = EXCLUDED.trust_tier,
    updated_at = now()
WHERE orgs.trust_tier IS DISTINCT FROM EXCLUDED.trust_tier`,
		orgID, displayName, opts.tier); err != nil {
		return fmt.Errorf("promote-org: upsert trust tier: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("promote-org: commit: %w", err)
	}
	_, _ = fmt.Fprintf(os.Stdout, "promote-org: %s (%s) trust_tier %q -> %q\n", opts.slug, orgID, current, opts.tier)
	return nil
}

func promoteResolveOrgID(ctx context.Context, rt *opruntime.Runtime, opts *promoteOrgOptions) (orgID, displayName string, err error) {
	conn, err := promoteOpenPG(ctx, rt, opts, "iam_service")
	if err != nil {
		return "", "", err
	}
	defer func() { _ = conn.Close(context.Background()) }()
	err = conn.QueryRow(ctx, `
SELECT org_id, display_name
FROM iam_organizations
WHERE slug = $1 AND state = 'active'`, opts.slug).Scan(&orgID, &displayName)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", fmt.Errorf("promote-org: no active organization with slug %q", opts.slug)
	}
	if err != nil {
		return "", "", fmt.Errorf("promote-org: resolve org by slug: %w", err)
	}
	return orgID, displayName, nil
}

func promoteOpenPG(ctx context.Context, rt *opruntime.Runtime, opts *promoteOrgOptions, db string) (*pgx.Conn, error) {
	passwordPath := opts.secretVarsFile
	if passwordPath == "" {
		passwordPath = opruntime.SiteSecretVarsPath(rt.RepoRoot, rt.Site)
	}
	return oppg.OpenOverSSH(ctx, rt, oppg.Config{
		Database:     db,
		User:         opts.pgUser,
		RemotePort:   opts.pgRemotePort,
		PasswordPath: passwordPath,
	})
}

func rollbackTx(ctx context.Context, tx pgx.Tx) {
	_ = tx.Rollback(ctx)
}
