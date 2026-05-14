package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/mail"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	opch "github.com/verself/operator-runtime/clickhouse"
	oppg "github.com/verself/operator-runtime/postgres"
	opruntime "github.com/verself/operator-runtime/runtime"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const (
	platformActor                                  = "system:platform-seed"
	platformManifestVersion                        = "verself.platform.v1"
	platformDefaultForgejoRemote                   = "127.0.0.1:3000"
	platformDefaultForgejoTokenPath                = "/etc/credstore/forgejo/automation-token"
	platformIAMBootstrapTarget                     = "//src/services/iam-service/cmd/iam-service:iam-service"
	platformIAMBootstrapBin                        = "bazel-bin/src/services/iam-service/cmd/iam-service/iam-service_/iam-service"
	platformIAMSpiceDBPresharedKeyPath             = "/etc/credstore/iam-service/spicedb-grpc-preshared-key"
	platformDefaultSandboxForgejoWebhookSecretPath = "/etc/credstore/sandbox-rental/forgejo-webhook-secret"
	platformDefaultZitadelRemote                   = "127.0.0.1:8085"
	platformDefaultZitadelAdminPATPath             = "/etc/zitadel/admin.pat"
	platformDefaultBranch                          = "main"
)

var (
	platformSlugRE        = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,78}[a-z0-9])?$`)
	platformPublicOrgIDRE = regexp.MustCompile(`^org_[0-9A-HJKMNP-TV-Z]{26}$`)
)

type platformOptions struct {
	operatorRuntimeOptions
	action              string
	format              string
	secretsFile         string
	pgUser              string
	pgRemotePort        int
	forgejoTokenPath    string
	forgejoWebhookPath  string
	forgejoRemoteAddr   string
	forgejoRepoPrivate  bool
	zitadelAdminPATPath string
	zitadelRemoteAddr   string
}

type platformMainVars struct {
	PlatformOrgID                string `yaml:"platform_org_id"`
	PlatformPublicOrgID          string `yaml:"platform_public_org_id"`
	SecretsServicePlatformOrgID  string `yaml:"secrets_service_platform_org_id"`
	PlatformOrganizationName     string `yaml:"platform_organization_name"`
	PlatformCompanySlug          string `yaml:"platform_company_slug"`
	PlatformCompanyDisplayName   string `yaml:"platform_company_display_name"`
	PlatformOwnerAlias           string `yaml:"platform_owner_alias"`
	PlatformOwnerName            string `yaml:"platform_owner_name"`
	PlatformOwnerEmail           string `yaml:"platform_owner_email"`
	PlatformRootOwnerEmail       string `yaml:"platform_root_owner_email"`
	PlatformTrustTier            string `yaml:"platform_trust_tier"`
	PlatformRepoSlug             string `yaml:"platform_repo_slug"`
	PlatformRepoDisplayName      string `yaml:"platform_repo_display_name"`
	PlatformRepoDescription      string `yaml:"platform_repo_description"`
	PlatformGitHubAccountID      int64  `yaml:"platform_github_account_id"`
	PlatformGitHubInstallationID int64  `yaml:"platform_github_installation_id"`
	PlatformGitHubRepositoryID   int64  `yaml:"platform_github_repository_id"`
	ForgejoDomain                string `yaml:"forgejo_domain"`
	ForgejoSubdomain             string `yaml:"forgejo_subdomain"`
	VerselfDomain                string `yaml:"verself_domain"`
	ZitadelDomain                string `yaml:"zitadel_domain"`
	ZitadelSubdomain             string `yaml:"zitadel_subdomain"`
}

type platformConfig struct {
	OrgIDText            string `json:"org_id"`
	PublicOrgIDText      string `json:"public_org_id"`
	OrganizationName     string `json:"organization_name"`
	CompanySlug          string `json:"company_slug"`
	CompanyDisplayName   string `json:"company_display_name"`
	OwnerAlias           string `json:"owner_alias"`
	OwnerName            string `json:"owner_name"`
	OwnerEmail           string `json:"owner_email"`
	RootOwnerEmail       string `json:"root_owner_email"`
	TrustTier            string `json:"trust_tier"`
	RepoSlug             string `json:"repo_slug"`
	RepoDisplayName      string `json:"repo_display_name"`
	RepoDescription      string `json:"repo_description"`
	GitHubAccountID      int64  `json:"github_account_id,omitempty"`
	GitHubInstallationID int64  `json:"github_installation_id,omitempty"`
	GitHubRepositoryID   int64  `json:"github_repository_id,omitempty"`
	ForgejoDomain        string `json:"forgejo_domain"`
	VerselfDomain        string `json:"verself_domain"`
	ZitadelHost          string `json:"zitadel_host"`
	CanonicalGitURL      string `json:"canonical_git_url"`
	ForgejoWebhookURL    string `json:"forgejo_webhook_url"`
}

type platformReport struct {
	Version         string                `json:"version"`
	Action          string                `json:"action"`
	Site            string                `json:"site"`
	Config          platformConfig        `json:"config"`
	ProjectID       string                `json:"project_id"`
	RepoID          string                `json:"repo_id"`
	BackendID       string                `json:"backend_id"`
	ForgejoRepoID   string                `json:"forgejo_repo_id"`
	Changed         []string              `json:"changed,omitempty"`
	BoundaryResults []platformBoundaryRow `json:"boundary_results"`
}

type platformBoundaryRow struct {
	Boundary string `json:"boundary"`
	Status   string `json:"status"`
	Detail   string `json:"detail,omitempty"`
}

type platformIssueError struct {
	issues []string
}

func (e platformIssueError) Error() string {
	return "platform check failed: " + strings.Join(e.issues, "; ")
}

type platformRunner struct {
	rt      *opruntime.Runtime
	opts    *platformOptions
	cfg     platformConfig
	changes []string
}

type platformIDs struct {
	ProjectID uuid.UUID
	RepoID    uuid.UUID
	BackendID uuid.UUID
}

type platformEnvironmentSpec struct {
	ID          uuid.UUID
	Slug        string
	DisplayName string
	Kind        string
}

type platformForgejoClient struct {
	BaseURL string
	Token   string
	Client  *http.Client
}

type platformForgejoOrg struct {
	UserName    string `json:"username"`
	FullName    string `json:"full_name"`
	Description string `json:"description"`
	Visibility  string `json:"visibility"`
}

type platformForgejoRepo struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	FullName      string `json:"full_name"`
	Description   string `json:"description"`
	DefaultBranch string `json:"default_branch"`
	Private       bool   `json:"private"`
}

type platformForgejoHook struct {
	ID     int64             `json:"id"`
	Active bool              `json:"active"`
	Config map[string]string `json:"config"`
}

type forgejoStatusError struct {
	Method string
	Path   string
	Status int
	Body   string
}

func (e forgejoStatusError) Error() string {
	return fmt.Sprintf("forgejo %s %s status %d: %s", e.Method, e.Path, e.Status, e.Body)
}

func cmdPlatform(args []string) error {
	fs := flagSet("platform")
	opts := &platformOptions{
		action:              "check",
		format:              "text",
		forgejoTokenPath:    platformDefaultForgejoTokenPath,
		forgejoWebhookPath:  platformDefaultSandboxForgejoWebhookSecretPath,
		forgejoRemoteAddr:   platformDefaultForgejoRemote,
		forgejoRepoPrivate:  true,
		zitadelAdminPATPath: platformDefaultZitadelAdminPATPath,
		zitadelRemoteAddr:   platformDefaultZitadelRemote,
	}
	addOperatorRuntimeFlags(&opts.operatorRuntimeOptions)
	fs.StringVar(&opts.action, "action", opts.action, "Action: check or seed")
	fs.StringVar(&opts.format, "format", opts.format, "Output format: text or json")
	fs.StringVar(&opts.site, "site", opts.site, "Deploy site")
	fs.StringVar(&opts.repoRoot, "repo-root", "", "verself-sh checkout root (defaults to cwd)")
	fs.StringVar(&opts.secretsFile, "secrets-file", os.Getenv("SOPS_SECRETS_FILE"), "SOPS secrets file")
	fs.StringVar(&opts.pgUser, "pg-user", envOr("PG_USER", oppg.DefaultUser), "PostgreSQL user")
	fs.IntVar(&opts.pgRemotePort, "pg-remote-port", envIntOr("PG_PORT", oppg.DefaultPort), "Remote PostgreSQL port on the worker loopback")
	fs.StringVar(&opts.forgejoTokenPath, "forgejo-token-path", opts.forgejoTokenPath, "Remote Forgejo automation token path")
	fs.StringVar(&opts.forgejoWebhookPath, "forgejo-webhook-secret-path", opts.forgejoWebhookPath, "Remote sandbox-rental Forgejo webhook secret path")
	fs.StringVar(&opts.forgejoRemoteAddr, "forgejo-remote-addr", opts.forgejoRemoteAddr, "Remote Forgejo HTTP address reached over SSH")
	fs.StringVar(&opts.zitadelAdminPATPath, "zitadel-admin-pat-path", opts.zitadelAdminPATPath, "Remote Zitadel admin PAT path")
	fs.StringVar(&opts.zitadelRemoteAddr, "zitadel-remote-addr", opts.zitadelRemoteAddr, "Remote Zitadel HTTP address reached over SSH")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("platform: unexpected positional args: %s", strings.Join(fs.Args(), " "))
	}
	if err := opts.validate(); err != nil {
		return err
	}
	command := "platform." + opts.action
	return runOperatorRuntime(command, opts.operatorRuntimeOptions, false, opch.Config{Database: "verself"}, func(rt *opruntime.Runtime, _ *opch.Client) error {
		cfg, err := loadPlatformConfig(rt.RepoRoot, rt.Site)
		if err != nil {
			return err
		}
		runner := &platformRunner{rt: rt, opts: opts, cfg: cfg}
		var report platformReport
		switch opts.action {
		case "check":
			report, err = runner.check()
		case "seed":
			report, err = runner.seed()
		default:
			return fmt.Errorf("platform: unsupported --action=%s", opts.action)
		}
		if err != nil {
			return err
		}
		return writePlatformReport(os.Stdout, opts.format, report)
	})
}

func (opts *platformOptions) validate() error {
	switch opts.action {
	case "check", "seed":
	default:
		return fmt.Errorf("platform: --action must be check or seed")
	}
	switch opts.format {
	case "text", "json":
	default:
		return fmt.Errorf("platform: --format must be text or json")
	}
	if opts.pgRemotePort <= 0 || opts.pgRemotePort > 65535 {
		return fmt.Errorf("platform: --pg-remote-port must be between 1 and 65535 (got %d)", opts.pgRemotePort)
	}
	if strings.TrimSpace(opts.forgejoTokenPath) == "" {
		return fmt.Errorf("platform: --forgejo-token-path is required")
	}
	if strings.TrimSpace(opts.forgejoWebhookPath) == "" {
		return fmt.Errorf("platform: --forgejo-webhook-secret-path is required")
	}
	if strings.TrimSpace(opts.forgejoRemoteAddr) == "" {
		return fmt.Errorf("platform: --forgejo-remote-addr is required")
	}
	if strings.TrimSpace(opts.zitadelAdminPATPath) == "" {
		return fmt.Errorf("platform: --zitadel-admin-pat-path is required")
	}
	if strings.TrimSpace(opts.zitadelRemoteAddr) == "" {
		return fmt.Errorf("platform: --zitadel-remote-addr is required")
	}
	return nil
}

func loadPlatformConfig(repoRoot, site string) (platformConfig, error) {
	mainPath := siteVarsPath(repoRoot, site)
	var mainVars platformMainVars
	if err := readYAMLFile(mainPath, &mainVars); err != nil {
		return platformConfig{}, err
	}
	cfg := platformConfig{
		OrgIDText:            firstNonEmpty(mainVars.PlatformOrgID, mainVars.SecretsServicePlatformOrgID),
		PublicOrgIDText:      strings.TrimSpace(mainVars.PlatformPublicOrgID),
		OrganizationName:     strings.TrimSpace(mainVars.PlatformOrganizationName),
		CompanySlug:          strings.TrimSpace(mainVars.PlatformCompanySlug),
		CompanyDisplayName:   strings.TrimSpace(mainVars.PlatformCompanyDisplayName),
		OwnerAlias:           strings.TrimSpace(mainVars.PlatformOwnerAlias),
		OwnerName:            strings.TrimSpace(mainVars.PlatformOwnerName),
		OwnerEmail:           strings.TrimSpace(mainVars.PlatformOwnerEmail),
		RootOwnerEmail:       strings.TrimSpace(mainVars.PlatformRootOwnerEmail),
		TrustTier:            strings.TrimSpace(mainVars.PlatformTrustTier),
		RepoSlug:             strings.TrimSpace(mainVars.PlatformRepoSlug),
		RepoDisplayName:      strings.TrimSpace(mainVars.PlatformRepoDisplayName),
		RepoDescription:      strings.TrimSpace(mainVars.PlatformRepoDescription),
		GitHubAccountID:      mainVars.PlatformGitHubAccountID,
		GitHubInstallationID: mainVars.PlatformGitHubInstallationID,
		GitHubRepositoryID:   mainVars.PlatformGitHubRepositoryID,
		ForgejoDomain:        resolveForgejoDomain(mainVars),
		VerselfDomain:        strings.TrimSpace(mainVars.VerselfDomain),
		ZitadelHost:          resolveZitadelHost(mainVars),
		ForgejoWebhookURL:    resolveForgejoWebhookURL(mainVars),
	}
	if err := cfg.validate(); err != nil {
		return platformConfig{}, err
	}
	cfg.CanonicalGitURL = "https://" + cfg.ForgejoDomain + "/" + cfg.CompanySlug + "/" + cfg.RepoSlug + ".git"
	return cfg, nil
}

func (cfg *platformConfig) validate() error {
	cfg.OrgIDText = strings.TrimSpace(cfg.OrgIDText)
	if cfg.OrgIDText == "" {
		return fmt.Errorf("platform config: platform_org_id is required")
	}
	if cfg.PublicOrgIDText == "" {
		return fmt.Errorf("platform config: platform_public_org_id is required")
	}
	if !platformPublicOrgIDRE.MatchString(cfg.PublicOrgIDText) {
		return fmt.Errorf("platform config: platform_public_org_id must match %s", platformPublicOrgIDRE.String())
	}
	if !platformSlugRE.MatchString(cfg.CompanySlug) {
		return fmt.Errorf("platform config: platform_company_slug must match %s", platformSlugRE.String())
	}
	if !platformSlugRE.MatchString(cfg.RepoSlug) {
		return fmt.Errorf("platform config: platform_repo_slug must match %s", platformSlugRE.String())
	}
	if cfg.CompanyDisplayName == "" {
		return fmt.Errorf("platform config: platform_company_display_name is required")
	}
	if cfg.OrganizationName == "" {
		return fmt.Errorf("platform config: platform_organization_name is required")
	}
	if cfg.OwnerEmail == "" {
		return fmt.Errorf("platform config: platform_owner_email is required")
	}
	if _, err := mail.ParseAddress(cfg.OwnerEmail); err != nil {
		return fmt.Errorf("platform config: platform_owner_email is invalid: %w", err)
	}
	if cfg.RootOwnerEmail == "" {
		return fmt.Errorf("platform config: platform_root_owner_email is required")
	}
	if _, err := mail.ParseAddress(cfg.RootOwnerEmail); err != nil {
		return fmt.Errorf("platform config: platform_root_owner_email is invalid: %w", err)
	}
	if cfg.OwnerName == "" {
		return fmt.Errorf("platform config: platform_owner_name is required")
	}
	if cfg.OwnerAlias == "" {
		return fmt.Errorf("platform config: platform_owner_alias is required")
	}
	if cfg.TrustTier == "" {
		return fmt.Errorf("platform config: platform_trust_tier is required")
	}
	if cfg.RepoDisplayName == "" {
		return fmt.Errorf("platform config: platform_repo_display_name is required")
	}
	if err := cfg.validateGitHubDogfood(); err != nil {
		return err
	}
	if cfg.ForgejoDomain == "" || strings.Contains(cfg.ForgejoDomain, "{{") {
		return fmt.Errorf("platform config: forgejo_domain or forgejo_subdomain + verself_domain is required")
	}
	if cfg.VerselfDomain == "" || strings.Contains(cfg.VerselfDomain, "{{") {
		return fmt.Errorf("platform config: verself_domain is required")
	}
	if cfg.ForgejoWebhookURL == "" || strings.Contains(cfg.ForgejoWebhookURL, "{{") {
		return fmt.Errorf("platform config: verself_domain is required for the sandbox Forgejo webhook URL")
	}
	if cfg.ZitadelHost == "" || strings.Contains(cfg.ZitadelHost, "{{") {
		return fmt.Errorf("platform config: zitadel_domain or zitadel_subdomain + verself_domain is required")
	}
	return nil
}

func (cfg platformConfig) githubDogfoodConfigured() bool {
	return cfg.GitHubAccountID > 0 || cfg.GitHubInstallationID > 0 || cfg.GitHubRepositoryID > 0
}

func (cfg platformConfig) validateGitHubDogfood() error {
	if !cfg.githubDogfoodConfigured() {
		return nil
	}
	switch {
	case cfg.GitHubAccountID <= 0:
		return fmt.Errorf("platform config: platform_github_account_id is required when GitHub dogfood is configured")
	case cfg.GitHubInstallationID <= 0:
		return fmt.Errorf("platform config: platform_github_installation_id is required when GitHub dogfood is configured")
	case cfg.GitHubRepositoryID <= 0:
		return fmt.Errorf("platform config: platform_github_repository_id is required when GitHub dogfood is configured")
	}
	return nil
}

func resolveForgejoDomain(ops platformMainVars) string {
	domain := strings.TrimSpace(ops.ForgejoDomain)
	if domain != "" && !strings.Contains(domain, "{{") {
		return domain
	}
	subdomain := strings.TrimSpace(ops.ForgejoSubdomain)
	verselfDomain := strings.TrimSpace(ops.VerselfDomain)
	if subdomain == "" || verselfDomain == "" {
		return domain
	}
	return subdomain + "." + verselfDomain
}

func resolveZitadelHost(ops platformMainVars) string {
	domain := strings.TrimSpace(ops.ZitadelDomain)
	if domain != "" && !strings.Contains(domain, "{{") {
		return domain
	}
	subdomain := strings.TrimSpace(ops.ZitadelSubdomain)
	verselfDomain := strings.TrimSpace(ops.VerselfDomain)
	if subdomain == "" {
		subdomain = "auth"
	}
	if verselfDomain == "" {
		return domain
	}
	return subdomain + "." + verselfDomain
}

func resolveForgejoWebhookURL(ops platformMainVars) string {
	verselfDomain := strings.TrimSpace(ops.VerselfDomain)
	if verselfDomain == "" {
		return ""
	}
	return "https://sandbox.api." + verselfDomain + "/webhooks/forgejo/actions"
}

func (r *platformRunner) seed() (platformReport, error) {
	if err := r.ensureIdentityOrganization(); err != nil {
		return platformReport{}, err
	}
	if _, err := r.ensurePlatformOwner(); err != nil {
		return platformReport{}, err
	}
	rootOwner, err := r.resolvePlatformRootOwner()
	if err != nil {
		return platformReport{}, err
	}
	if err := r.ensureIAMOwnerPolicy(rootOwner.ID); err != nil {
		return platformReport{}, err
	}
	if err := r.ensurePlatformBillingOrganization(); err != nil {
		return platformReport{}, err
	}
	if err := r.ensureProject(); err != nil {
		return platformReport{}, err
	}
	forgejoRepo, err := r.ensureForgejo()
	if err != nil {
		return platformReport{}, err
	}
	if err := r.ensureSourceRepository(strconv.FormatInt(forgejoRepo.ID, 10)); err != nil {
		return platformReport{}, err
	}
	if err := r.ensureSandboxRunnerRepository(forgejoRepo.ID); err != nil {
		return platformReport{}, err
	}
	if err := r.ensureGitHubDogfoodRunnerRepository(); err != nil {
		return platformReport{}, err
	}
	if err := r.ensureForgejoActionsWebhook(); err != nil {
		return platformReport{}, err
	}
	report, err := r.check()
	report.Changed = append([]string{}, r.changes...)
	return report, err
}

func (r *platformRunner) ensureIAMOwnerPolicy(ownerSubject string) error {
	return r.withSpan("platform.iam_policy.ensure", []attribute.KeyValue{
		attribute.String("verself.org_id", r.cfg.PublicOrgIDText),
		attribute.String("verself.owner_subject", ownerSubject),
	}, func(ctx context.Context) error {
		endpoint, err := lookupNomadService(ctx, r.rt.SSH, "spicedb-grpc")
		if err != nil {
			return fmt.Errorf("iam policy: resolve spicedb-grpc: %w", err)
		}
		args := []string{
			"bootstrap-policy",
			"--spicedb-endpoint", endpoint,
			"--spicedb-preshared-key-file", platformIAMSpiceDBPresharedKeyPath,
			"--org-id", r.cfg.PublicOrgIDText,
			"--owner-subject", ownerSubject,
		}
		if err := runRemoteBazelExecutable(r.rt, platformIAMBootstrapTarget, platformIAMBootstrapBin, "verself-iam-bootstrap-policy", "iam_service", args); err != nil {
			return fmt.Errorf("iam policy: bootstrap owner policy: %w", err)
		}
		return nil
	})
}

func (r *platformRunner) check() (platformReport, error) {
	ids := r.ids()
	report := platformReport{
		Version:   platformManifestVersion,
		Action:    r.opts.action,
		Site:      r.rt.Site,
		Config:    r.cfg,
		ProjectID: ids.ProjectID.String(),
		RepoID:    ids.RepoID.String(),
		BackendID: ids.BackendID.String(),
	}
	var issues []string
	report.BoundaryResults = append(report.BoundaryResults, r.checkIdentityOrganization(&issues))
	report.BoundaryResults = append(report.BoundaryResults, r.checkPlatformOwner(&issues))
	report.BoundaryResults = append(report.BoundaryResults, r.checkPlatformBillingOrganization(&issues))
	report.BoundaryResults = append(report.BoundaryResults, r.checkProject(&issues))
	forgejoRow, forgejoRepoID := r.checkForgejo(&issues)
	report.ForgejoRepoID = forgejoRepoID
	report.BoundaryResults = append(report.BoundaryResults, forgejoRow)
	report.BoundaryResults = append(report.BoundaryResults, r.checkSourceRepository(forgejoRepoID, &issues))
	report.BoundaryResults = append(report.BoundaryResults, r.checkSandboxRunnerRepository(forgejoRepoID, &issues))
	if r.cfg.githubDogfoodConfigured() {
		report.BoundaryResults = append(report.BoundaryResults, r.checkGitHubDogfoodRunnerRepository(&issues))
	}
	if len(issues) > 0 {
		sort.Strings(issues)
		return report, platformIssueError{issues: issues}
	}
	return report, nil
}

func (r *platformRunner) ids() platformIDs {
	projectID := stableUUID("project", r.cfg.PublicOrgIDText, r.cfg.RepoSlug)
	repoID := stableUUID("source-repository", r.cfg.PublicOrgIDText, projectID.String())
	backendID := stableUUID("source-repository-backend", repoID.String(), "forgejo")
	return platformIDs{ProjectID: projectID, RepoID: repoID, BackendID: backendID}
}

func (r *platformRunner) environments() []platformEnvironmentSpec {
	ids := r.ids()
	return []platformEnvironmentSpec{
		{ID: stableUUID("project-environment", ids.ProjectID.String(), "production"), Slug: "production", DisplayName: "Production", Kind: "production"},
		{ID: stableUUID("project-environment", ids.ProjectID.String(), "preview"), Slug: "preview", DisplayName: "Preview", Kind: "preview"},
		{ID: stableUUID("project-environment", ids.ProjectID.String(), "development"), Slug: "development", DisplayName: "Development", Kind: "development"},
	}
}

func stableUUID(parts ...string) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("urn:verself:"+strings.Join(parts, ":")))
}

func (r *platformRunner) ensureIdentityOrganization() error {
	return r.withSpan("platform.iam.ensure", []attribute.KeyValue{
		attribute.String("db.system", "postgresql"),
		attribute.String("db.name", "iam_service"),
		attribute.String("verself.org_id", r.cfg.PublicOrgIDText),
		attribute.String("iam.identity_provider_org_id", r.cfg.OrgIDText),
		attribute.String("verself.org_slug", r.cfg.CompanySlug),
		attribute.String("verself.org_name", r.cfg.OrganizationName),
	}, func(ctx context.Context) error {
		conn, err := r.openPG(ctx, "iam_service")
		if err != nil {
			return err
		}
		defer func() { _ = conn.Close(context.Background()) }()
		tx, err := conn.Begin(ctx)
		if err != nil {
			return fmt.Errorf("iam organization: begin: %w", err)
		}
		defer rollbackTx(ctx, tx)

		var providerOrgID, displayName, slug, state string
		err = tx.QueryRow(ctx, `
