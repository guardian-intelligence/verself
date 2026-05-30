package deploycontract

import (
	"fmt"
	"path/filepath"
	"strings"
)

type IntegrationCatalog struct {
	Version             string               `yaml:"version"`
	Site                string               `yaml:"site"`
	SecretStorePolicy   string               `yaml:"secret_store_policy"`
	ProviderProject     ProviderProject      `yaml:"provider_project"`
	BootstrapExceptions []BootstrapException `yaml:"bootstrap_exceptions"`
	Integrations        []Integration        `yaml:"integrations"`
}

type ProviderProject struct {
	Engine           string `yaml:"engine"`
	ProjectName      string `yaml:"project_name"`
	Directory        string `yaml:"directory"`
	Status           string `yaml:"status"`
	BootstrapCommand string `yaml:"bootstrap_command"`
}

type BootstrapException struct {
	Key            string   `yaml:"key"`
	Provider       string   `yaml:"provider"`
	Isolation      string   `yaml:"isolation"`
	CredentialKeys []string `yaml:"credential_keys"`
	StorageTargets []string `yaml:"storage_targets"`
	AllowedUses    []string `yaml:"allowed_uses"`
	Reason         string   `yaml:"reason"`
}

type Integration struct {
	Key             string                  `yaml:"key"`
	Provider        string                  `yaml:"provider"`
	Replacement     string                  `yaml:"replacement"`
	Owner           string                  `yaml:"owner"`
	Purpose         string                  `yaml:"purpose"`
	ProviderProject ProviderProjectRef      `yaml:"provider_project"`
	Credentials     []IntegrationCredential `yaml:"credentials"`
	PublicConfig    []string                `yaml:"public_config"`
	Generation      string                  `yaml:"generation"`
	Reason          string                  `yaml:"reason"`
	GammaPolicy     string                  `yaml:"gamma_policy"`
	MigrationCheck  string                  `yaml:"migration_check"`
}

type ProviderProjectRef struct {
	Engine      string `yaml:"engine"`
	ProjectName string `yaml:"project_name"`
	Service     string `yaml:"service"`
}

type IntegrationCredential struct {
	Key          string `yaml:"key"`
	Source       string `yaml:"source"`
	ExternalName string `yaml:"external_name"`
	Target       string `yaml:"target"`
	TargetStore  string `yaml:"target_store"`
	OpenBaoName  string `yaml:"openbao_name"`
	SiteVar      string `yaml:"site_var"`
	CatalogField string `yaml:"catalog_field"`
	Isolation    string `yaml:"isolation"`
}

func (v *Validator) validateIntegrationCatalog(rel, path string) {
	var doc IntegrationCatalog
	if !v.decode(rel, path, &doc) {
		return
	}
	if doc.Version != "verself.integrations.v1" {
		v.add(rel, "version must be verself.integrations.v1")
	}
	site := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	if doc.Site != site {
		v.add(rel, fmt.Sprintf("site must match filename %q", site))
	}
	if !siteNameRE.MatchString(doc.Site) {
		v.add(rel, "site must be a stable site key")
	}
	if doc.SecretStorePolicy != "" && doc.SecretStorePolicy != "openbao_only" {
		v.add(rel, "secret_store_policy must be openbao_only when set")
	}
	validateProviderProject(v, rel, "provider_project", doc.ProviderProject, false)
	for i, exception := range doc.BootstrapExceptions {
		validateBootstrapException(v, rel, i, exception)
	}
	seenIntegrations := map[string]bool{}
	for i, integration := range doc.Integrations {
		prefix := fmt.Sprintf("integrations[%d]", i)
		if !secretRE.MatchString(integration.Key) {
			v.add(rel, prefix+".key must be provider-qualified")
		}
		if seenIntegrations[integration.Key] {
			v.add(rel, prefix+".key is duplicated")
		}
		seenIntegrations[integration.Key] = true
		requireName(v, rel, prefix+".provider", integration.Provider)
		if strings.TrimSpace(integration.Owner) == "" {
			v.add(rel, prefix+".owner is required")
		}
		requireName(v, rel, prefix+".purpose", integration.Purpose)
		validateProviderProjectRef(v, rel, prefix+".provider_project", integration.ProviderProject)
		for j, credential := range integration.Credentials {
			validateIntegrationCredential(v, rel, fmt.Sprintf("%s.credentials[%d]", prefix, j), credential)
		}
		for j, public := range integration.PublicConfig {
			requireName(v, rel, fmt.Sprintf("%s.public_config[%d]", prefix, j), public)
		}
	}
}

