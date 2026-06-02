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

	"github.com/verself/deployment-service/deploycontract"
	"gopkg.in/yaml.v3"
)

type seedPolicy struct {
	keys           map[string]seedKey
	controllerOnly map[string]seedKey
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
	catalogPath := filepath.Join(root, "src", "integrations", "catalog", "sites", site+".yml")
	catalogExists := true
	if _, err := os.Stat(catalogPath); errors.Is(err, fs.ErrNotExist) {
		catalogExists = false
	} else if err != nil {
		return seedPolicy{}, fmt.Errorf("stat %s: %w", catalogPath, err)
	}
	policy := seedPolicy{keys: map[string]seedKey{}, controllerOnly: map[string]seedKey{}}
	if catalogExists {
		for key, meta := range machineProvisionedSeedKeys {
			policy.keys[key] = meta
		}
	} else {
		policy = fallbackSeedPolicy()
	}
	if catalogExists {
		if err := collectCatalogSeedKeys(catalogPath, &policy); err != nil {
			return seedPolicy{}, err
		}
	}
	return policy, nil
}

func fallbackSeedPolicy() seedPolicy {
	policy := seedPolicy{keys: map[string]seedKey{}, controllerOnly: map[string]seedKey{}}
	for key, meta := range fallbackProvidedSeedKeys {
		policy.keys[key] = meta
	}
	for key, meta := range machineProvisionedSeedKeys {
		policy.keys[key] = meta
	}
	return policy
}

func collectCatalogSeedKeys(path string, policy *seedPolicy) error {
	var catalog deploycontract.IntegrationCatalog
	if err := decodeYAMLFile(path, &catalog); err != nil {
		return err
	}
	for _, exception := range catalog.BootstrapExceptions {
		if !bootstrapExceptionTargetsSiteSeed(exception) {
			for _, key := range exception.CredentialKeys {
				addCatalogKey(policy.controllerOnly, key, "controller_openbao")
			}
			continue
		}
		for _, key := range exception.CredentialKeys {
			addCatalogKey(policy.keys, key, "provider_bootstrap")
		}
	}
	for _, integration := range catalog.Integrations {
		for _, credential := range integration.Credentials {
			switch credential.Target {
			case "bootstrap_seed":
				addCatalogKey(policy.keys, credential.SiteVar, "machine_provisioned_"+integration.Provider)
			}
		}
	}
	return nil
}

func bootstrapExceptionTargetsSiteSeed(exception deploycontract.BootstrapException) bool {
	for _, target := range exception.StorageTargets {
		switch strings.TrimSpace(target) {
		case "site_openbao", "runtime_secret":
			return true
		}
	}
	return false
}

func addCatalogKey(keys map[string]seedKey, key, source string) {
	key = strings.TrimSpace(key)
	if key == "" {
		return
	}
	if machine, ok := machineProvisionedSeedKeys[key]; ok {
		keys[key] = machine
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

func (p seedPolicy) operatorProvidedKeys() []string {
	var out []string
	for key, meta := range p.keys {
		if strings.HasPrefix(meta.Source, "machine_provisioned") {
			continue
		}
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func (p seedPolicy) bootstrapMaterializedKeys() []string {
	out := make([]string, 0, len(p.keys))
	for key, meta := range p.keys {
		if meta.RequiredForBootstrap {
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out
}