SELECT identity_provider_org_id, display_name, slug, state
FROM iam_organizations
WHERE org_id = $1
FOR UPDATE`, r.cfg.PublicOrgIDText).Scan(&providerOrgID, &displayName, &slug, &state)
		now := time.Now().UTC()
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			var staleOrgID string
			staleErr := tx.QueryRow(ctx, `
SELECT org_id
FROM iam_organizations
WHERE slug = $1
FOR UPDATE`, r.cfg.CompanySlug).Scan(&staleOrgID)
			if staleErr == nil && staleOrgID != r.cfg.PublicOrgIDText {
				if _, err := tx.Exec(ctx, `
DELETE FROM iam_organizations
WHERE org_id = $1`, staleOrgID); err != nil {
					return fmt.Errorf("iam organization: delete stale slug owner %s: %w", staleOrgID, err)
				}
				r.markChanged("iam.organization.stale_slug_owner_deleted")
			} else if staleErr != nil && !errors.Is(staleErr, pgx.ErrNoRows) {
				return fmt.Errorf("iam organization: query stale slug owner: %w", staleErr)
			}
			if _, err := tx.Exec(ctx, `
INSERT INTO iam_organizations (org_id, identity_provider_org_id, display_name, slug, state, version, created_by, updated_by, created_at, updated_at)
VALUES ($1, $2, $3, $4, 'active', 1, $5, $5, $6, $6)`,
				r.cfg.PublicOrgIDText, r.cfg.OrgIDText, r.cfg.OrganizationName, r.cfg.CompanySlug, platformActor, now); err != nil {
				return fmt.Errorf("iam organization: insert: %w", err)
			}
			r.markChanged("iam.organization.created")
		case err != nil:
			return fmt.Errorf("iam organization: query: %w", err)
		default:
			if slug != r.cfg.CompanySlug {
				if _, err := tx.Exec(ctx, `