func validateProviderProject(v *Validator, rel, prefix string, project ProviderProject, required bool) {
	empty := project == ProviderProject{}
	if empty && !required {
		return
	}
	if project.Engine != "stripe_projects" {
		v.add(rel, prefix+".engine must be stripe_projects")
	}
	if strings.TrimSpace(project.ProjectName) == "" {
		v.add(rel, prefix+".project_name is required")
	}
	if !strings.HasPrefix(strings.TrimSpace(project.Directory), "src/integrations/stripe-projects/sites/") {
		v.add(rel, prefix+".directory must be under src/integrations/stripe-projects/sites")
	}
	if strings.TrimSpace(project.Status) == "" {
		v.add(rel, prefix+".status is required")
	}
}

func validateProviderProjectRef(v *Validator, rel, prefix string, project ProviderProjectRef) {
	if project == (ProviderProjectRef{}) {
		return
	}
	if project.Engine != "stripe_projects" {
		v.add(rel, prefix+".engine must be stripe_projects")
	}
	if strings.TrimSpace(project.ProjectName) == "" {
		v.add(rel, prefix+".project_name is required")
	}
	if strings.TrimSpace(project.Service) == "" {
		v.add(rel, prefix+".service is required")
	}
}

func validateBootstrapException(v *Validator, rel string, index int, exception BootstrapException) {
	prefix := fmt.Sprintf("bootstrap_exceptions[%d]", index)
	if !secretRE.MatchString(exception.Key) {
		v.add(rel, prefix+".key must be provider-qualified")
	}
	requireName(v, rel, prefix+".provider", exception.Provider)
	if exception.Isolation != "bootstrap_shared" {
		v.add(rel, prefix+".isolation must be bootstrap_shared")
	}
	if len(exception.CredentialKeys) == 0 {
		v.add(rel, prefix+".credential_keys must not be empty")
	}
	for i, key := range exception.CredentialKeys {
		requireName(v, rel, fmt.Sprintf("%s.credential_keys[%d]", prefix, i), key)
	}
	if len(exception.StorageTargets) == 0 {
		v.add(rel, prefix+".storage_targets must not be empty")
	}
	for i, target := range exception.StorageTargets {
		validateStorageTarget(v, rel, fmt.Sprintf("%s.storage_targets[%d]", prefix, i), target)
	}
	if len(exception.AllowedUses) == 0 {
		v.add(rel, prefix+".allowed_uses must not be empty")
	}
	if strings.TrimSpace(exception.Reason) == "" {
		v.add(rel, prefix+".reason is required")
	}
}

func validateIntegrationCredential(v *Validator, rel, prefix string, credential IntegrationCredential) {
	if !secretRE.MatchString(credential.Key) {
		v.add(rel, prefix+".key must be provider-qualified")
	}
	if strings.TrimSpace(credential.Source) == "" {
		v.add(rel, prefix+".source is required")
	}
	if strings.TrimSpace(credential.Target) == "" {
		v.add(rel, prefix+".target is required")
	}
	if credential.TargetStore == "" {
		switch credential.Target {
		case "public_config", "provider_resource_id":
		default:
			v.add(rel, prefix+".target_store is required")
		}
	} else {
		validateStorageTarget(v, rel, prefix+".target_store", credential.TargetStore)
	}
	if credential.TargetStore == "site_openbao" || credential.TargetStore == "controller_openbao" {
		if credential.OpenBaoName == "" {
			v.add(rel, prefix+".openbao_name is required for OpenBao targets")
		} else if !secretRE.MatchString(credential.OpenBaoName) {
			v.add(rel, prefix+".openbao_name must match the runtime secret naming shape")
		}
	}
	if credential.Target == "public_config" && strings.TrimSpace(credential.SiteVar) == "" {
		v.add(rel, prefix+".site_var is required for public_config targets")
	}
	if credential.Isolation != "" && credential.Isolation != "bootstrap_shared" {
		v.add(rel, prefix+".isolation must be bootstrap_shared when set")
	}
}

func validateStorageTarget(v *Validator, rel, field, target string) {
	switch target {
	case "bootstrap_session", "controller_openbao", "site_openbao", "host_credstore", "runtime_secret", "product_kv", "public_config", "provider_resource_id", "provisioning_bootstrap":
	default:
		v.add(rel, field+" has unsupported storage target "+sortedJSON(target))
	}
}
