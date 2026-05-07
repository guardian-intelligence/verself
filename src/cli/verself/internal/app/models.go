package app

import "time"

const (
	currentCompanyVersion = 1
	envBootstrap          = "bootstrap"
	rootSOPSKeyName       = "VERSELF_SOPS_AGE_IDENTITY"
	defaultProject        = "verself"
	defaultTrustTier      = "platform"
)

type Config struct {
	Version       int    `json:"version"`
	ActiveCompany string `json:"active_company,omitempty"`
	ActiveProfile string `json:"active_profile,omitempty"`
}

type ProfileRecord struct {
	Version     int       `json:"version"`
	Name        string    `json:"name"`
	TokenRef    string    `json:"token_ref"`
	IAMURL      string    `json:"iam_url,omitempty"`
	ProjectsURL string    `json:"projects_url,omitempty"`
	SourceURL   string    `json:"source_url,omitempty"`
	SelectedOrg *OrgRef   `json:"selected_org,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type OrgRef struct {
	OrgID       string `json:"org_id"`
	Slug        string `json:"slug"`
	DisplayName string `json:"display_name"`
}

type CompanyRecord struct {
	Version          int             `json:"version"`
	Name             string          `json:"name"`
	Site             string          `json:"site"`
	ProductDomain    string          `json:"product_domain"`
	CompanyDomain    string          `json:"company_domain"`
	CompanyName      string          `json:"company_name"`
	OwnerAlias       string          `json:"owner_alias"`
	OwnerName        string          `json:"owner_name"`
	CLIName          string          `json:"cli_name"`
	Project          string          `json:"project"`
	OrganizationName string          `json:"organization_name"`
	OwnerEmail       string          `json:"owner_email"`
	TrustTier        string          `json:"trust_tier"`
	Options          []CompanyOption `json:"options,omitempty"`
	Secrets          []CompanySecret `json:"secrets,omitempty"`
	RootSOPSKey      *RootSOPSKey    `json:"root_sops_key,omitempty"`
	CreatedAt        time.Time       `json:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"`
}

type CompanyOption struct {
	Name          string    `json:"name"`
	Source        string    `json:"source"`
	Sensitivity   string    `json:"sensitivity"`
	ValueRef      string    `json:"value_ref,omitempty"`
	Value         string    `json:"value,omitempty"`
	Provider      string    `json:"provider,omitempty"`
	Kind          string    `json:"kind,omitempty"`
	Purpose       string    `json:"purpose,omitempty"`
	RenderTargets []string  `json:"render_targets,omitempty"`
	RequiredBy    string    `json:"required_by,omitempty"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type CompanySecret struct {
	Key           string    `json:"key"`
	Kind          string    `json:"kind"`
	Sensitivity   string    `json:"sensitivity"`
	ValueRef      string    `json:"value_ref"`
	RenderTargets []string  `json:"render_targets,omitempty"`
	RevealCommand string    `json:"reveal_command,omitempty"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type RootSOPSKey struct {
	EnvKey    string    `json:"env_key"`
	Provider  string    `json:"provider"`
	Scope     EnvScope  `json:"scope"`
	Recipient string    `json:"recipient"`
	SecretRef string    `json:"secret_ref"`
	UpdatedAt time.Time `json:"updated_at"`
}

type EnvScope struct {
	Org         string `json:"org"`
	Project     string `json:"project"`
	Environment string `json:"environment"`
}

type EnvStore struct {
	Version int                  `json:"version"`
	Scope   EnvScope             `json:"scope"`
	Values  map[string]EnvSecret `json:"values"`
}

type EnvSecret struct {
	Key         string    `json:"key"`
	ValueRef    string    `json:"value_ref"`
	Sensitivity string    `json:"sensitivity"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type BootstrapRun struct {
	Version      int       `json:"version"`
	RunID        string    `json:"run_id"`
	Company      string    `json:"company"`
	Site         string    `json:"site"`
	RepoRoot     string    `json:"repo_root"`
	ManifestPath string    `json:"manifest_path"`
	CreatedAt    time.Time `json:"created_at"`
}

type SecretSpec struct {
	Key           string
	Kind          string
	RenderTargets []string
	Generator     func() (string, error)
}

type OptionOverride struct {
	Name  string
	Value string
}