INSERT INTO iam_organization_slug_redirects (slug, org_id, created_at, created_by)
VALUES ($1, $2, $3, $4)
ON CONFLICT DO NOTHING`, slug, r.cfg.PublicOrgIDText, now, platformActor); err != nil {
					return fmt.Errorf("iam organization: insert slug redirect: %w", err)
				}
			}
			if providerOrgID != r.cfg.OrgIDText || displayName != r.cfg.OrganizationName || slug != r.cfg.CompanySlug || state != "active" {
				if _, err := tx.Exec(ctx, `
UPDATE iam_organizations
SET identity_provider_org_id = $2,
    display_name = $3,
    slug = $4,
    state = 'active',
    version = version + 1,
    updated_at = $5,
    updated_by = $6
WHERE org_id = $1`,
					r.cfg.PublicOrgIDText, r.cfg.OrgIDText, r.cfg.OrganizationName, r.cfg.CompanySlug, now, platformActor); err != nil {
					return fmt.Errorf("iam organization: update: %w", err)
				}
				r.markChanged("iam.organization.updated")
			}
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("iam organization: commit: %w", err)
		}
		return nil
	})
}

func (r *platformRunner) ensurePlatformBillingOrganization() error {
	return r.withSpan("platform.billing_org.ensure", []attribute.KeyValue{
		attribute.String("db.system", "postgresql"),
		attribute.String("db.name", "billing"),
		attribute.String("verself.org_id", r.cfg.PublicOrgIDText),
		attribute.String("billing.trust_tier", r.cfg.TrustTier),
	}, func(ctx context.Context) error {
		if r.cfg.TrustTier != "platform" {
			return fmt.Errorf("platform billing organization requires trust tier platform, got %q", r.cfg.TrustTier)
		}
		conn, err := r.openPG(ctx, "billing")
		if err != nil {
			return err
		}
		defer func() { _ = conn.Close(context.Background()) }()
		tx, err := conn.Begin(ctx)
		if err != nil {
			return fmt.Errorf("billing platform organization: begin: %w", err)
		}
		defer rollbackTx(ctx, tx)
		rows, err := tx.Query(ctx, `
SELECT org_id
FROM orgs
WHERE trust_tier = 'platform'
FOR UPDATE`)
		if err != nil {
			return fmt.Errorf("billing platform organization: query platform orgs: %w", err)
		}
		var platformOrgIDs []string
		for rows.Next() {
			var orgID string
			if err := rows.Scan(&orgID); err != nil {
				rows.Close()
				return err
			}
			platformOrgIDs = append(platformOrgIDs, orgID)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return fmt.Errorf("billing platform organization: scan platform orgs: %w", err)
		}
		for _, orgID := range platformOrgIDs {
			if orgID != r.cfg.PublicOrgIDText {
				return fmt.Errorf("billing platform organization: refusing second platform org %s while configured platform org is %s", orgID, r.cfg.PublicOrgIDText)
			}
		}
		tag, err := tx.Exec(ctx, `
INSERT INTO orgs (org_id, display_name, billing_email, trust_tier, overage_policy, overage_consent_at)
VALUES ($1, $2, $3, 'platform', 'block', NULL)
ON CONFLICT (org_id) DO UPDATE
SET display_name = EXCLUDED.display_name,
    billing_email = EXCLUDED.billing_email,
    trust_tier = 'platform',
    overage_policy = 'block',
    overage_consent_at = NULL,
    updated_at = now()
WHERE orgs.display_name IS DISTINCT FROM EXCLUDED.display_name
   OR orgs.billing_email IS DISTINCT FROM EXCLUDED.billing_email
   OR orgs.trust_tier IS DISTINCT FROM 'platform'
   OR orgs.overage_policy IS DISTINCT FROM 'block'
   OR orgs.overage_consent_at IS NOT NULL`,
			r.cfg.PublicOrgIDText, r.cfg.OrganizationName, r.cfg.OwnerEmail)
		if err != nil {
			return fmt.Errorf("billing platform organization: upsert: %w", err)
		}
		if tag.RowsAffected() > 0 {
			r.markChanged("billing.platform_organization.updated")
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("billing platform organization: commit: %w", err)
		}
		return nil
	})
}

func (r *platformRunner) ensureProject() error {
	return r.withSpan("platform.projects.ensure", []attribute.KeyValue{
		attribute.String("db.system", "postgresql"),
		attribute.String("db.name", "projects_service"),
		attribute.String("verself.org_id", r.cfg.PublicOrgIDText),
		attribute.String("verself.project_slug", r.cfg.RepoSlug),
	}, func(ctx context.Context) error {
		conn, err := r.openPG(ctx, "projects_service")
		if err != nil {
			return err
		}
		defer func() { _ = conn.Close(context.Background()) }()
		ids := r.ids()
		tx, err := conn.Begin(ctx)
		if err != nil {
			return fmt.Errorf("project: begin: %w", err)
		}
		defer rollbackTx(ctx, tx)

		var existingSlug, displayName, description, state string
		err = tx.QueryRow(ctx, `
SELECT slug, display_name, description, state
FROM projects
WHERE org_id = $1 AND project_id = $2
FOR UPDATE`, r.cfg.PublicOrgIDText, ids.ProjectID).Scan(&existingSlug, &displayName, &description, &state)
		now := time.Now().UTC()
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			var conflicting uuid.UUID
			conflictErr := tx.QueryRow(ctx, `
SELECT project_id
FROM projects
WHERE org_id = $1 AND slug = $2`, r.cfg.PublicOrgIDText, r.cfg.RepoSlug).Scan(&conflicting)
			if conflictErr == nil {
				return fmt.Errorf("project: slug %q is already owned by non-deterministic project_id %s", r.cfg.RepoSlug, conflicting)
			}
			if !errors.Is(conflictErr, pgx.ErrNoRows) {
				return fmt.Errorf("project: query slug conflict: %w", conflictErr)
			}
			if _, err := tx.Exec(ctx, `
