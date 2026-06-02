package sitebootstrap

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	SeedBundleVersion = "verself.site-bootstrap.seed.v1"
)

type SeedBundle struct {
	Version string            `json:"version" yaml:"version"`
	Site    string            `json:"site" yaml:"site"`
	Values  map[string]string `json:"values" yaml:"values"`
}

type MaterializeOptions struct {
	Site       string
	SeedPath   string
	VarsPath   string
	Evidence   string
	RepoRoot   string
	ForceWrite bool
}

type SeedTemplateOptions struct {
	Site       string
	OutputPath string
	RepoRoot   string
	ForceWrite bool
}

type Evidence struct {
	Version string            `json:"version"`
	Site    string            `json:"site"`
	Values  []EvidenceValue   `json:"values"`
	Outputs map[string]string `json:"outputs"`
}

type EvidenceValue struct {
	Key         string `json:"key"`
	Source      string `json:"source"`
	Fingerprint string `json:"sha256"`
}

type seedKey struct {
	Source               string
	RequiredForBootstrap bool
}

var fallbackProvidedSeedKeys = map[string]seedKey{}

var machineProvisionedSeedKeys = map[string]seedKey{
	"nomad_artifact_getter_s3_access_key_id":     {Source: "machine_provisioned_cloudflare_r2_control_plane"},
	"nomad_artifact_getter_s3_secret_access_key": {Source: "machine_provisioned_cloudflare_r2_control_plane"},
}

func WriteSeedTemplate(opts SeedTemplateOptions) error {
	site := strings.TrimSpace(opts.Site)
	if site == "" {
		return errors.New("site is required")
	}
	out := strings.TrimSpace(opts.OutputPath)
	if out == "" {
		return errors.New("output path is required")
	}
	bundle := SeedBundle{
		Version: SeedBundleVersion,
		Site:    site,
		Values:  map[string]string{},
	}
	policy, err := loadSeedPolicy(opts.RepoRoot, site)
	if err != nil {
		return err
	}
	for _, key := range policy.operatorProvidedKeys() {
		bundle.Values[key] = ""
	}
	body, err := yaml.Marshal(bundle)
	if err != nil {
		return fmt.Errorf("encode seed template: %w", err)
	}
	return writePrivateFile(out, body, opts.ForceWrite)
}

func ValidateSeedBundle(site, path string, repoRoot ...string) (Evidence, error) {
	bundle, err := readSeedBundle(path)
	if err != nil {
		return Evidence{}, err
	}
	root := ""
	if len(repoRoot) > 0 {
		root = repoRoot[0]
	}
	policy, err := loadSeedPolicy(root, site)
	if err != nil {
		return Evidence{}, err
	}
	return validateSeedBundle(site, path, bundle, policy)
}

func MaterializeSeedBundle(opts MaterializeOptions) (Evidence, error) {
	if strings.TrimSpace(opts.Site) == "" {
		return Evidence{}, errors.New("site is required")
	}
	if strings.TrimSpace(opts.SeedPath) == "" {
		return Evidence{}, errors.New("seed bundle path is required")
	}
	if strings.TrimSpace(opts.VarsPath) == "" {
		return Evidence{}, errors.New("vars output path is required")
	}
	if strings.TrimSpace(opts.Evidence) == "" {
		return Evidence{}, errors.New("evidence output path is required")
	}
	bundle, err := readSeedBundle(opts.SeedPath)
	if err != nil {
		return Evidence{}, err
	}
	policy, err := loadSeedPolicy(opts.RepoRoot, opts.Site)
	if err != nil {
		return Evidence{}, err
	}
	if _, err := validateSeedBundle(opts.Site, opts.SeedPath, bundle, policy); err != nil {
		return Evidence{}, err
	}
	values := map[string]string{}
	for key, value := range bundle.Values {
		values[key] = value
	}
	if err := validateCompleteValues(opts.Site, values, policy); err != nil {
		return Evidence{}, err
	}
	varsBody, err := json.MarshalIndent(values, "", "  ")
	if err != nil {
		return Evidence{}, fmt.Errorf("encode Ansible vars: %w", err)
	}
	varsBody = append(varsBody, '\n')
	if err := writePrivateFile(opts.VarsPath, varsBody, opts.ForceWrite); err != nil {
		return Evidence{}, err
	}

	outputs := map[string]string{
		"ansible_vars": opts.VarsPath,
	}
	evidence := buildEvidence(opts.Site, values, policy, outputs)
	evidenceBody, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		return Evidence{}, fmt.Errorf("encode evidence: %w", err)
	}
	evidenceBody = append(evidenceBody, '\n')
	if err := writePrivateFile(opts.Evidence, evidenceBody, opts.ForceWrite); err != nil {
		return Evidence{}, err
	}
	return evidence, nil
}

