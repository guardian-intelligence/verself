package siteconfig

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const cloudflareAccountConfigVersion = "verself.cloudflare.account.v1"

var postgresIdentifierRE = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

type CloudflareProvider struct {
	ControlPlaneSite          string
	AccountID                 string
	R2Endpoint                string
	DeploymentArtifactsBucket string
	RecoveryBucket            string
}

type PostgresBootstrap struct {
	ServiceDatabases []PostgresServiceDatabase
	PeerMappings     []PostgresPeerMapping
}

type PostgresServiceDatabase struct {
	Name  string `yaml:"name"`
	Owner string `yaml:"owner"`
}

type PostgresPeerMapping struct {
	SystemUser string `yaml:"system_user"`
	PGUser     string `yaml:"pg_user"`
}

type Model struct {
	Site                    string
	ProductDomain           string
	CompanyDomain           string
	ZitadelDomain           string
	ZitadelProductProjectID string
	SpiffeTrustDomain       string
	InstallationID          string
	GitHubAppID             string
	GitHubAppSlug           string
	GitHubOAuthClientID     string
	GitHubAppSettingsURL    string
	GitHubRunnerClassPrefix string
	DeployGitHubRepos       string
	DeployGitHubRefs        string
	DeployGitHubWorkflows   string
	DeployRepoURL           string
	CloudflareAccountID     string
	CloudflareR2Endpoint    string
	NomadArtifactBucket     string
	Domains                 map[string]string
	PostgresBootstrap       PostgresBootstrap
}

