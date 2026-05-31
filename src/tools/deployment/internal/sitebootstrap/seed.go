package sitebootstrap

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
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

const SeedBundleVersion = "verself.site-bootstrap.seed.v1"

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
	ForceWrite bool
}

type Evidence struct {
	Version string             `json:"version"`
	Site    string             `json:"site"`
	Values  []EvidenceValue    `json:"values"`
	Outputs map[string]string  `json:"outputs"`
	Missing []MissingSeedValue `json:"missing,omitempty"`
}

type EvidenceValue struct {
	Key         string `json:"key"`
	Source      string `json:"source"`
	Fingerprint string `json:"sha256"`
}

type MissingSeedValue struct {
	Key    string `json:"key"`
	Source string `json:"source"`
}

type seedKey struct {
	Source string
}

var fallbackProvidedSeedKeys = map[string]seedKey{
	"cloudflare_api_token": {Source: "provider_bootstrap"},

	"stripe_secret_key":                                         {Source: "provider_runtime"},
	"stripe_webhook_secret":                                     {Source: "provider_runtime"},
	"stripe_publishable_key":                                    {Source: "provider_public_config"},
	"stripe_test_webhook_endpoint_id":                           {Source: "provider_resource_id"},
	"resend_api_key":                                            {Source: "provider_runtime"},
	"github_integration_service_github_app_private_key":         {Source: "provider_runtime"},
	"github_integration_service_github_app_webhook_secret":      {Source: "provider_runtime"},
	"github_integration_service_github_app_oauth_client_secret": {Source: "provider_runtime"},
}

var generatedSeedKeys = map[string]seedKey{
	"postgresql_admin_password":           {Source: "generated_host"},
	"postgresql_billing_password":         {Source: "generated_host"},
	"postgresql_sandbox_rental_password":  {Source: "generated_host"},
	"postgresql_iam_service_password":     {Source: "generated_host"},
	"postgresql_email_service_password":   {Source: "generated_host"},
	"zitadel_db_password":                 {Source: "generated_host"},
	"zitadel_masterkey":                   {Source: "generated_host"},
	"zitadel_admin_password":              {Source: "generated_host"},
	"stalwart_admin_password":             {Source: "generated_host"},
	"platform_agent_password":             {Source: "generated_host"},
	"iam_service_email_identity_hmac_key": {Source: "generated_runtime"},
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
	policy, err := loadSeedPolicy("", site)
	if err != nil {
		return err
	}
	for _, key := range policy.providedKeys() {
		bundle.Values[key] = ""
	}
	body, err := yaml.Marshal(bundle)
	if err != nil {
		return fmt.Errorf("encode seed template: %w", err)
	}
	return writePrivateFile(out, body, opts.ForceWrite)
}

func ValidateSeedBundle(site, path string) (Evidence, error) {
	bundle, err := readSeedBundle(path)
	if err != nil {
		return Evidence{}, err
	}
	policy, err := loadSeedPolicy("", site)
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
	existingValues, err := readExistingVars(opts.VarsPath)
	if err != nil {
		return Evidence{}, err
	}
	generated := map[string]bool{}
	for _, key := range policy.generatedKeys() {
		if strings.TrimSpace(values[key]) == "" {
			if strings.TrimSpace(existingValues[key]) != "" {
				values[key] = existingValues[key]
				continue
			}
			value, err := generateSecret(key)
			if err != nil {
				return Evidence{}, err
			}
			values[key] = value
			generated[key] = true
		}
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

	evidence := buildEvidence(opts.Site, values, generated, policy, map[string]string{
		"ansible_vars": opts.VarsPath,
	})
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

func readExistingVars(path string) (map[string]string, error) {
	body, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read existing vars %s: %w", path, err)
	}
	values := map[string]string{}
	if err := yaml.Unmarshal(body, &values); err != nil {
		return nil, fmt.Errorf("decode existing vars %s: %w", path, err)
	}
	return values, nil
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
		return Evidence{}, fmt.Errorf("%s: values.%s is not a declared bootstrap seed key", path, key)
	}
	var missing []MissingSeedValue
	for _, key := range policy.providedKeys() {
		if strings.TrimSpace(bundle.Values[key]) == "" {
			missing = append(missing, MissingSeedValue{Key: key, Source: policy.keys[key].Source})
		}
	}
	if len(missing) > 0 {
		evidence := Evidence{
			Version: SeedBundleVersion,
			Site:    bundle.Site,
			Missing: missing,
		}
		return evidence, fmt.Errorf("%s: missing %d required provider seed values", path, len(missing))
	}
	if err := validateProviderIsolation(site, bundle.Values); err != nil {
		return Evidence{}, fmt.Errorf("%s: %w", path, err)
	}
	return buildEvidence(site, bundle.Values, nil, policy, nil), nil
}

func validateCompleteValues(site string, values map[string]string, policy seedPolicy) error {
	var missing []string
	for _, key := range policy.providedKeys() {
		if strings.TrimSpace(values[key]) == "" {
			missing = append(missing, key)
		}
	}
	for _, key := range policy.generatedKeys() {
		if strings.TrimSpace(values[key]) == "" {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing bootstrap values after materialization: %s", strings.Join(missing, ", "))
	}
	if len(values["zitadel_masterkey"]) != 32 {
		return fmt.Errorf("zitadel_masterkey must be exactly 32 bytes")
	}
	return validateProviderIsolation(site, values)
}

func validateProviderIsolation(site string, values map[string]string) error {
	if site == "prod" {
		return nil
	}
	if strings.HasPrefix(values["stripe_secret_key"], "sk_live_") || strings.HasPrefix(values["stripe_secret_key"], "rk_live_") {
		return fmt.Errorf("non-prod site %s cannot use a live-mode Stripe secret key", site)
	}
	if strings.HasPrefix(values["stripe_publishable_key"], "pk_live_") {
		return fmt.Errorf("non-prod site %s cannot use a live-mode Stripe publishable key", site)
	}
	return nil
}

func buildEvidence(site string, values map[string]string, generated map[string]bool, policy seedPolicy, outputs map[string]string) Evidence {
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
		if generated[key] {
			source = "generated"
		} else if meta, ok := policy.keys[key]; ok {
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

func generateSecret(key string) (string, error) {
	size := 32
	switch key {
	case "zitadel_masterkey":
		size = 24 // RawURLEncoding produces the 32-byte ASCII key Zitadel requires.
	case "iam_service_email_identity_hmac_key":
		size = 64
	}
	raw := make([]byte, size)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate %s: %w", key, err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
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