func readSeedBundle(path string) (SeedBundle, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return SeedBundle{}, fmt.Errorf("read seed bundle %s: %w", path, err)
	}
	dec := yaml.NewDecoder(bytes.NewReader(body))
	dec.KnownFields(true)
	var bundle SeedBundle
	if err := dec.Decode(&bundle); err != nil {
		return SeedBundle{}, fmt.Errorf("decode seed bundle %s: %w", path, err)
	}
	if bundle.Values == nil {
		bundle.Values = map[string]string{}
	}
	return bundle, nil
}

func validateSeedBundle(site, path string, bundle SeedBundle, policy seedPolicy) (Evidence, error) {
	if bundle.Version != SeedBundleVersion {
		return Evidence{}, fmt.Errorf("%s: version must be %s", path, SeedBundleVersion)
	}
	if strings.TrimSpace(bundle.Site) == "" {
		return Evidence{}, fmt.Errorf("%s: site is required", path)
	}
	if strings.TrimSpace(site) != bundle.Site {
		return Evidence{}, fmt.Errorf("%s: site %q does not match selected site %q", path, bundle.Site, site)
	}
	for key := range bundle.Values {
		if _, ok := policy.keys[key]; ok {
			continue
		}
		if _, ok := policy.controllerOnly[key]; ok {
			return Evidence{}, fmt.Errorf("%s: values.%s is controller-only bootstrap authority; import it into controller OpenBao instead of the bootstrap seed bundle", path, key)
		}
		return Evidence{}, fmt.Errorf("%s: values.%s is not a declared bootstrap seed key", path, key)
	}
	return buildEvidence(site, bundle.Values, policy, nil), nil
}

func validateCompleteValues(site string, values map[string]string, policy seedPolicy) error {
	missingBySource := map[string][]string{}
	for _, key := range policy.bootstrapMaterializedKeys() {
		if strings.TrimSpace(values[key]) == "" {
			missingBySource[policy.keys[key].Source] = append(missingBySource[policy.keys[key].Source], key)
		}
	}
	if len(missingBySource) > 0 {
		return fmt.Errorf("missing bootstrap values after materialization: %s", formatMissingBySource(missingBySource))
	}
	return nil
}

func formatMissingBySource(missing map[string][]string) string {
	sources := make([]string, 0, len(missing))
	for source := range missing {
		sources = append(sources, source)
	}
	sort.Strings(sources)
	parts := make([]string, 0, len(sources))
	for _, source := range sources {
		keys := append([]string(nil), missing[source]...)
		sort.Strings(keys)
		parts = append(parts, source+"="+strings.Join(keys, ","))
	}
	return strings.Join(parts, "; ")
}

func buildEvidence(site string, values map[string]string, policy seedPolicy, outputs map[string]string) Evidence {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := Evidence{
		Version: SeedBundleVersion,
		Site:    site,
		Outputs: map[string]string{},
	}
	for key, value := range outputs {
		out.Outputs[key] = value
	}
	for _, key := range keys {
		value := values[key]
		source := "provided"
		if meta, ok := policy.keys[key]; ok {
			source = meta.Source
		}
		digest := sha256.Sum256([]byte(value))
		out.Values = append(out.Values, EvidenceValue{
			Key:         key,
			Source:      source,
			Fingerprint: hex.EncodeToString(digest[:]),
		})
	}
	return out
}

func writePrivateFile(path string, body []byte, force bool) error {
	path = filepath.Clean(path)
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to write through symlink %s", path)
		}
		if !force {
			return fmt.Errorf("%s already exists; pass --force to overwrite", path)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat %s: %w", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	_, writeErr := f.Write(body)
	closeErr := f.Close()
	if writeErr != nil {
		return fmt.Errorf("write %s: %w", path, writeErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close %s: %w", path, closeErr)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("chmod %s: %w", path, err)
	}
	return nil
}
