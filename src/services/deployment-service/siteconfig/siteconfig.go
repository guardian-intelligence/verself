package siteconfig

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type Model struct {
	Site                                   string
	ProductDomain                          string
	CompanyDomain                          string
	ZitadelDomain                          string
	SpiffeTrustDomain                      string
	InstallationID                         string
	GitHubAppID                            string
	GitHubAppSlug                          string
	GitHubOAuthClientID                    string
	GitHubAppSettingsURL                   string
	GitHubRunnerClassPrefix                string
	DeployGitHubRepos                      string
	DeployGitHubRefs                       string
	DeployGitHubWorkflows                  string
	DeployRepoURL                          string
	ObjectStorageS3Endpoint                string
	ObjectStorageDeploymentArtifactsBucket string
	Domains                                map[string]string
}

func Load(repoRoot, site string) (Model, error) {
	path := filepath.Join(repoRoot, "src", "bootstrap", "sites", site, "vars.yml")
	body, err := os.ReadFile(path)
	if err != nil {
		return Model{}, fmt.Errorf("read %s: %w", path, err)
	}
	values := map[string]any{}
	if err := yaml.Unmarshal(body, &values); err != nil {
		return Model{}, fmt.Errorf("decode %s: %w", path, err)
	}
	required := map[string]string{}
	for _, key := range []string{"verself_site", "verself_domain", "company_domain", "spire_trust_domain", "verself_installation_id"} {
		value := resolveString(values, key)
		if value == "" {
			return Model{}, fmt.Errorf("%s: %s is required", path, key)
		}
		required[key] = value
	}
	model := Model{
		Site:              required["verself_site"],
		ProductDomain:     required["verself_domain"],
		CompanyDomain:     required["company_domain"],
		SpiffeTrustDomain: required["spire_trust_domain"],
		InstallationID:    required["verself_installation_id"],
		Domains:           map[string]string{},
	}
	if model.Site != site {
		return Model{}, fmt.Errorf("%s: verself_site=%q does not match selected site %q", path, model.Site, site)
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
	model.ObjectStorageS3Endpoint = resolveString(values, "object_storage_s3_endpoint")
	if err := validateHTTPSURL("object_storage_s3_endpoint", model.ObjectStorageS3Endpoint); err != nil {
		return Model{}, fmt.Errorf("%s: %w", path, err)
	}
	model.ObjectStorageDeploymentArtifactsBucket = resolveString(values, "object_storage_deployment_artifacts_bucket")
	if !isS3BucketName(model.ObjectStorageDeploymentArtifactsBucket) {
		return Model{}, fmt.Errorf("%s: object_storage_deployment_artifacts_bucket must be a valid lowercase S3 bucket name", path)
	}
	return model, nil
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

func validateHTTPSURL(field, value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("%s is required", field)
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return fmt.Errorf("%s=%q must be an HTTPS URL: %w", field, value, err)
	}
	if parsed.Scheme != "https" || parsed.Host == "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("%s=%q must be an HTTPS URL without query or fragment", field, value)
	}
	return nil
}

func isS3BucketName(value string) bool {
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
		"__VERSELF_SITE__":                                       m.Site,
		"__VERSELF_PRODUCT_DOMAIN__":                             m.ProductDomain,
		"__VERSELF_COMPANY_DOMAIN__":                             m.CompanyDomain,
		"__VERSELF_PRODUCT_BASE_URL__":                           productBase,
		"__VERSELF_COMPANY_BASE_URL__":                           companyBase,
		"__VERSELF_ZITADEL_DOMAIN__":                             m.ZitadelDomain,
		"__VERSELF_AUTH_ISSUER_URL__":                            "https://" + m.ZitadelDomain,
		"__VERSELF_SPIFFE_TRUST_DOMAIN__":                        m.SpiffeTrustDomain,
		"__VERSELF_SPIFFE_SERVICE_PREFIX__":                      "spiffe://" + m.SpiffeTrustDomain + "/svc",
		"__VERSELF_INSTALLATION_ID__":                            m.InstallationID,
		"__VERSELF_AGENT_SENDER_ADDRESS__":                       "agents@" + m.CompanyDomain,
		"__VERSELF_BILLING_RETURN_ORIGINS__":                     productBase,
		"__VERSELF_EMAIL_FROM_ADDRESS__":                         "noreply@" + m.domain("resend"),
		"__VERSELF_STALWART_PUBLIC_BASE_URL__":                   stalwartBase,
		"__VERSELF_STALWART_DOMAIN__":                            m.domain("stalwart"),
		"__VERSELF_SOURCE_PUBLIC_BASE_URL__":                     "https://" + m.domain("source_code_hosting_service"),
		"__VERSELF_GITHUB_OAUTH_REDIRECT_URL__":                  "https://" + m.domain("github_integration_service") + "/api/v1/github/user-authorizations/complete",
		"__VERSELF_ANALYTICS_GITHUB_OIDC_AUDIENCE__":             "https://" + m.domain("analytics_service"),
		"__VERSELF_OBJECT_STORAGE_S3_ENDPOINT__":                 m.ObjectStorageS3Endpoint,
		"__VERSELF_OBJECT_STORAGE_DEPLOYMENT_ARTIFACTS_BUCKET__": m.ObjectStorageDeploymentArtifactsBucket,
		"__VERSELF_DEPLOY_GITHUB_ALLOWED_REPOSITORIES__":         m.DeployGitHubRepos,
		"__VERSELF_DEPLOY_GITHUB_ALLOWED_REFS__":                 m.DeployGitHubRefs,
		"__VERSELF_DEPLOY_GITHUB_ALLOWED_WORKFLOW_REFS__":        m.DeployGitHubWorkflows,
		"__VERSELF_DEPLOY_REPO_URL__":                            m.DeployRepoURL,
		"__VERSELF_TEMPORAL_SYSTEM_ADMIN_IDS__":                  "spiffe://" + m.SpiffeTrustDomain + "/svc/temporal-server",
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
	tokens["__VERSELF_DISTRIBUTION_TRUSTED_BUILDERS__"] = "spiffe://" + m.SpiffeTrustDomain + "/svc/release-builder"
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