INSERT INTO projects (project_id, org_id, slug, display_name, description, state, version, created_by, updated_by, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, 'active', 1, $6, $6, $7, $7)`,
				ids.ProjectID, r.cfg.PublicOrgIDText, r.cfg.RepoSlug, r.cfg.RepoDisplayName, r.cfg.RepoDescription, platformActor, now); err != nil {
				return fmt.Errorf("project: insert: %w", err)
			}
			r.markChanged("projects.project.created")
		case err != nil:
			return fmt.Errorf("project: query: %w", err)
		default:
			if existingSlug != r.cfg.RepoSlug {
				if _, err := tx.Exec(ctx, `
INSERT INTO project_slug_redirects (org_id, slug, project_id, created_by, created_at)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT DO NOTHING`, r.cfg.PublicOrgIDText, existingSlug, ids.ProjectID, platformActor, now); err != nil {
					return fmt.Errorf("project: insert slug redirect: %w", err)
				}
			}
			if existingSlug != r.cfg.RepoSlug || displayName != r.cfg.RepoDisplayName || description != r.cfg.RepoDescription || state != "active" {
				if _, err := tx.Exec(ctx, `
UPDATE projects
SET slug = $3,
    display_name = $4,
    description = $5,
    state = 'active',
    version = version + 1,
    updated_by = $6,
    updated_at = $7,
    archived_at = NULL
WHERE org_id = $1 AND project_id = $2`,
					r.cfg.PublicOrgIDText, ids.ProjectID, r.cfg.RepoSlug, r.cfg.RepoDisplayName, r.cfg.RepoDescription, platformActor, now); err != nil {
					return fmt.Errorf("project: update: %w", err)
				}
				r.markChanged("projects.project.updated")
			}
		}
		for _, env := range r.environments() {
			if err := r.ensureProjectEnvironment(ctx, tx, ids.ProjectID, env, now); err != nil {
				return err
			}
		}
		payload, err := json.Marshal(map[string]string{"slug": r.cfg.RepoSlug, "display_name": r.cfg.RepoDisplayName})
		if err != nil {
			return fmt.Errorf("project: marshal seed event payload: %w", err)
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO project_events (event_id, org_id, project_id, environment_id, event_type, actor_id, payload, trace_id, traceparent, created_at)
VALUES ($1, $2, $3, NULL, 'project.platform_seeded', $4, $5, $6, '', $7)
ON CONFLICT DO NOTHING`,
			stableUUID("project-event", ids.ProjectID.String(), "platform-seeded"),
			r.cfg.PublicOrgIDText,
			ids.ProjectID,
			platformActor,
			payload,
			r.rt.TraceID(),
			now); err != nil {
			return fmt.Errorf("project: insert seed event: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("project: commit: %w", err)
		}
		return nil
	})
}

func (r *platformRunner) ensureProjectEnvironment(ctx context.Context, tx pgx.Tx, projectID uuid.UUID, env platformEnvironmentSpec, now time.Time) error {
	var existingSlug, displayName, kind, state string
	err := tx.QueryRow(ctx, `
SELECT slug, display_name, kind, state
FROM project_environments
WHERE org_id = $1 AND project_id = $2 AND environment_id = $3
FOR UPDATE`, r.cfg.PublicOrgIDText, projectID, env.ID).Scan(&existingSlug, &displayName, &kind, &state)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		if _, err := tx.Exec(ctx, `
INSERT INTO project_environments (environment_id, project_id, org_id, slug, display_name, kind, state, protection_policy, version, created_by, updated_by, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, 'active', '{}'::jsonb, 1, $7, $7, $8, $8)`,
			env.ID, projectID, r.cfg.PublicOrgIDText, env.Slug, env.DisplayName, env.Kind, platformActor, now); err != nil {
			return fmt.Errorf("project environment %s: insert: %w", env.Slug, err)
		}
		r.markChanged("projects.environment." + env.Slug + ".created")
		return nil
	case err != nil:
		return fmt.Errorf("project environment %s: query: %w", env.Slug, err)
	}
	if existingSlug != env.Slug || displayName != env.DisplayName || kind != env.Kind || state != "active" {
		if _, err := tx.Exec(ctx, `
UPDATE project_environments
SET slug = $4,
    display_name = $5,
    kind = $6,
    state = 'active',
    version = version + 1,
    updated_by = $7,
    updated_at = $8,
    archived_at = NULL
WHERE org_id = $1 AND project_id = $2 AND environment_id = $3`,
			r.cfg.PublicOrgIDText, projectID, env.ID, env.Slug, env.DisplayName, env.Kind, platformActor, now); err != nil {
			return fmt.Errorf("project environment %s: update: %w", env.Slug, err)
		}
		r.markChanged("projects.environment." + env.Slug + ".updated")
	}
	return nil
}

func (r *platformRunner) ensureForgejo() (platformForgejoRepo, error) {
	var repo platformForgejoRepo
	err := r.withSpan("platform.forgejo.ensure", []attribute.KeyValue{
		attribute.String("http.url", "http://"+r.opts.forgejoRemoteAddr),
		attribute.String("forgejo.org", r.cfg.CompanySlug),
		attribute.String("forgejo.repo", r.cfg.RepoSlug),
	}, func(ctx context.Context) error {
		client, closeFn, err := r.forgejoClient(ctx)
		if err != nil {
			return err
		}
		defer closeFn()
		if err := r.ensureForgejoOrg(ctx, client); err != nil {
			return err
		}
		repo, err = r.ensureForgejoRepo(ctx, client)
		return err
	})
	return repo, err
}

func (r *platformRunner) ensureForgejoOrg(ctx context.Context, client platformForgejoClient) error {
	org, found, err := client.GetOrg(ctx, r.cfg.CompanySlug)
	if err != nil {
		return err
	}
	if !found {
		if _, err := client.CreateOrg(ctx, map[string]any{
			"username":    r.cfg.CompanySlug,
			"full_name":   r.cfg.CompanyDisplayName,
			"description": "Verself platform organization",
			"visibility":  "private",
		}); err != nil {
			return err
		}
		r.markChanged("forgejo.organization.created")
		return nil
	}
	if org.FullName != r.cfg.CompanyDisplayName || org.Visibility != "private" {
		if _, err := client.PatchOrg(ctx, r.cfg.CompanySlug, map[string]any{
			"full_name":  r.cfg.CompanyDisplayName,
			"visibility": "private",
		}); err != nil {
			return err
		}
		r.markChanged("forgejo.organization.updated")
	}
	return nil
}

func (r *platformRunner) ensureForgejoRepo(ctx context.Context, client platformForgejoClient) (platformForgejoRepo, error) {
	repo, found, err := client.GetRepo(ctx, r.cfg.CompanySlug, r.cfg.RepoSlug)
	if err != nil {
		return platformForgejoRepo{}, err
	}
	if !found {
		repo, err = client.CreateOrgRepo(ctx, r.cfg.CompanySlug, map[string]any{
			"name":           r.cfg.RepoSlug,
			"description":    r.cfg.RepoDescription,
			"private":        r.opts.forgejoRepoPrivate,
			"auto_init":      false,
			"default_branch": platformDefaultBranch,
		})
		if err != nil {
			return platformForgejoRepo{}, err
		}
		r.markChanged("forgejo.repository.created")
		return repo, nil
	}
	if repo.Description != r.cfg.RepoDescription || repo.Private != r.opts.forgejoRepoPrivate || (repo.DefaultBranch != "" && repo.DefaultBranch != platformDefaultBranch) {
		repo, err = client.PatchRepo(ctx, r.cfg.CompanySlug, r.cfg.RepoSlug, map[string]any{
			"description":    r.cfg.RepoDescription,
			"private":        r.opts.forgejoRepoPrivate,
			"default_branch": platformDefaultBranch,
		})
		if err != nil {
			return platformForgejoRepo{}, err
		}
		r.markChanged("forgejo.repository.updated")
	}
	return repo, nil
}