func LoadCloudflareProvider(repoRoot string) (CloudflareProvider, error) {
	path := filepath.Join(repoRoot, "src", "integrations", "cloudflare", "account.json")
	body, err := os.ReadFile(path)
	if err != nil {
		return CloudflareProvider{}, fmt.Errorf("read %s: %w", path, err)
	}
	var raw struct {
		Version          string `json:"version"`
		ControlPlaneSite string `json:"control_plane_site"`
		AccountID        string `json:"account_id"`
		R2               struct {
			DeploymentArtifactsBucket string `json:"deployment_artifacts_bucket"`
			RecoveryBucket            string `json:"recovery_bucket"`
		} `json:"r2"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return CloudflareProvider{}, fmt.Errorf("decode %s: %w", path, err)
	}
	if raw.Version != cloudflareAccountConfigVersion {
		return CloudflareProvider{}, fmt.Errorf("%s: version must be %s", path, cloudflareAccountConfigVersion)
	}
	if raw.ControlPlaneSite != "prod" {
		return CloudflareProvider{}, fmt.Errorf("%s: control_plane_site must be prod", path)
	}
	accountID := strings.ToLower(strings.TrimSpace(raw.AccountID))
	if !isCloudflareAccountID(accountID) {
		return CloudflareProvider{}, fmt.Errorf("%s: account_id must be a 32-character hex Cloudflare account ID", path)
	}
	deploymentBucket := strings.TrimSpace(raw.R2.DeploymentArtifactsBucket)
	if !isR2BucketName(deploymentBucket) {
		return CloudflareProvider{}, fmt.Errorf("%s: r2.deployment_artifacts_bucket must be a valid lowercase R2 bucket name", path)
	}
	recoveryBucket := strings.TrimSpace(raw.R2.RecoveryBucket)
	if !isR2BucketName(recoveryBucket) {
		return CloudflareProvider{}, fmt.Errorf("%s: r2.recovery_bucket must be a valid lowercase R2 bucket name", path)
	}
	return CloudflareProvider{
		ControlPlaneSite:          raw.ControlPlaneSite,
		AccountID:                 accountID,
		R2Endpoint:                "https://" + accountID + ".r2.cloudflarestorage.com",
		DeploymentArtifactsBucket: deploymentBucket,
		RecoveryBucket:            recoveryBucket,
	}, nil
}

func Load(repoRoot, site string) (Model, error) {
	path := filepath.Join(repoRoot, "src", "sites", site, "vars.yml")
	body, err := os.ReadFile(path)
	if err != nil {
		return Model{}, fmt.Errorf("read %s: %w", path, err)
	}
	values := map[string]any{}
	if err := yaml.Unmarshal(body, &values); err != nil {
		return Model{}, fmt.Errorf("decode %s: %w", path, err)
	}
	required := map[string]string{}
	for _, key := range []string{"verself_site", "verself_domain", "company_domain", "spire_trust_domain", "verself_installation_id", "zitadel_product_project_id"} {
		value := resolveString(values, key)
		if value == "" {
			return Model{}, fmt.Errorf("%s: %s is required", path, key)
		}
		required[key] = value
	}
	model := Model{
		Site:                    required["verself_site"],
		ProductDomain:           required["verself_domain"],
		CompanyDomain:           required["company_domain"],
		ZitadelProductProjectID: required["zitadel_product_project_id"],
		SpiffeTrustDomain:       required["spire_trust_domain"],
		InstallationID:          required["verself_installation_id"],
		Domains:                 map[string]string{},
	}
	if model.Site != site {
		return Model{}, fmt.Errorf("%s: verself_site=%q does not match selected site %q", path, model.Site, site)
	}
	cloudflare, err := LoadCloudflareProvider(repoRoot)
	if err != nil {
		return Model{}, err
	}
	if err := validateDNSName("verself_domain", model.ProductDomain); err != nil {
		return Model{}, fmt.Errorf("%s: %w", path, err)
	}
	if err := validateDNSName("company_domain", model.CompanyDomain); err != nil {
		return Model{}, fmt.Errorf("%s: %w", path, err)
	}
	if err := validateDNSName("spire_trust_domain", model.SpiffeTrustDomain); err != nil {
		return Model{}, fmt.Errorf("%s: %w", path, err)
	}
	if err := validateZitadelPublicID("zitadel_product_project_id", model.ZitadelProductProjectID); err != nil {
		return Model{}, fmt.Errorf("%s: %w", path, err)
	}
	zitadel := resolveString(values, "zitadel_domain")
	if zitadel == "" {
		zitadel = model.ProductDomain
	}
	model.ZitadelDomain = zitadel
	if err := validateDNSName("zitadel_domain", model.ZitadelDomain); err != nil {
		return Model{}, fmt.Errorf("%s: %w", path, err)
	}
	for _, key := range domainKeys() {
		domain := resolveString(values, key+"_domain")
		if domain == "" {
			subdomain := resolveString(values, key+"_subdomain")
			if subdomain != "" {
				domain = subdomain + "." + model.ProductDomain
			}
		}
		if domain != "" {
			if err := validateDNSName(key+"_domain", domain); err != nil {
				return Model{}, fmt.Errorf("%s: %w", path, err)
			}
			model.Domains[key] = domain
		}
	}
	model.Domains["product"] = model.ProductDomain
	model.Domains["company"] = model.CompanyDomain
	model.Domains["zitadel"] = model.ZitadelDomain
	model.GitHubAppID = resolveString(values, "github_integration_service_github_app_id")
	model.GitHubAppSlug = resolveString(values, "github_integration_service_github_app_slug")
	model.GitHubOAuthClientID = resolveString(values, "github_integration_service_github_app_client_id")
	model.GitHubAppSettingsURL = resolveString(values, "github_integration_service_github_app_settings_url")
	model.GitHubRunnerClassPrefix = resolveString(values, "github_integration_service_github_runner_class_prefix")
	model.DeployGitHubRepos = resolveString(values, "deployment_github_allowed_repositories")
	model.DeployGitHubRefs = resolveString(values, "deployment_github_allowed_refs")
	model.DeployGitHubWorkflows = resolveString(values, "deployment_github_allowed_workflow_refs")
	model.DeployRepoURL = resolveString(values, "deployment_repo_url")
	model.CloudflareAccountID = cloudflare.AccountID
	model.CloudflareR2Endpoint = cloudflare.R2Endpoint
	model.NomadArtifactBucket = cloudflare.DeploymentArtifactsBucket
	postgres, err := LoadPostgresBootstrap(repoRoot)
	if err != nil {
		return Model{}, err
	}
	model.PostgresBootstrap = postgres
	return model, nil
}

func LoadPostgresBootstrap(repoRoot string) (PostgresBootstrap, error) {
	roots := []string{
		"src/infrastructure-components",
		"src/integrations",
		"src/services",
		"src/viteplus-monorepo/apps",
	}
	out := PostgresBootstrap{}
	seenDBs := map[string]string{}
	seenPeers := map[string]bool{}
	for _, relRoot := range roots {
		root := filepath.Join(repoRoot, filepath.FromSlash(relRoot))
		if _, err := os.Stat(root); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return PostgresBootstrap{}, fmt.Errorf("stat %s: %w", root, err)
		}
		if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || filepath.Base(path) != "postgres.yml" || filepath.Base(filepath.Dir(path)) != "deploy" {
				return nil
			}
			body, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("read %s: %w", path, err)
			}
			var doc struct {
				ServiceDatabases []PostgresServiceDatabase `yaml:"postgresql_service_databases"`
				PeerMappings     []PostgresPeerMapping     `yaml:"postgresql_peer_mappings"`
			}
			if err := yaml.Unmarshal(body, &doc); err != nil {
				return fmt.Errorf("decode %s: %w", path, err)
			}
			for _, db := range doc.ServiceDatabases {
				if err := validatePostgresIdentifier(path, "postgresql_service_databases.name", db.Name); err != nil {
					return err
				}
				if err := validatePostgresIdentifier(path, "postgresql_service_databases.owner", db.Owner); err != nil {
					return err
				}
				if prior := seenDBs[db.Name]; prior != "" {
					return fmt.Errorf("%s: duplicate PostgreSQL service database %q previously declared by %s", path, db.Name, prior)
				}
				seenDBs[db.Name] = path
				out.ServiceDatabases = append(out.ServiceDatabases, db)
			}
			for _, mapping := range doc.PeerMappings {
				if err := validatePostgresIdentifier(path, "postgresql_peer_mappings.system_user", mapping.SystemUser); err != nil {
					return err
				}
				if err := validatePostgresIdentifier(path, "postgresql_peer_mappings.pg_user", mapping.PGUser); err != nil {
					return err
				}
				peerKey := mapping.SystemUser + "\x00" + mapping.PGUser
				if seenPeers[peerKey] {
					continue
				}
				seenPeers[peerKey] = true
				out.PeerMappings = append(out.PeerMappings, mapping)
			}
			return nil
		}); err != nil {
			return PostgresBootstrap{}, fmt.Errorf("walk %s: %w", relRoot, err)
		}
	}
	sort.Slice(out.ServiceDatabases, func(i, j int) bool {
		if out.ServiceDatabases[i].Name == out.ServiceDatabases[j].Name {
			return out.ServiceDatabases[i].Owner < out.ServiceDatabases[j].Owner
		}
		return out.ServiceDatabases[i].Name < out.ServiceDatabases[j].Name
	})
	sort.Slice(out.PeerMappings, func(i, j int) bool {
		if out.PeerMappings[i].SystemUser == out.PeerMappings[j].SystemUser {
			return out.PeerMappings[i].PGUser < out.PeerMappings[j].PGUser
		}
		return out.PeerMappings[i].SystemUser < out.PeerMappings[j].SystemUser
	})
	return out, nil
}

func validatePostgresIdentifier(path, field, value string) error {
	if !postgresIdentifierRE.MatchString(strings.TrimSpace(value)) {
		return fmt.Errorf("%s: %s must match %s", path, field, postgresIdentifierRE.String())
	}
	return nil
}

func resolveString(values map[string]any, key string) string {
	raw, ok := values[key]
	if !ok {
		return ""
	}
	value, ok := raw.(string)
	if !ok {
		return fmt.Sprint(raw)
	}
	return resolveTemplate(value, values)
}

func resolveTemplate(value string, values map[string]any) string {
	out := value
	for i := 0; i < 8; i++ {
		next := out
		for key, raw := range values {
			rawString, ok := raw.(string)
			if !ok {
				continue
			}
			if strings.Contains(rawString, "{{") {
				continue
			}
			next = strings.ReplaceAll(next, "{{ "+key+" }}", rawString)
			next = strings.ReplaceAll(next, "{{"+key+"}}", rawString)
		}
		if next == out {
			break
		}
		out = next
	}
	out = strings.TrimSpace(out)
	out = strings.TrimPrefix(out, "\"")
	out = strings.TrimSuffix(out, "\"")
	if strings.Contains(out, "{{") || strings.Contains(out, "}}") {
		return ""
	}
	return out
}

func validateDNSName(field, value string) error {
	if value == "" {
		return fmt.Errorf("%s is required", field)
	}
	if strings.ContainsAny(value, "/:@") {
		return fmt.Errorf("%s=%q must be a DNS name, not a URL or SPIFFE ID", field, value)
	}
	labels := strings.Split(value, ".")
	if len(labels) < 2 {
		return fmt.Errorf("%s=%q must contain at least two DNS labels", field, value)
	}
	for _, label := range labels {
		if label == "" {
			return fmt.Errorf("%s=%q has an empty DNS label", field, value)
		}
		if strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return fmt.Errorf("%s=%q has a DNS label starting or ending with '-'", field, value)
		}
		for _, r := range label {
			if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
				return fmt.Errorf("%s=%q contains unsupported DNS character %q", field, value, r)
			}
		}
	}
	return nil
}

func validateZitadelPublicID(field, value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("%s is required", field)
	}
	if len(value) > 200 {
		return fmt.Errorf("%s=%q must be 200 characters or less", field, value)
	}
	if strings.ContainsAny(value, " \t\r\n/:?#") {
		return fmt.Errorf("%s=%q contains an unsupported delimiter", field, value)
	}
	return nil
}

func isCloudflareAccountID(value string) bool {
	if len(value) != 32 {
		return false
	}
	for _, r := range value {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

func isR2BucketName(value string) bool {
	if len(value) < 3 || len(value) > 63 {
		return false
	}
	if value[0] == '-' || value[len(value)-1] == '-' {
		return false
	}
	for _, r := range value {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
			return false
		}
	}
	return true
}

func (m Model) TokenMap() map[string]string {
	productBase := "https://" + m.ProductDomain
	companyBase := "https://" + m.CompanyDomain
	stalwartBase := "https://" + m.domain("stalwart")
	tokens := map[string]string{
		"__VERSELF_SITE__":                                m.Site,
		"__VERSELF_PRODUCT_DOMAIN__":                      m.ProductDomain,
		"__VERSELF_COMPANY_DOMAIN__":                      m.CompanyDomain,
		"__VERSELF_PRODUCT_BASE_URL__":                    productBase,
		"__VERSELF_COMPANY_BASE_URL__":                    companyBase,
		"__VERSELF_ZITADEL_DOMAIN__":                      m.ZitadelDomain,
		"__VERSELF_ZITADEL_PRODUCT_PROJECT_ID__":          m.ZitadelProductProjectID,
		"__VERSELF_AUTH_ISSUER_URL__":                     "https://" + m.ZitadelDomain,
		"__VERSELF_CLOUDFLARE_ACCOUNT_ID__":               m.CloudflareAccountID,
		"__VERSELF_SPIFFE_TRUST_DOMAIN__":                 m.SpiffeTrustDomain,
		"__VERSELF_SPIFFE_SERVICE_PREFIX__":               "spiffe://" + m.SpiffeTrustDomain + "/svc",
		"__VERSELF_INSTALLATION_ID__":                     m.InstallationID,
		"__VERSELF_AGENT_SENDER_ADDRESS__":                "agents@" + m.CompanyDomain,
		"__VERSELF_BILLING_RETURN_ORIGINS__":              productBase,
		"__VERSELF_EMAIL_FROM_ADDRESS__":                  "noreply@" + m.domain("resend"),
		"__VERSELF_STALWART_PUBLIC_BASE_URL__":            stalwartBase,
		"__VERSELF_STALWART_DOMAIN__":                     m.domain("stalwart"),
		"__VERSELF_SOURCE_PUBLIC_BASE_URL__":              "https://" + m.domain("source_code_hosting_service"),
		"__VERSELF_GITHUB_OAUTH_REDIRECT_URL__":           "https://" + m.domain("github_integration_service") + "/api/v1/github/user-authorizations/complete",
		"__VERSELF_ANALYTICS_GITHUB_OIDC_AUDIENCE__":      "https://" + m.domain("analytics_service"),
		"__VERSELF_CLOUDFLARE_R2_ENDPOINT__":              m.CloudflareR2Endpoint,
		"__VERSELF_NOMAD_ARTIFACT_BUCKET__":               m.NomadArtifactBucket,
		"__VERSELF_DEPLOY_GITHUB_ALLOWED_REPOSITORIES__":  m.DeployGitHubRepos,
		"__VERSELF_DEPLOY_GITHUB_ALLOWED_REFS__":          m.DeployGitHubRefs,
		"__VERSELF_DEPLOY_GITHUB_ALLOWED_WORKFLOW_REFS__": m.DeployGitHubWorkflows,
		"__VERSELF_DEPLOY_REPO_URL__":                     m.DeployRepoURL,
		"__VERSELF_POSTGRESQL_BOOTSTRAP_IDENT_ROWS__":     m.PostgresBootstrap.RenderIdentRows(),
		"__VERSELF_POSTGRESQL_BOOTSTRAP_DATABASE_SQL__":   m.PostgresBootstrap.RenderDatabaseSQL(),
		"__VERSELF_TEMPORAL_SYSTEM_ADMIN_IDS__":           "spiffe://" + m.SpiffeTrustDomain + "/svc/temporal-server",
	}
	if m.GitHubAppID != "" && m.GitHubAppID != "0" {
		tokens["__VERSELF_GITHUB_APP_ID__"] = m.GitHubAppID
	}
	if m.GitHubAppSlug != "" {
		tokens["__VERSELF_GITHUB_APP_SLUG__"] = m.GitHubAppSlug
		tokens["__VERSELF_GITHUB_APP_SETUP_URL__"] = "https://github.com/apps/" + m.GitHubAppSlug + "/installations/new"
	}
	if m.GitHubOAuthClientID != "" {
		tokens["__VERSELF_GITHUB_OAUTH_CLIENT_ID__"] = m.GitHubOAuthClientID
	}
	if m.GitHubAppSettingsURL != "" {
		tokens["__VERSELF_GITHUB_APP_SETTINGS_URL__"] = m.GitHubAppSettingsURL
	}
	if m.GitHubRunnerClassPrefix != "" {
		tokens["__VERSELF_GITHUB_RUNNER_CLASS_PREFIX__"] = m.GitHubRunnerClassPrefix
	}
	tokens["__VERSELF_DISTRIBUTION_TRUSTED_BUILDERS__"] = strings.Join([]string{
		"spiffe://" + m.SpiffeTrustDomain + "/svc/release-builder",
		"spiffe://" + m.SpiffeTrustDomain + "/svc/deployment-service",
		"verself://site-bootstrap/" + m.Site,
	}, ",")
	tokens["__VERSELF_TEMPORAL_NAMESPACE_ROLES__"] = strings.Join([]string{
		"spiffe://" + m.SpiffeTrustDomain + "/svc/sandbox-rental-service|sandbox-rental-service|admin",
		"spiffe://" + m.SpiffeTrustDomain + "/svc/billing-service|billing-service|admin",
		"spiffe://" + m.SpiffeTrustDomain + "/svc/distribution-service|distribution-service|admin",
	}, ",")
	for key, domain := range m.Domains {
		tokenKey := "__VERSELF_" + strings.ToUpper(strings.ReplaceAll(key, "-", "_")) + "_DOMAIN__"
		tokens[tokenKey] = domain
		tokens[strings.TrimSuffix(tokenKey, "_DOMAIN__")+"_BASE_URL__"] = "https://" + domain
	}
	return tokens
}

func (p PostgresBootstrap) RenderIdentRows() string {
	var b strings.Builder
	b.WriteString("verself_services      postgres                 postgres")
	for _, mapping := range p.PeerMappings {
		if mapping.SystemUser == "postgres" && mapping.PGUser == "postgres" {
			continue
		}
		fmt.Fprintf(&b, "\nverself_services      %-24s %s", mapping.SystemUser, mapping.PGUser)
	}
	return b.String()
}

func (p PostgresBootstrap) RenderDatabaseSQL() string {
	if len(p.ServiceDatabases) == 0 {
		return ""
	}
	owners := make([]string, 0, len(p.ServiceDatabases))
	seenOwners := map[string]bool{}
	for _, db := range p.ServiceDatabases {
		if seenOwners[db.Owner] {
			continue
		}
		seenOwners[db.Owner] = true
		owners = append(owners, db.Owner)
	}
	sort.Strings(owners)

	var b strings.Builder
	b.WriteString("DO $$\nBEGIN")
	for _, owner := range owners {
		fmt.Fprintf(&b, `
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = '%s') THEN
    CREATE ROLE %s LOGIN;
  END IF;`, owner, owner)
	}
	b.WriteString("\nEND\n$$;\n")
	for _, db := range p.ServiceDatabases {
		fmt.Fprintf(&b, `
SELECT 'CREATE DATABASE %s OWNER %s'
WHERE NOT EXISTS (SELECT 1 FROM pg_database WHERE datname = '%s')\gexec
`, db.Name, db.Owner, db.Name)
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m Model) domain(key string) string {
	if value := m.Domains[key]; value != "" {
		return value
	}
	return key + "." + m.ProductDomain
}

func (m Model) SortedTokens() []string {
	tokens := m.TokenMap()
	keys := make([]string, 0, len(tokens))
	for key := range tokens {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func (m Model) ProductURL() *url.URL {
	u, _ := url.Parse("https://" + m.ProductDomain)
	return u
}

func domainKeys() []string {
	return []string{
		"analytics_service",
		"billing_service",
		"deployment_service",
		"distribution_service",
		"email_service",
		"forgejo",
		"github_integration_service",
		"governance_service",
		"iam_service",
		"notifications_service",
		"npm_registry",
		"pomerium",
		"profile_service",
		"projects_service",
		"resend",
		"sandbox_rental_service",
		"secrets_service",
		"source_code_hosting_service",
		"stalwart",
	}
}
