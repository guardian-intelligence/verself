package sitebootstrap

import (
	"bytes"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const secretInputFilePermMask = 0o077

var bootstrapCredentialGroups = map[string][]string{
	"GitHub App": {
		"github-integration-service.github.private_key",
		"github-integration-service.github.webhook_secret",
		"github-integration-service.github.oauth_client_secret",
	},
	"GitHub login": {
		"auth-control-plane.github_login.oauth_client_secret",
	},
	"Stripe billing": {
		"billing-service.stripe.secret_key",
		"billing-service.stripe.webhook_secret",
	},
}

type bootstrapCredentialFile struct {
	Site                 string                             `yaml:"site"`
	Cloudflare           bootstrapCloudflareCredentialInput `yaml:"cloudflare"`
	OpenBaoRuntimeSecret map[string]string                  `yaml:"openbao_runtime_secrets"`
}

type bootstrapCredentialInput struct {
	OpenBaoRuntimeSecrets map[string]string
	Cloudflare            *bootstrapCloudflareObjectStorageInput
}

type bootstrapCloudflareCredentialInput struct {
	AccountAdminAPIToken string `yaml:"account_admin_api_token"`
	AccountID            string `yaml:"account_id"`
	ObjectStorageBucket  string `yaml:"object_storage_bucket"`
	ChildTokenTTL        string `yaml:"child_token_ttl"`
}

type bootstrapCloudflareObjectStorageInput struct {
	AccountAdminAPIToken string
	AccountID            string
	ObjectStorageBucket  string
	ChildTokenTTL        time.Duration
}

func loadBootstrapCredentialInput(site, path string, catalog runtimeSecretCatalog) (bootstrapCredentialInput, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return bootstrapCredentialInput{}, fmt.Errorf("bootstrap credentials file is required")
	}
	var file bootstrapCredentialFile
	if err := decodeSecretInputYAML(path, &file); err != nil {
		return bootstrapCredentialInput{}, err
	}
	file.Site = strings.TrimSpace(file.Site)
	if file.Site == "" {
		return bootstrapCredentialInput{}, fmt.Errorf("%s: site is required", path)
	}
	if file.Site != site {
		return bootstrapCredentialInput{}, fmt.Errorf("%s: site %q does not match --site=%s", path, file.Site, site)
	}
	cloudflare, err := validateCloudflareBootstrapInput(path, file.Cloudflare)
	if err != nil {
		return bootstrapCredentialInput{}, err
	}
	if len(file.OpenBaoRuntimeSecret) == 0 && cloudflare == nil {
		return bootstrapCredentialInput{}, fmt.Errorf("%s: openbao_runtime_secrets or cloudflare must contain at least one bootstrap input", path)
	}
	imports := map[string]string{}
	for rawName, rawValue := range file.OpenBaoRuntimeSecret {
		name := strings.TrimSpace(rawName)
		value := strings.TrimSpace(rawValue)
		if name == "" {
			return bootstrapCredentialInput{}, fmt.Errorf("%s: openbao_runtime_secrets contains an empty secret name", path)
		}
		if value == "" {
			return bootstrapCredentialInput{}, fmt.Errorf("%s: openbao_runtime_secrets.%s is empty", path, name)
		}
		declaration, ok := catalog.ByName[name]
		if !ok {
			return bootstrapCredentialInput{}, fmt.Errorf("%s: openbao_runtime_secrets.%s is not declared by any deploy/runtime-secrets.yml", path, name)
		}
		if !declaration.ExternalOpenBao {
			return bootstrapCredentialInput{}, fmt.Errorf("%s: openbao_runtime_secrets.%s is not marked external_openbao", path, name)
		}
		imports[name] = value
	}
	if err := validateBootstrapCredentialGroups(path, imports); err != nil {
		return bootstrapCredentialInput{}, err
	}
	return bootstrapCredentialInput{
		OpenBaoRuntimeSecrets: imports,
		Cloudflare:            cloudflare,
	}, nil
}

func loadBootstrapCredentialImports(site, path string, catalog runtimeSecretCatalog) (map[string]string, error) {
	input, err := loadBootstrapCredentialInput(site, path, catalog)
	return input.OpenBaoRuntimeSecrets, err
}

func validateCloudflareBootstrapInput(path string, raw bootstrapCloudflareCredentialInput) (*bootstrapCloudflareObjectStorageInput, error) {
	raw.AccountAdminAPIToken = strings.TrimSpace(raw.AccountAdminAPIToken)
	raw.AccountID = strings.TrimSpace(raw.AccountID)
	raw.ObjectStorageBucket = strings.TrimSpace(raw.ObjectStorageBucket)
	raw.ChildTokenTTL = strings.TrimSpace(raw.ChildTokenTTL)
	if raw.AccountAdminAPIToken == "" && raw.AccountID == "" && raw.ObjectStorageBucket == "" && raw.ChildTokenTTL == "" {
		return nil, nil
	}
	missing := []string{}
	if raw.AccountAdminAPIToken == "" {
		missing = append(missing, "cloudflare.account_admin_api_token")
	}
	if raw.AccountID == "" {
		missing = append(missing, "cloudflare.account_id")
	}
	if raw.ObjectStorageBucket == "" {
		missing = append(missing, "cloudflare.object_storage_bucket")
	}
	if raw.ChildTokenTTL == "" {
		missing = append(missing, "cloudflare.child_token_ttl")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("%s: incomplete Cloudflare bootstrap input; missing %s", path, strings.Join(missing, ", "))
	}
	ttl, err := time.ParseDuration(raw.ChildTokenTTL)
	if err != nil {
		return nil, fmt.Errorf("%s: cloudflare.child_token_ttl must be a Go duration: %w", path, err)
	}
	if ttl <= 0 {
		return nil, fmt.Errorf("%s: cloudflare.child_token_ttl must be positive", path)
	}
	return &bootstrapCloudflareObjectStorageInput{
		AccountAdminAPIToken: raw.AccountAdminAPIToken,
		AccountID:            raw.AccountID,
		ObjectStorageBucket:  raw.ObjectStorageBucket,
		ChildTokenTTL:        ttl,
	}, nil
}

func decodeSecretInputYAML(path string, out any) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("stat secret input file %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("secret input file %s must not be a symlink", path)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("secret input file %s must be a regular file", path)
	}
	if info.Mode().Perm()&secretInputFilePermMask != 0 {
		return fmt.Errorf("secret input file %s must be mode 0600 or stricter", path)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read secret input file %s: %w", path, err)
	}
	decoder := yaml.NewDecoder(bytes.NewReader(body))
	decoder.KnownFields(true)
	if err := decoder.Decode(out); err != nil {
		return fmt.Errorf("decode secret input file %s: %w", path, err)
	}
	return nil
}

func validateBootstrapCredentialGroups(path string, imports map[string]string) error {
	groupNames := make([]string, 0, len(bootstrapCredentialGroups))
	for group := range bootstrapCredentialGroups {
		groupNames = append(groupNames, group)
	}
	sort.Strings(groupNames)
	for _, group := range groupNames {
		names := bootstrapCredentialGroups[group]
		present := []string{}
		missing := []string{}
		for _, name := range names {
			if strings.TrimSpace(imports[name]) == "" {
				missing = append(missing, name)
			} else {
				present = append(present, name)
			}
		}
		if len(present) > 0 && len(missing) > 0 {
			return fmt.Errorf("%s: incomplete %s credential group; provided %s but missing %s", path, group, strings.Join(present, ", "), strings.Join(missing, ", "))
		}
	}
	return nil
}