func (r *platformRunner) ensureSourceRepository(forgejoRepoID string) error {
	return r.withSpan("platform.source.ensure", []attribute.KeyValue{
		attribute.String("db.system", "postgresql"),
		attribute.String("db.name", "source_code_hosting"),
		attribute.String("verself.org_id", r.cfg.PublicOrgIDText),
		attribute.String("source.backend", "forgejo"),
		attribute.String("source.backend_owner", r.cfg.CompanySlug),
		attribute.String("source.backend_repo", r.cfg.RepoSlug),
	}, func(ctx context.Context) error {
		conn, err := r.openPG(ctx, "source_code_hosting")
		if err != nil {
			return err
		}
		defer func() { _ = conn.Close(context.Background()) }()
		ids := r.ids()
		tx, err := conn.Begin(ctx)
		if err != nil {
			return fmt.Errorf("source repository: begin: %w", err)
		}
		defer rollbackTx(ctx, tx)
		now := time.Now().UTC()

		var existingProjectID uuid.UUID
		var name, slug, description, defaultBranch, visibility, state string
		err = tx.QueryRow(ctx, `
SELECT project_id, name, slug, description, default_branch, visibility, state
FROM source_repositories
WHERE org_id = $1 AND repo_id = $2
FOR UPDATE`, r.cfg.PublicOrgIDText, ids.RepoID).Scan(&existingProjectID, &name, &slug, &description, &defaultBranch, &visibility, &state)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			var conflicting uuid.UUID
			conflictErr := tx.QueryRow(ctx, `
SELECT repo_id
FROM source_repositories
WHERE org_id = $1 AND project_id = $2`, r.cfg.PublicOrgIDText, ids.ProjectID).Scan(&conflicting)
			if conflictErr == nil {
				return fmt.Errorf("source repository: org/project is already owned by non-deterministic repo_id %s", conflicting)
			}
			if !errors.Is(conflictErr, pgx.ErrNoRows) {
				return fmt.Errorf("source repository: query org/project conflict: %w", conflictErr)
			}
			if _, err := tx.Exec(ctx, `
INSERT INTO source_repositories (repo_id, org_id, project_id, created_by, name, slug, description, default_branch, visibility, state, version, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'private', 'active', 1, $9, $9)`,
				ids.RepoID, r.cfg.PublicOrgIDText, ids.ProjectID, platformActor, r.cfg.RepoDisplayName, r.cfg.RepoSlug, r.cfg.RepoDescription, platformDefaultBranch, now); err != nil {
				return fmt.Errorf("source repository: insert: %w", err)
			}
			r.markChanged("source.repository.created")
		case err != nil:
			return fmt.Errorf("source repository: query: %w", err)
		default:
			if existingProjectID != ids.ProjectID || name != r.cfg.RepoDisplayName || slug != r.cfg.RepoSlug || description != r.cfg.RepoDescription || defaultBranch != platformDefaultBranch || visibility != "private" || state != "active" {
				if _, err := tx.Exec(ctx, `
UPDATE source_repositories
SET project_id = $3,
    name = $4,
    slug = $5,
    description = $6,
    default_branch = $7,
    visibility = 'private',
    state = 'active',
    version = version + 1,
    updated_at = $8,
    deleted_at = NULL
WHERE org_id = $1 AND repo_id = $2`,
					r.cfg.PublicOrgIDText, ids.RepoID, ids.ProjectID, r.cfg.RepoDisplayName, r.cfg.RepoSlug, r.cfg.RepoDescription, platformDefaultBranch, now); err != nil {
					return fmt.Errorf("source repository: update: %w", err)
				}
				r.markChanged("source.repository.updated")
			}
		}
		if err := r.ensureSourceBackend(ctx, tx, ids, forgejoRepoID, now); err != nil {
			return err
		}
		details, err := json.Marshal(map[string]string{
			"slug":            r.cfg.RepoSlug,
			"backend":         "forgejo",
			"backend_owner":   r.cfg.CompanySlug,
			"backend_repo":    r.cfg.RepoSlug,
			"backend_repo_id": forgejoRepoID,
			"origin":          "platform-seed",
		})
		if err != nil {
			return fmt.Errorf("source repository: marshal seed event details: %w", err)
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO source_events (event_id, org_id, actor_id, repo_id, project_id, event_type, result, trace_id, details, created_at)
VALUES ($1, $2, $3, $4, $5, 'source.platform_repository.seeded', 'allowed', $6, $7, $8)
ON CONFLICT DO NOTHING`,
			stableUUID("source-event", ids.RepoID.String(), "platform-seeded"),
			r.cfg.PublicOrgIDText,
			platformActor,
			ids.RepoID,
			ids.ProjectID,
			r.rt.TraceID(),
			details,
			now); err != nil {
			return fmt.Errorf("source repository: insert event: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("source repository: commit: %w", err)
		}
		return nil
	})
}

func (r *platformRunner) ensureSourceBackend(ctx context.Context, tx pgx.Tx, ids platformIDs, forgejoRepoID string, now time.Time) error {
	var conflictingRepoID, conflictingBackendID uuid.UUID
	conflictErr := tx.QueryRow(ctx, `
SELECT repo_id, backend_id
FROM source_repository_backends
WHERE backend = 'forgejo' AND backend_owner = $1 AND backend_repo = $2`,
		r.cfg.CompanySlug, r.cfg.RepoSlug).Scan(&conflictingRepoID, &conflictingBackendID)
	if conflictErr == nil && (conflictingRepoID != ids.RepoID || conflictingBackendID != ids.BackendID) {
		if _, err := tx.Exec(ctx, `
DELETE FROM source_repositories
WHERE repo_id = $1`, conflictingRepoID); err != nil {
			return fmt.Errorf("source backend: replace stale forgejo mapping repo_id %s backend_id %s: %w", conflictingRepoID, conflictingBackendID, err)
		}
		r.markChanged("source.repository.stale_mapping_deleted")
	}
	if conflictErr != nil && !errors.Is(conflictErr, pgx.ErrNoRows) {
		return fmt.Errorf("source backend: query backend conflict: %w", conflictErr)
	}

	var backendOwner, backendRepo, backendRepoID, state string
	err := tx.QueryRow(ctx, `
SELECT backend_owner, backend_repo, backend_repo_id, state
FROM source_repository_backends
WHERE backend_id = $1
FOR UPDATE`, ids.BackendID).Scan(&backendOwner, &backendRepo, &backendRepoID, &state)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		if _, err := tx.Exec(ctx, `
INSERT INTO source_repository_backends (backend_id, repo_id, backend, backend_owner, backend_repo, backend_repo_id, state, created_at, updated_at)
VALUES ($1, $2, 'forgejo', $3, $4, $5, 'active', $6, $6)`,
			ids.BackendID, ids.RepoID, r.cfg.CompanySlug, r.cfg.RepoSlug, forgejoRepoID, now); err != nil {
			return fmt.Errorf("source backend: insert: %w", err)
		}
		r.markChanged("source.backend.created")
	case err != nil:
		return fmt.Errorf("source backend: query: %w", err)
	default:
		if backendOwner != r.cfg.CompanySlug || backendRepo != r.cfg.RepoSlug || backendRepoID != forgejoRepoID || state != "active" {
			if _, err := tx.Exec(ctx, `
UPDATE source_repository_backends
SET backend_owner = $3,
    backend_repo = $4,
    backend_repo_id = $5,
    state = 'active',
    updated_at = $6
WHERE backend_id = $1 AND repo_id = $2`,
				ids.BackendID, ids.RepoID, r.cfg.CompanySlug, r.cfg.RepoSlug, forgejoRepoID, now); err != nil {
				return fmt.Errorf("source backend: update: %w", err)
			}
			r.markChanged("source.backend.updated")
		}
	}
	return nil
}

func (r *platformRunner) ensureSandboxRunnerRepository(forgejoRepoID int64) error {
	if forgejoRepoID <= 0 {
		return fmt.Errorf("sandbox runner repository: forgejo repo id is required")
	}
	return r.withSpan("platform.sandbox_runner.ensure", []attribute.KeyValue{
		attribute.String("db.system", "postgresql"),
		attribute.String("db.name", "sandbox_rental"),
		attribute.String("verself.org_id", r.cfg.PublicOrgIDText),
		attribute.Int64("forgejo.repository_id", forgejoRepoID),
	}, func(ctx context.Context) error {
		conn, err := r.openPG(ctx, "sandbox_rental")
		if err != nil {
			return err
		}
		defer func() { _ = conn.Close(context.Background()) }()
		ids := r.ids()
		now := time.Now().UTC()
		tag, err := conn.Exec(ctx, `
INSERT INTO runner_provider_repositories (
    provider, provider_repository_id, org_id, project_id, source_repository_id,
    provider_owner, provider_repo, repository_full_name, active, created_at, updated_at
)
VALUES ('forgejo', $1, $2, $3, $4, $5, $6, $7, true, $8, $8)
ON CONFLICT (provider, provider_repository_id) DO UPDATE SET
    org_id = EXCLUDED.org_id,
    project_id = EXCLUDED.project_id,
    source_repository_id = EXCLUDED.source_repository_id,
    provider_owner = EXCLUDED.provider_owner,
    provider_repo = EXCLUDED.provider_repo,
    repository_full_name = EXCLUDED.repository_full_name,
    active = true,
    updated_at = EXCLUDED.updated_at`,
			forgejoRepoID,
			r.cfg.PublicOrgIDText,
			ids.ProjectID,
			ids.RepoID,
			r.cfg.CompanySlug,
			r.cfg.RepoSlug,
			r.cfg.CompanySlug+"/"+r.cfg.RepoSlug,
			now,
		)
		if err != nil {
			return fmt.Errorf("sandbox runner repository: upsert: %w", err)
		}
		if tag.RowsAffected() > 0 {
			r.markChanged("sandbox.runner_repository.upserted")
		}
		return nil
	})
}

func (r *platformRunner) ensureGitHubDogfoodRunnerRepository() error {
	if !r.cfg.githubDogfoodConfigured() {
		return nil
	}
	return r.withSpan("platform.github_runner.ensure", []attribute.KeyValue{
		attribute.String("db.system", "postgresql"),
		attribute.String("db.name", "sandbox_rental"),
		attribute.String("verself.org_id", r.cfg.PublicOrgIDText),
		attribute.Int64("github.account_id", r.cfg.GitHubAccountID),
		attribute.Int64("github.installation_id", r.cfg.GitHubInstallationID),
		attribute.Int64("github.repository_id", r.cfg.GitHubRepositoryID),
	}, func(ctx context.Context) error {
		conn, err := r.openPG(ctx, "sandbox_rental")
		if err != nil {
			return err
		}
		defer func() { _ = conn.Close(context.Background()) }()
		tx, err := conn.Begin(ctx)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback(context.Background()) }()
		ids := r.ids()
		now := time.Now().UTC()
		if _, err := tx.Exec(ctx, `
INSERT INTO github_accounts (account_id, account_login, account_type, created_at, updated_at)
VALUES ($1, $2, 'Organization', $3, $3)
ON CONFLICT (account_id) DO UPDATE SET
    account_login = EXCLUDED.account_login,
    account_type = EXCLUDED.account_type,
    updated_at = EXCLUDED.updated_at`,
			r.cfg.GitHubAccountID,
			r.cfg.CompanySlug,
			now,
		); err != nil {
			return fmt.Errorf("github dogfood: upsert account: %w", err)
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO github_installations (installation_id, account_id, active, repository_selection, permissions_json, created_at, updated_at)
VALUES ($1, $2, true, 'selected', '{}'::jsonb, $3, $3)
ON CONFLICT (installation_id) DO UPDATE SET
    account_id = EXCLUDED.account_id,
    active = true,
    repository_selection = EXCLUDED.repository_selection,
    permissions_json = EXCLUDED.permissions_json,
    updated_at = EXCLUDED.updated_at`,
			r.cfg.GitHubInstallationID,
			r.cfg.GitHubAccountID,
			now,
		); err != nil {
			return fmt.Errorf("github dogfood: upsert installation: %w", err)
		}
		connectionID := stableUUID("github-installation-connection", r.cfg.PublicOrgIDText, strconv.FormatInt(r.cfg.GitHubInstallationID, 10))
		if _, err := tx.Exec(ctx, `
INSERT INTO github_installation_connections (
    connection_id, installation_id, org_id, connected_by_actor_id, state, created_at, updated_at
)
VALUES ($1, $2, $3, $4, 'active', $5, $5)
ON CONFLICT (installation_id, org_id) DO UPDATE SET
    connected_by_actor_id = EXCLUDED.connected_by_actor_id,
    state = 'active',
    updated_at = EXCLUDED.updated_at`,
			connectionID,
			r.cfg.GitHubInstallationID,
			r.cfg.PublicOrgIDText,
			platformActor,
			now,
		); err != nil {
			return fmt.Errorf("github dogfood: upsert connection: %w", err)
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO runner_provider_repositories (
    provider, provider_repository_id, org_id, project_id, source_repository_id,
    provider_owner, provider_repo, repository_full_name, active, created_at, updated_at
)
VALUES ('github', $1, $2, $3, $4, $5, $6, $7, true, $8, $8)
ON CONFLICT (provider, provider_repository_id) DO UPDATE SET
    org_id = EXCLUDED.org_id,
    project_id = EXCLUDED.project_id,
    source_repository_id = EXCLUDED.source_repository_id,
    provider_owner = EXCLUDED.provider_owner,
    provider_repo = EXCLUDED.provider_repo,
    repository_full_name = EXCLUDED.repository_full_name,
    active = true,
    updated_at = EXCLUDED.updated_at`,
			r.cfg.GitHubRepositoryID,
			r.cfg.PublicOrgIDText,
			ids.ProjectID,
			ids.RepoID,
			r.cfg.CompanySlug,
			r.cfg.RepoSlug,
			r.cfg.CompanySlug+"/"+r.cfg.RepoSlug,
			now,
		); err != nil {
			return fmt.Errorf("github dogfood: upsert runner repository: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("github dogfood: commit: %w", err)
		}
		r.markChanged("sandbox.github_runner_repository.upserted")
		return nil
	})
}

func (r *platformRunner) ensureForgejoActionsWebhook() error {
	return r.withSpan("platform.forgejo.actions_webhook.ensure", []attribute.KeyValue{
		attribute.String("forgejo.org", r.cfg.CompanySlug),
		attribute.String("forgejo.repo", r.cfg.RepoSlug),
		attribute.String("forgejo.webhook_url", r.cfg.ForgejoWebhookURL),
	}, func(ctx context.Context) error {
		rawSecret, err := opruntime.ReadRemoteFile(ctx, r.rt.SSH, r.opts.forgejoWebhookPath)
		if err != nil {
			return fmt.Errorf("forgejo webhook: read secret: %w", err)
		}
		secret := strings.TrimSpace(string(rawSecret))
		if secret == "" {
			return fmt.Errorf("forgejo webhook: secret is empty")
		}
		client, closeFn, err := r.forgejoClient(ctx)
		if err != nil {
			return err
		}
		defer closeFn()
		hooks, err := client.ListRepoHooks(ctx, r.cfg.CompanySlug, r.cfg.RepoSlug)
		if err != nil {
			return err
		}
		for _, hook := range hooks {
			if hook.Config != nil && strings.TrimSpace(hook.Config["url"]) == r.cfg.ForgejoWebhookURL {
				if err := client.PatchRepoHook(ctx, r.cfg.CompanySlug, r.cfg.RepoSlug, hook.ID, platformForgejoHookBody(r.cfg.ForgejoWebhookURL, secret, false)); err != nil {
					return err
				}
				r.markChanged("forgejo.actions_webhook.updated")
				return nil
			}
		}
		if err := client.CreateRepoHook(ctx, r.cfg.CompanySlug, r.cfg.RepoSlug, platformForgejoHookBody(r.cfg.ForgejoWebhookURL, secret, true)); err != nil {
			return err
		}
		r.markChanged("forgejo.actions_webhook.created")
		return nil
	})
}

func (r *platformRunner) checkIdentityOrganization(issues *[]string) platformBoundaryRow {
	row := platformBoundaryRow{Boundary: "iam_service.iam_organizations", Status: "ok"}
	err := r.withSpan("platform.iam.check", []attribute.KeyValue{
		attribute.String("db.system", "postgresql"),
		attribute.String("db.name", "iam_service"),
		attribute.String("verself.org_id", r.cfg.PublicOrgIDText),
		attribute.String("iam.identity_provider_org_id", r.cfg.OrgIDText),
	}, func(ctx context.Context) error {
		conn, err := r.openPG(ctx, "iam_service")
		if err != nil {
			return err
		}
		defer func() { _ = conn.Close(context.Background()) }()
		var providerOrgID, displayName, slug, state string
		err = conn.QueryRow(ctx, `
SELECT identity_provider_org_id, display_name, slug, state
FROM iam_organizations
WHERE org_id = $1`, r.cfg.PublicOrgIDText).Scan(&providerOrgID, &displayName, &slug, &state)
		if errors.Is(err, pgx.ErrNoRows) {
			*issues = append(*issues, "iam organization is missing")
			row.Status = "missing"
			return nil
		}
		if err != nil {
			return fmt.Errorf("iam organization: query: %w", err)
		}
		var mismatches []string
		if providerOrgID != r.cfg.OrgIDText {
			mismatches = append(mismatches, fmt.Sprintf("identity_provider_org_id=%q", providerOrgID))
		}
		if displayName != r.cfg.OrganizationName {
			mismatches = append(mismatches, fmt.Sprintf("display_name=%q", displayName))
		}
		if slug != r.cfg.CompanySlug {
			mismatches = append(mismatches, fmt.Sprintf("slug=%q", slug))
		}
		if state != "active" {
			mismatches = append(mismatches, fmt.Sprintf("state=%q", state))
		}
		if len(mismatches) > 0 {
			*issues = append(*issues, "iam organization mismatch: "+strings.Join(mismatches, ", "))
			row.Status = "mismatch"
			row.Detail = strings.Join(mismatches, ", ")
		}
		return nil
	})
	if err != nil {
		row.Status = "error"
		row.Detail = err.Error()
		*issues = append(*issues, err.Error())
	}
	return row
}

func (r *platformRunner) checkPlatformBillingOrganization(issues *[]string) platformBoundaryRow {
	row := platformBoundaryRow{Boundary: "billing.orgs.platform", Status: "ok"}
	err := r.withSpan("platform.billing_org.check", []attribute.KeyValue{
		attribute.String("db.system", "postgresql"),
		attribute.String("db.name", "billing"),
		attribute.String("verself.org_id", r.cfg.PublicOrgIDText),
	}, func(ctx context.Context) error {
		conn, err := r.openPG(ctx, "billing")
		if err != nil {
			return err
		}
		defer func() { _ = conn.Close(context.Background()) }()
		rows, err := conn.Query(ctx, `
SELECT org_id, display_name, billing_email, state, trust_tier
FROM orgs
WHERE trust_tier = 'platform'
ORDER BY org_id`)
		if err != nil {
			return fmt.Errorf("billing platform organization: query: %w", err)
		}
		defer rows.Close()
		type platformBillingOrg struct {
			orgID        string
			displayName  string
			billingEmail string
			state        string
			trustTier    string
		}
		var matches []platformBillingOrg
		for rows.Next() {
			var item platformBillingOrg
			if err := rows.Scan(&item.orgID, &item.displayName, &item.billingEmail, &item.state, &item.trustTier); err != nil {
				return err
			}
			matches = append(matches, item)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if len(matches) == 0 {
			*issues = append(*issues, "billing platform organization is missing")
			row.Status = "missing"
			return nil
		}
		if len(matches) > 1 {
			ids := make([]string, 0, len(matches))
			for _, match := range matches {
				ids = append(ids, match.orgID)
			}
			row.Status = "mismatch"
			row.Detail = "multiple platform orgs: " + strings.Join(ids, ",")
			*issues = append(*issues, row.Detail)
			return nil
		}
		match := matches[0]
		var mismatches []string
		if match.orgID != r.cfg.PublicOrgIDText {
			mismatches = append(mismatches, fmt.Sprintf("org_id=%q", match.orgID))
		}
		if match.displayName != r.cfg.OrganizationName {
			mismatches = append(mismatches, fmt.Sprintf("display_name=%q", match.displayName))
		}
		if match.billingEmail != r.cfg.OwnerEmail {
			mismatches = append(mismatches, fmt.Sprintf("billing_email=%q", match.billingEmail))
		}
		if match.state != "active" {
			mismatches = append(mismatches, fmt.Sprintf("state=%q", match.state))
		}
		if match.trustTier != "platform" {
			mismatches = append(mismatches, fmt.Sprintf("trust_tier=%q", match.trustTier))
		}
		if len(mismatches) > 0 {
			row.Status = "mismatch"
			row.Detail = strings.Join(mismatches, ", ")
			*issues = append(*issues, "billing platform organization mismatch: "+row.Detail)
		}
		return nil
	})
	if err != nil {
		row.Status = "error"
		row.Detail = err.Error()
		*issues = append(*issues, err.Error())
	}
	return row
}

func (r *platformRunner) checkProject(issues *[]string) platformBoundaryRow {
	row := platformBoundaryRow{Boundary: "projects_service.projects", Status: "ok"}
	err := r.withSpan("platform.projects.check", []attribute.KeyValue{
		attribute.String("db.system", "postgresql"),
		attribute.String("db.name", "projects_service"),
		attribute.String("verself.org_id", r.cfg.PublicOrgIDText),
		attribute.String("verself.project_slug", r.cfg.RepoSlug),
	}, func(ctx context.Context) error {
		conn, err := r.openPG(ctx, "projects_service")
		if err != nil {
			return err
		}
		defer func() { _ = conn.Close(context.Background()) }()
		ids := r.ids()
		var slug, displayName, description, state string
		err = conn.QueryRow(ctx, `
SELECT slug, display_name, description, state
FROM projects
WHERE org_id = $1 AND project_id = $2`, r.cfg.PublicOrgIDText, ids.ProjectID).Scan(&slug, &displayName, &description, &state)
		if errors.Is(err, pgx.ErrNoRows) {
			*issues = append(*issues, "platform project is missing")
			row.Status = "missing"
			return nil
		}
		if err != nil {
			return fmt.Errorf("project: query: %w", err)
		}
		var mismatches []string
		if slug != r.cfg.RepoSlug {
			mismatches = append(mismatches, fmt.Sprintf("slug=%q", slug))
		}
		if displayName != r.cfg.RepoDisplayName {
			mismatches = append(mismatches, fmt.Sprintf("display_name=%q", displayName))
		}
		if description != r.cfg.RepoDescription {
			mismatches = append(mismatches, fmt.Sprintf("description=%q", description))
		}
		if state != "active" {
			mismatches = append(mismatches, fmt.Sprintf("state=%q", state))
		}
		if envIssues, err := r.checkProjectEnvironments(ctx, conn, ids.ProjectID); err != nil {
			return err
		} else {
			mismatches = append(mismatches, envIssues...)
		}
		if len(mismatches) > 0 {
			*issues = append(*issues, "platform project mismatch: "+strings.Join(mismatches, ", "))
			row.Status = "mismatch"
			row.Detail = strings.Join(mismatches, ", ")
		}
		return nil
	})
	if err != nil {
		row.Status = "error"
		row.Detail = err.Error()
		*issues = append(*issues, err.Error())
	}
	return row
}

func (r *platformRunner) checkProjectEnvironments(ctx context.Context, conn *pgx.Conn, projectID uuid.UUID) ([]string, error) {
	rows, err := conn.Query(ctx, `
SELECT environment_id, slug, display_name, kind, state
FROM project_environments
WHERE org_id = $1 AND project_id = $2`, r.cfg.PublicOrgIDText, projectID)
	if err != nil {
		return nil, fmt.Errorf("project environments: query: %w", err)
	}
	defer rows.Close()
	type envRow struct {
		slug        string
		displayName string
		kind        string
		state       string
	}
	byID := map[uuid.UUID]envRow{}
	for rows.Next() {
		var id uuid.UUID
		var row envRow
		if err := rows.Scan(&id, &row.slug, &row.displayName, &row.kind, &row.state); err != nil {
			return nil, fmt.Errorf("project environments: scan: %w", err)
		}
		byID[id] = row
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("project environments: rows: %w", err)
	}
	var issues []string
	for _, spec := range r.environments() {
		row, ok := byID[spec.ID]
		if !ok {
			issues = append(issues, "environment "+spec.Slug+" missing")
			continue
		}
		if row.slug != spec.Slug || row.displayName != spec.DisplayName || row.kind != spec.Kind || row.state != "active" {
			issues = append(issues, "environment "+spec.Slug+" mismatch")
		}
	}
	return issues, nil
}

func (r *platformRunner) checkForgejo(issues *[]string) (platformBoundaryRow, string) {
	row := platformBoundaryRow{Boundary: "forgejo.repository", Status: "ok"}
	forgejoRepoID := ""
	err := r.withSpan("platform.forgejo.check", []attribute.KeyValue{
		attribute.String("http.url", "http://"+r.opts.forgejoRemoteAddr),
		attribute.String("forgejo.org", r.cfg.CompanySlug),
		attribute.String("forgejo.repo", r.cfg.RepoSlug),
	}, func(ctx context.Context) error {
		client, closeFn, err := r.forgejoClient(ctx)
		if err != nil {
			return err
		}
		defer closeFn()
		org, found, err := client.GetOrg(ctx, r.cfg.CompanySlug)
		if err != nil {
			return err
		}
		if !found {
			*issues = append(*issues, "Forgejo organization is missing")
			row.Status = "missing"
			return nil
		}
		var mismatches []string
		if org.FullName != r.cfg.CompanyDisplayName {
			mismatches = append(mismatches, fmt.Sprintf("org.full_name=%q", org.FullName))
		}
		if org.Visibility != "private" {
			mismatches = append(mismatches, fmt.Sprintf("org.visibility=%q", org.Visibility))
		}
		repo, found, err := client.GetRepo(ctx, r.cfg.CompanySlug, r.cfg.RepoSlug)
		if err != nil {
			return err
		}
		if !found {
			*issues = append(*issues, "Forgejo repository is missing")
			row.Status = "missing"
			return nil
		}
		forgejoRepoID = strconv.FormatInt(repo.ID, 10)
		if repo.Name != r.cfg.RepoSlug {
			mismatches = append(mismatches, fmt.Sprintf("repo.name=%q", repo.Name))
		}
		if repo.Private != r.opts.forgejoRepoPrivate {
			mismatches = append(mismatches, fmt.Sprintf("repo.private=%t", repo.Private))
		}
		if repo.Description != r.cfg.RepoDescription {
			mismatches = append(mismatches, fmt.Sprintf("repo.description=%q", repo.Description))
		}
		if repo.DefaultBranch != "" && repo.DefaultBranch != platformDefaultBranch {
			mismatches = append(mismatches, fmt.Sprintf("repo.default_branch=%q", repo.DefaultBranch))
		}
		if len(mismatches) > 0 {
			*issues = append(*issues, "Forgejo repository mismatch: "+strings.Join(mismatches, ", "))
			row.Status = "mismatch"
			row.Detail = strings.Join(mismatches, ", ")
		}
		return nil
	})
	if err != nil {
		row.Status = "error"
		row.Detail = err.Error()
		*issues = append(*issues, err.Error())
	}
	return row, forgejoRepoID
}

func (r *platformRunner) checkSourceRepository(forgejoRepoID string, issues *[]string) platformBoundaryRow {
	row := platformBoundaryRow{Boundary: "source_code_hosting.source_repositories", Status: "ok"}
	err := r.withSpan("platform.source.check", []attribute.KeyValue{
		attribute.String("db.system", "postgresql"),
		attribute.String("db.name", "source_code_hosting"),
		attribute.String("verself.org_id", r.cfg.PublicOrgIDText),
		attribute.String("source.backend", "forgejo"),
		attribute.String("source.backend_owner", r.cfg.CompanySlug),
		attribute.String("source.backend_repo", r.cfg.RepoSlug),
	}, func(ctx context.Context) error {
		conn, err := r.openPG(ctx, "source_code_hosting")
		if err != nil {
			return err
		}
		defer func() { _ = conn.Close(context.Background()) }()
		ids := r.ids()
		var projectID, backendID uuid.UUID
		var name, slug, description, defaultBranch, visibility, repoState, backend, backendOwner, backendRepo, backendRepoID, backendState string
		err = conn.QueryRow(ctx, `
SELECT
  r.project_id,
  r.name,
  r.slug,
  r.description,
  r.default_branch,
  r.visibility,
  r.state,
  b.backend_id,
  b.backend,
  b.backend_owner,
  b.backend_repo,
  b.backend_repo_id,
  b.state
FROM source_repositories r
JOIN source_repository_backends b ON b.repo_id = r.repo_id AND b.backend = 'forgejo'
WHERE r.org_id = $1 AND r.repo_id = $2`,
			r.cfg.PublicOrgIDText, ids.RepoID).Scan(
			&projectID,
			&name,
			&slug,
			&description,
			&defaultBranch,
			&visibility,
			&repoState,
			&backendID,
			&backend,
			&backendOwner,
			&backendRepo,
			&backendRepoID,
			&backendState,
		)
		if errors.Is(err, pgx.ErrNoRows) {
			*issues = append(*issues, "source repository mapping is missing")
			row.Status = "missing"
			return nil
		}
		if err != nil {
			return fmt.Errorf("source repository: query: %w", err)
		}
		var mismatches []string
		if projectID != ids.ProjectID {
			mismatches = append(mismatches, fmt.Sprintf("project_id=%s", projectID))
		}
		if name != r.cfg.RepoDisplayName {
			mismatches = append(mismatches, fmt.Sprintf("name=%q", name))
		}
		if slug != r.cfg.RepoSlug {
			mismatches = append(mismatches, fmt.Sprintf("slug=%q", slug))
		}
		if description != r.cfg.RepoDescription {
			mismatches = append(mismatches, fmt.Sprintf("description=%q", description))
		}
		if defaultBranch != platformDefaultBranch {
			mismatches = append(mismatches, fmt.Sprintf("default_branch=%q", defaultBranch))
		}
		if visibility != "private" || repoState != "active" {
			mismatches = append(mismatches, fmt.Sprintf("repo_state=%s/%s", visibility, repoState))
		}
		if backendID != ids.BackendID || backend != "forgejo" || backendOwner != r.cfg.CompanySlug || backendRepo != r.cfg.RepoSlug || backendState != "active" {
			mismatches = append(mismatches, "backend mapping mismatch")
		}
		if forgejoRepoID != "" && backendRepoID != forgejoRepoID {
			mismatches = append(mismatches, fmt.Sprintf("backend_repo_id=%q", backendRepoID))
		}
		if len(mismatches) > 0 {
			*issues = append(*issues, "source repository mismatch: "+strings.Join(mismatches, ", "))
			row.Status = "mismatch"
			row.Detail = strings.Join(mismatches, ", ")
		}
		return nil
	})
	if err != nil {
		row.Status = "error"
		row.Detail = err.Error()
		*issues = append(*issues, err.Error())
	}
	return row
}

func (r *platformRunner) checkSandboxRunnerRepository(forgejoRepoID string, issues *[]string) platformBoundaryRow {
	row := platformBoundaryRow{Boundary: "sandbox_rental.runner_provider_repositories", Status: "ok"}
	if strings.TrimSpace(forgejoRepoID) == "" {
		*issues = append(*issues, "sandbox runner repository cannot be checked without forgejo repo id")
		row.Status = "missing"
		return row
	}
	err := r.withSpan("platform.sandbox_runner.check", []attribute.KeyValue{
		attribute.String("db.system", "postgresql"),
		attribute.String("db.name", "sandbox_rental"),
		attribute.String("verself.org_id", r.cfg.PublicOrgIDText),
	}, func(ctx context.Context) error {
		conn, err := r.openPG(ctx, "sandbox_rental")
		if err != nil {
			return err
		}
		defer func() { _ = conn.Close(context.Background()) }()
		ids := r.ids()
		providerRepoID, err := strconv.ParseInt(forgejoRepoID, 10, 64)
		if err != nil || providerRepoID <= 0 {
			return fmt.Errorf("sandbox runner repository: invalid forgejo repo id %q", forgejoRepoID)
		}
		var orgID string
		var projectID, sourceRepositoryID uuid.UUID
		var providerOwner, providerRepo, repositoryFullName string
		var active bool
		err = conn.QueryRow(ctx, `
SELECT org_id, project_id, source_repository_id, provider_owner, provider_repo, repository_full_name, active
FROM runner_provider_repositories
WHERE provider = 'forgejo' AND provider_repository_id = $1`, providerRepoID).Scan(
			&orgID,
			&projectID,
			&sourceRepositoryID,
			&providerOwner,
			&providerRepo,
			&repositoryFullName,
			&active,
		)
		if errors.Is(err, pgx.ErrNoRows) {
			*issues = append(*issues, "sandbox runner repository mapping is missing")
			row.Status = "missing"
			return nil
		}
		if err != nil {
			return fmt.Errorf("sandbox runner repository: query: %w", err)
		}
		var mismatches []string
		if orgID != r.cfg.PublicOrgIDText {
			mismatches = append(mismatches, fmt.Sprintf("org_id=%q", orgID))
		}
		if projectID != ids.ProjectID {
			mismatches = append(mismatches, fmt.Sprintf("project_id=%s", projectID))
		}
		if sourceRepositoryID != ids.RepoID {
			mismatches = append(mismatches, fmt.Sprintf("source_repository_id=%s", sourceRepositoryID))
		}
		if providerOwner != r.cfg.CompanySlug || providerRepo != r.cfg.RepoSlug || repositoryFullName != r.cfg.CompanySlug+"/"+r.cfg.RepoSlug {
			mismatches = append(mismatches, "forgejo repository identity mismatch")
		}
		if !active {
			mismatches = append(mismatches, "active=false")
		}
		if len(mismatches) > 0 {
			*issues = append(*issues, "sandbox runner repository mismatch: "+strings.Join(mismatches, ", "))
			row.Status = "mismatch"
			row.Detail = strings.Join(mismatches, ", ")
		}
		return nil
	})
	if err != nil {
		row.Status = "error"
		row.Detail = err.Error()
		*issues = append(*issues, err.Error())
	}
	return row
}

func (r *platformRunner) checkGitHubDogfoodRunnerRepository(issues *[]string) platformBoundaryRow {
	row := platformBoundaryRow{Boundary: "sandbox_rental.github_runner_repository", Status: "ok"}
	err := r.withSpan("platform.github_runner.check", []attribute.KeyValue{
		attribute.String("db.system", "postgresql"),
		attribute.String("db.name", "sandbox_rental"),
		attribute.String("verself.org_id", r.cfg.PublicOrgIDText),
		attribute.Int64("github.installation_id", r.cfg.GitHubInstallationID),
		attribute.Int64("github.repository_id", r.cfg.GitHubRepositoryID),
	}, func(ctx context.Context) error {
		conn, err := r.openPG(ctx, "sandbox_rental")
		if err != nil {
			return err
		}
		defer func() { _ = conn.Close(context.Background()) }()
		ids := r.ids()
		var accountID int64
		var accountLogin, accountType string
		var installationActive, repoActive bool
		var connectionOrgID, connectionState string
		var repoOrgID string
		var projectID, sourceRepositoryID uuid.UUID
		var providerOwner, providerRepo, repositoryFullName string
		err = conn.QueryRow(ctx, `
SELECT
    ga.account_id,
    ga.account_login,
    ga.account_type,
    gi.active,
    gc.org_id,
    gc.state,
    rpr.org_id,
    COALESCE(rpr.project_id, '00000000-0000-0000-0000-000000000000'::uuid),
    COALESCE(rpr.source_repository_id, '00000000-0000-0000-0000-000000000000'::uuid),
    rpr.provider_owner,
    rpr.provider_repo,
    rpr.repository_full_name,
    rpr.active
FROM github_accounts ga
JOIN github_installations gi ON gi.account_id = ga.account_id
JOIN github_installation_connections gc ON gc.installation_id = gi.installation_id
JOIN runner_provider_repositories rpr ON rpr.provider = 'github'
WHERE gi.installation_id = $1
  AND gc.org_id = $2
  AND rpr.provider_repository_id = $3`,
			r.cfg.GitHubInstallationID,
			r.cfg.PublicOrgIDText,
			r.cfg.GitHubRepositoryID,
		).Scan(
			&accountID,
			&accountLogin,
			&accountType,
			&installationActive,
			&connectionOrgID,
			&connectionState,
			&repoOrgID,
			&projectID,
			&sourceRepositoryID,
			&providerOwner,
			&providerRepo,
			&repositoryFullName,
			&repoActive,
		)
		if errors.Is(err, pgx.ErrNoRows) {
			*issues = append(*issues, "github dogfood runner repository mapping is missing")
			row.Status = "missing"
			return nil
		}
		if err != nil {
			return fmt.Errorf("github dogfood: query: %w", err)
		}
		var mismatches []string
		if accountID != r.cfg.GitHubAccountID {
			mismatches = append(mismatches, fmt.Sprintf("account_id=%d", accountID))
		}
		if accountLogin != r.cfg.CompanySlug || accountType != "Organization" {
			mismatches = append(mismatches, "github account identity mismatch")
		}
		if !installationActive {
			mismatches = append(mismatches, "installation.active=false")
		}
		if connectionOrgID != r.cfg.PublicOrgIDText || connectionState != "active" {
			mismatches = append(mismatches, "installation connection mismatch")
		}
		if repoOrgID != r.cfg.PublicOrgIDText {
			mismatches = append(mismatches, fmt.Sprintf("runner_repository.org_id=%q", repoOrgID))
		}
		if projectID != ids.ProjectID {
			mismatches = append(mismatches, fmt.Sprintf("project_id=%s", projectID))
		}
		if sourceRepositoryID != ids.RepoID {
			mismatches = append(mismatches, fmt.Sprintf("source_repository_id=%s", sourceRepositoryID))
		}
		if providerOwner != r.cfg.CompanySlug || providerRepo != r.cfg.RepoSlug || repositoryFullName != r.cfg.CompanySlug+"/"+r.cfg.RepoSlug {
			mismatches = append(mismatches, "github repository identity mismatch")
		}
		if !repoActive {
			mismatches = append(mismatches, "runner_repository.active=false")
		}
		if len(mismatches) > 0 {
			*issues = append(*issues, "github dogfood runner repository mismatch: "+strings.Join(mismatches, ", "))
			row.Status = "mismatch"
			row.Detail = strings.Join(mismatches, ", ")
		}
		return nil
	})
	if err != nil {
		row.Status = "error"
		row.Detail = err.Error()
		*issues = append(*issues, err.Error())
	}
	return row
}

func (r *platformRunner) openPG(ctx context.Context, dbName string) (*pgx.Conn, error) {
	return oppg.OpenOverSSH(ctx, r.rt, oppg.Config{
		Database:     dbName,
		User:         r.opts.pgUser,
		RemotePort:   r.opts.pgRemotePort,
		PasswordPath: r.postgresSecretsPath(),
	})
}

func (r *platformRunner) postgresSecretsPath() string {
	if r.opts.secretsFile != "" {
		return r.opts.secretsFile
	}
	return opruntime.HostConfigurationSecretsPath(r.rt.RepoRoot, r.rt.Site)
}

func (r *platformRunner) forgejoClient(ctx context.Context) (platformForgejoClient, func(), error) {
	rawToken, err := opruntime.ReadRemoteFile(ctx, r.rt.SSH, r.opts.forgejoTokenPath)
	if err != nil {
		return platformForgejoClient{}, func() {}, fmt.Errorf("forgejo: read automation token: %w", err)
	}
	token := strings.TrimSpace(string(rawToken))
	if token == "" {
		return platformForgejoClient{}, func() {}, fmt.Errorf("forgejo: automation token is empty")
	}
	forward, err := r.rt.SSH.Forward(ctx, "forgejo-http", r.opts.forgejoRemoteAddr)
	if err != nil {
		return platformForgejoClient{}, func() {}, fmt.Errorf("forgejo: open HTTP forward: %w", err)
	}
	closeFn := func() { _ = forward.Close() }
	client := platformForgejoClient{
		BaseURL: "http://" + forward.ListenAddr,
		Token:   token,
		Client:  &http.Client{Timeout: 5 * time.Second},
	}
	return client, closeFn, nil
}

func (c platformForgejoClient) GetOrg(ctx context.Context, org string) (platformForgejoOrg, bool, error) {
	var out platformForgejoOrg
	err := c.doJSON(ctx, http.MethodGet, "/api/v1/orgs/"+url.PathEscape(org), nil, &out, http.StatusOK)
	var status forgejoStatusError
	if errors.As(err, &status) && status.Status == http.StatusNotFound {
		return platformForgejoOrg{}, false, nil
	}
	return out, err == nil, err
}

func (c platformForgejoClient) CreateOrg(ctx context.Context, body map[string]any) (platformForgejoOrg, error) {
	var out platformForgejoOrg
	err := c.doJSON(ctx, http.MethodPost, "/api/v1/orgs", body, &out, http.StatusCreated, http.StatusOK)
	return out, err
}

func (c platformForgejoClient) PatchOrg(ctx context.Context, org string, body map[string]any) (platformForgejoOrg, error) {
	var out platformForgejoOrg
	err := c.doJSON(ctx, http.MethodPatch, "/api/v1/orgs/"+url.PathEscape(org), body, &out, http.StatusOK)
	return out, err
}

func (c platformForgejoClient) GetRepo(ctx context.Context, owner, repo string) (platformForgejoRepo, bool, error) {
	var out platformForgejoRepo
	err := c.doJSON(ctx, http.MethodGet, "/api/v1/repos/"+url.PathEscape(owner)+"/"+url.PathEscape(repo), nil, &out, http.StatusOK)
	var status forgejoStatusError
	if errors.As(err, &status) && status.Status == http.StatusNotFound {
		return platformForgejoRepo{}, false, nil
	}
	return out, err == nil, err
}

func (c platformForgejoClient) CreateOrgRepo(ctx context.Context, owner string, body map[string]any) (platformForgejoRepo, error) {
	var out platformForgejoRepo
	err := c.doJSON(ctx, http.MethodPost, "/api/v1/orgs/"+url.PathEscape(owner)+"/repos", body, &out, http.StatusCreated, http.StatusOK)
	return out, err
}

func (c platformForgejoClient) PatchRepo(ctx context.Context, owner, repo string, body map[string]any) (platformForgejoRepo, error) {
	var out platformForgejoRepo
	err := c.doJSON(ctx, http.MethodPatch, "/api/v1/repos/"+url.PathEscape(owner)+"/"+url.PathEscape(repo), body, &out, http.StatusOK)
	return out, err
}

func (c platformForgejoClient) ListRepoHooks(ctx context.Context, owner, repo string) ([]platformForgejoHook, error) {
	var out []platformForgejoHook
	err := c.doJSON(ctx, http.MethodGet, "/api/v1/repos/"+url.PathEscape(owner)+"/"+url.PathEscape(repo)+"/hooks", nil, &out, http.StatusOK)
	return out, err
}

func (c platformForgejoClient) CreateRepoHook(ctx context.Context, owner, repo string, body map[string]any) error {
	return c.doJSON(ctx, http.MethodPost, "/api/v1/repos/"+url.PathEscape(owner)+"/"+url.PathEscape(repo)+"/hooks", body, nil, http.StatusCreated)
}

func (c platformForgejoClient) PatchRepoHook(ctx context.Context, owner, repo string, hookID int64, body map[string]any) error {
	if hookID <= 0 {
		return fmt.Errorf("forgejo hook id is required")
	}
	return c.doJSON(ctx, http.MethodPatch, "/api/v1/repos/"+url.PathEscape(owner)+"/"+url.PathEscape(repo)+"/hooks/"+strconv.FormatInt(hookID, 10), body, nil, http.StatusOK)
}

func platformForgejoHookBody(targetURL, secret string, includeType bool) map[string]any {
	body := map[string]any{
		"config": map[string]string{
			"url":          targetURL,
			"content_type": "json",
			"http_method":  "post",
			"secret":       secret,
		},
		"events": []string{"push", "pull_request", "schedule", "workflow_dispatch", "action_run_failure", "action_run_recover", "action_run_success"},
		"active": true,
	}
	if includeType {
		body["type"] = "forgejo"
	}
	return body
}

func (c platformForgejoClient) doJSON(ctx context.Context, method, path string, body any, out any, expected ...int) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.BaseURL, "/")+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "token "+c.Token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := c.Client
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("forgejo %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if !expectedStatus(resp.StatusCode, expected) {
		return forgejoStatusError{Method: method, Path: path, Status: resp.StatusCode, Body: string(data)}
	}
	if out == nil {
		return nil
	}
	if len(data) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("forgejo %s %s: decode response: %w", method, path, err)
	}
	return nil
}

func expectedStatus(status int, expected []int) bool {
	for _, item := range expected {
		if status == item {
			return true
		}
	}
	return false
}

func (r *platformRunner) withSpan(name string, attrs []attribute.KeyValue, fn func(context.Context) error) error {
	ctx, span := r.rt.Tracer.Start(r.rt.Ctx, name, trace.WithAttributes(attrs...))
	err := fn(ctx)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	} else {
		span.SetStatus(codes.Ok, "")
	}
	span.End()
	return err
}

func (r *platformRunner) markChanged(change string) {
	r.changes = append(r.changes, change)
}

func rollbackTx(ctx context.Context, tx pgx.Tx) {
	_ = tx.Rollback(ctx)
}

func writePlatformReport(w io.Writer, format string, report platformReport) error {
	switch format {
	case "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	case "text":
		changed := "none"
		if len(report.Changed) > 0 {
			changed = strings.Join(report.Changed, ",")
		}
		_, err := fmt.Fprintf(w,
			"platform %s ok: org=%s org_id=%s identity_provider_org_id=%s repo=%s/%s project_id=%s source_repo_id=%s forgejo_repo_id=%s git=%s changed=%s\n",
			report.Action,
			report.Config.CompanySlug,
			report.Config.PublicOrgIDText,
			report.Config.OrgIDText,
			report.Config.CompanySlug,
			report.Config.RepoSlug,
			report.ProjectID,
			report.RepoID,
			report.ForgejoRepoID,
			report.Config.CanonicalGitURL,
			changed,
		)
		return err
	default:
		return fmt.Errorf("unsupported platform report format %q", format)
	}
}
