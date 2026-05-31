package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/verself/deployment-tools/internal/openbaoruntime"
	"github.com/verself/deployment-tools/internal/runtime"
)

const openBaoReconcileTokenEnv = "VERSELF_OPENBAO_RECONCILE_TOKEN"

func applyOpenBaoRuntime(ctx context.Context, rt *runtime.Runtime, plan *deployPlan) error {
	if len(plan.OpenBao.Secrets) == 0 {
		return nil
	}
	ctx, span := rt.Tracer.Start(ctx, "verself_deploy.openbao.apply",
		trace.WithAttributes(
			attribute.String("verself.site", plan.Site),
			attribute.Int("verself.openbao.runtime_secret_count", len(plan.OpenBao.Secrets)),
			attribute.Int("verself.openbao.nomad_role_count", len(plan.OpenBao.NomadRoles)),
		),
	)
	defer span.End()
	result, err := openbaoruntime.Apply(ctx, openbaoruntime.ApplyOptions{
		RepoRoot:      rt.RepoRoot,
		Site:          plan.Site,
		SSH:           rt.SSH,
		Tracer:        rt.Tracer,
		SpiffeSubject: "spiffe://" + plan.SiteModel.SpiffeTrustDomain + "/svc/secrets-service",
		SeedVarsPath:  filepath.Join(rt.RepoRoot, ".verself", "site-bootstrap", plan.Site, "ansible-secrets.json"),
		Token:         openBaoReconcileToken(),
	}, plan.OpenBao)
	span.SetAttributes(
		attribute.Int("verself.openbao.secrets_created", result.SecretsCreated),
		attribute.Int("verself.openbao.secrets_updated", result.SecretsUpdated),
		attribute.Int("verself.openbao.secrets_kept", result.SecretsKept),
		attribute.Int("verself.openbao.policies_written", result.PoliciesWritten),
		attribute.Int("verself.openbao.roles_written", result.RolesWritten),
	)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	span.AddEvent("verself.openbao.runtime_reconciled", trace.WithAttributes(
		attribute.String("verself.deploy_run_key", rt.Identity.RunKey()),
		attribute.String("verself.site", plan.Site),
		attribute.Int("verself.openbao.secrets_created", result.SecretsCreated),
		attribute.Int("verself.openbao.secrets_updated", result.SecretsUpdated),
		attribute.Int("verself.openbao.secrets_kept", result.SecretsKept),
		attribute.Int("verself.openbao.policies_written", result.PoliciesWritten),
		attribute.Int("verself.openbao.roles_written", result.RolesWritten),
	))
	span.SetStatus(codes.Ok, "")
	return nil
}

func openBaoReconcileToken() string {
	return strings.TrimSpace(os.Getenv(openBaoReconcileTokenEnv))
}
