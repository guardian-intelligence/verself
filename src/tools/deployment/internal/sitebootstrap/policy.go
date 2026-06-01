package sitebootstrap

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/verself/deployment-tools/internal/deploycontract"
	"gopkg.in/yaml.v3"
)

type seedPolicy struct {
	keys map[string]seedKey
}

func loadSeedPolicy(repoRoot, site string) (seedPolicy, error) {
	root := strings.TrimSpace(repoRoot)
	if root == "" {
		wd, err := os.Getwd()
		if err != nil {
			return seedPolicy{}, fmt.Errorf("resolve working directory: %w", err)
		}
		root = wd
	}
	policy := fallbackSeedPolicy()
	catalogPath := filepath.Join(root, "src", "integrations", "catalog", "sites", site+".yml")
	if _, err := os.Stat(catalogPath); errors.Is(err, fs.ErrNotExist) {
		return policy, nil
	} else if err != nil {
		return seedPolicy{}, fmt.Errorf("stat %s: %w", catalogPath, err)
	}
	derived := seedPolicy{keys: map[string]seedKey{}}
	for key, meta := range generatedSeedKeys {
		derived.keys[key] = meta
	}
	for _, key := range []string{
		"nomad_artifact_getter_s3_access_key_id",
		"nomad_artifact_getter_s3_secret_access_key",
	} {
		derived.keys[key] = fallbackProvidedSeedKeys[key]
	}
	siteVars, err := loadSiteVars(root, site)
	if err != nil {
		return seedPolicy{}, err
	}
	if err := collectOwnerLocalSeedKeys(root, siteVars, derived.keys); err != nil {
		return seedPolicy{}, err
	}
	if err := collectCatalogSeedKeys(catalogPath, derived.keys); err != nil {
		return seedPolicy{}, err
	}
	if len(derived.providedKeys()) == 0 {
		return policy, nil
	}
	return derived, nil
}

func fallbackSeedPolicy() seedPolicy {
	policy := seedPolicy{keys: map[string]seedKey{}}
	for key, meta := range fallbackProvidedSeedKeys {
		policy.keys[key] = meta
	}
	for key, meta := range generatedSeedKeys {
		policy.keys[key] = meta
	}
	return policy
}

func loadSiteVars(root, site string) (map[string]string, error) {
	path := filepath.Join(root, "src", "host", "sites", site, "vars.yml")
	body, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	values := map[string]any{}
	if err := yaml.Unmarshal(body, &values); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	out := map[string]string{}
	for key, value := range values {
		out[key] = strings.TrimSpace(fmt.Sprint(value))
	}
	return out, nil
}

func collectOwnerLocalSeedKeys(root string, siteVars map[string]string, keys map[string]seedKey) error {
	for _, relRoot := range []string{"src/services", "src/infrastructure-components", "src/viteplus-monorepo/apps"} {
		absRoot := filepath.Join(root, filepath.FromSlash(relRoot))
		if _, err := os.Stat(absRoot); errors.Is(err, fs.ErrNotExist) {
			continue
		} else if err != nil {
			return fmt.Errorf("stat %s: %w", relRoot, err)
		}
		if err := filepath.WalkDir(absRoot, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || filepath.Base(filepath.Dir(path)) != "deploy" || filepath.Ext(path) != ".yml" {
				return nil
			}
			switch filepath.Base(path) {
			case "runtime-secrets.yml", "openbao.yml":
				var doc deploycontract.RuntimeSecretsFile
				if err := decodeYAMLFile(path, &doc); err != nil {
					return err
				}
				for _, seed := range doc.Seeds {
					addOwnerLocalSiteSecret(siteVars, keys, seed.SiteSecret)
				}
			case "credstore.yml":
				var doc deploycontract.CredstoreFile
				if err := decodeYAMLFile(path, &doc); err != nil {
					return err
				}
				for _, file := range doc.Files {
					addOwnerLocalSiteSecret(siteVars, keys, file.SiteSecret)
				}
			}
			return nil
		}); err != nil {
			return fmt.Errorf("walk %s: %w", relRoot, err)
		}
	}
	return nil
}

func loadRuntimeSiteSecretKeys(root string) ([]string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		wd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("resolve working directory: %w", err)
		}
		root = wd
	}
	seen := map[string]bool{}
	for _, relRoot := range []string{"src/services", "src/infrastructure-components", "src/viteplus-monorepo/apps"} {
		absRoot := filepath.Join(root, filepath.FromSlash(relRoot))
		if _, err := os.Stat(absRoot); errors.Is(err, fs.ErrNotExist) {
			continue
		} else if err != nil {
			return nil, fmt.Errorf("stat %s: %w", relRoot, err)
		}
		if err := filepath.WalkDir(absRoot, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || filepath.Base(filepath.Dir(path)) != "deploy" || filepath.Ext(path) != ".yml" {
				return nil
			}
			switch filepath.Base(path) {
			case "runtime-secrets.yml", "openbao.yml":
				var doc deploycontract.RuntimeSecretsFile
				if err := decodeYAMLFile(path, &doc); err != nil {
					return err
				}
				for _, seed := range doc.Seeds {
					key := strings.TrimSpace(seed.SiteSecret)
					if key != "" {
						seen[key] = true
					}
				}
			}
			return nil
		}); err != nil {
			return nil, fmt.Errorf("walk %s: %w", relRoot, err)
		}
	}
	out := make([]string, 0, len(seen))
	for key := range seen {
		out = append(out, key)
	}
	sort.Strings(out)
	return out, nil
}

func addOwnerLocalSiteSecret(siteVars map[string]string, keys map[string]seedKey, key string) {
	key = strings.TrimSpace(key)
	if key == "" {
		return
	}
	if strings.TrimSpace(siteVars[key]) != "" {
		return
	}
	if meta, ok := generatedSeedKeys[key]; ok {
		keys[key] = meta
		return
	}
	keys[key] = seedKey{Source: "provider_runtime"}
}

func collectCatalogSeedKeys(path string, keys map[string]seedKey) error {
	var catalog deploycontract.IntegrationCatalog
	if err := decodeYAMLFile(path, &catalog); err != nil {
		return err
	}
	for _, exception := range catalog.BootstrapExceptions {
		for _, key := range exception.CredentialKeys {
			addCatalogKey(keys, key, "provider_bootstrap")
		}
	}
	for _, integration := range catalog.Integrations {
		for _, credential := range integration.Credentials {
			switch credential.Target {
			case "public_config":
				addCatalogKey(keys, credential.SiteVar, "provider_public_config")
			case "provider_resource_id":
				addCatalogKey(keys, credential.CatalogField, "provider_resource_id")
			}
		}
	}
	return nil
}

func addCatalogKey(keys map[string]seedKey, key, source string) {
	key = strings.TrimSpace(key)
	if key == "" {
		return
	}
	if _, generated := generatedSeedKeys[key]; generated {
		return
	}
	keys[key] = seedKey{Source: source}
}

func decodeYAMLFile(path string, into any) error {
	body, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	dec := yaml.NewDecoder(bytes.NewReader(body))
	dec.KnownFields(true)
	if err := dec.Decode(into); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}

func (p seedPolicy) providedKeys() []string {
	var out []string
	for key, meta := range p.keys {
		if strings.HasPrefix(meta.Source, "generated") {
			continue
		}
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func (p seedPolicy) generatedKeys() []string {
	var out []string
	for key, meta := range p.keys {
		if strings.HasPrefix(meta.Source, "generated") {
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out
}
