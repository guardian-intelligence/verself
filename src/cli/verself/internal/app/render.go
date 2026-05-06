package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type RenderOptions struct {
	RepoRoot        string
	Site            string
	Force           bool
	OptionOverrides []OptionOverride
}

func renderBootstrap(store *Store, company CompanyRecord, opts RenderOptions) (BootstrapRun, error) {
	if opts.RepoRoot == "" {
		opts.RepoRoot = "."
	}
	repoRoot, err := filepath.Abs(opts.RepoRoot)
	if err != nil {
		return BootstrapRun{}, err
	}
	if company.Site == "" {
		return BootstrapRun{}, errors.New("bootstrap render: site is required")
	}
	if _, err := ensureAllGeneratedSecrets(store, &company, false); err != nil {
		return BootstrapRun{}, err
	}
	if err := store.SaveCompany(company); err != nil {
		return BootstrapRun{}, err
	}
	renderCompany := company
	if opts.Site != "" {
		renderCompany.Site = opts.Site
	}
	for _, override := range opts.OptionOverrides {
		opt := classifyOption(renderCompany, override.Name)
		opt.Source = "bootstrap_override"
		opt.Sensitivity = "public"
		opt.Value = override.Value
		opt.UpdatedAt = time.Now().UTC()
		upsertOption(&renderCompany, opt)
	}
	if err := writeRenderedFiles(store, renderCompany, repoRoot, opts.Force); err != nil {
		return BootstrapRun{}, err
	}
	runID, err := randomHex(12)
	if err != nil {
		return BootstrapRun{}, err
	}
	run := BootstrapRun{
		Version:      1,
		RunID:        runID,
		Company:      renderCompany.Name,
		Site:         renderCompany.Site,
		RepoRoot:     repoRoot,
		ManifestPath: filepath.Join(repoRoot, ".verself", "bootstrap", "manifest.yaml"),
		CreatedAt:    time.Now().UTC(),
	}
	if err := writeJSONFile(store.paths.bootstrapRunPath(runID), run, 0o600); err != nil {
		return BootstrapRun{}, err
	}
	return run, nil
}

func writeRenderedFiles(store *Store, company CompanyRecord, repoRoot string, force bool) error {
	siteVarsPath := filepath.Join(repoRoot, "src", "host", "sites", company.Site, "vars.yml")
	siteVars, err := renderSiteVars(siteVarsPath, company)
	if err != nil {
		return err
	}
	files := map[string][]byte{
		filepath.Join(repoRoot, ".verself", "bootstrap", "manifest.yaml"): []byte(renderManifest(company)),
		filepath.Join(repoRoot, ".sops.yaml"):                             []byte(renderSOPSConfig(company)),
		siteVarsPath:                                                      []byte(siteVars),
		filepath.Join(repoRoot, "src", "host", "sites", company.Site, "provisioning.tfvars.json.template"): []byte(renderProvisioningTemplate(company)),
		filepath.Join(repoRoot, "README.md"): []byte(renderREADME(company)),
	}
	for path, data := range files {
		if err := writeRenderFile(path, data, 0o644, force); err != nil {
			return err
		}
	}
	if err := renderCLIEntrypoint(repoRoot, company, force); err != nil {
		return err
	}
	if err := renderSOPSBags(store, company, repoRoot, force); err != nil {
		return err
	}
	return nil
}

func writeRenderFile(path string, data []byte, mode os.FileMode, force bool) error {
	if !force {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("render would overwrite %s; pass --force", path)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return atomicWriteFile(path, data, mode)
}

func renderManifest(company CompanyRecord) string {
	var b strings.Builder
	b.WriteString("version: verself.bootstrap.v1\n")
	b.WriteString("site: " + yamlQuote(company.Site) + "\n")
	b.WriteString("inputs:\n")
	b.WriteString("  product_domain: " + yamlQuote(company.ProductDomain) + "\n")
	b.WriteString("  company_domain: " + yamlQuote(company.CompanyDomain) + "\n")
	b.WriteString("  company_name: " + yamlQuote(company.CompanyName) + "\n")
	b.WriteString("  owner_alias: " + yamlQuote(company.OwnerAlias) + "\n")
	b.WriteString("  owner_name: " + yamlQuote(company.OwnerName) + "\n")
	b.WriteString("  cli_name: " + yamlQuote(company.CLIName) + "\n")
	b.WriteString("  project: " + yamlQuote(company.Project) + "\n")
	b.WriteString("root_sops_key:\n")
	if company.RootSOPSKey != nil {
		b.WriteString("  env_key: " + yamlQuote(company.RootSOPSKey.EnvKey) + "\n")
		b.WriteString("  provider: " + yamlQuote(company.RootSOPSKey.Provider) + "\n")
		b.WriteString("  recipient: " + yamlQuote(company.RootSOPSKey.Recipient) + "\n")
		b.WriteString("  secret_ref: " + yamlQuote(envSecretRef(company.RootSOPSKey.Scope, company.RootSOPSKey.EnvKey)) + "\n")
		b.WriteString("  scope:\n")
		b.WriteString("    org: " + yamlQuote(company.RootSOPSKey.Scope.Org) + "\n")
		b.WriteString("    project: " + yamlQuote(company.RootSOPSKey.Scope.Project) + "\n")
		b.WriteString("    environment: " + yamlQuote(company.RootSOPSKey.Scope.Environment) + "\n")
	}
	b.WriteString("company_options:\n")
	for _, opt := range company.Options {
		b.WriteString("  - name: " + yamlQuote(opt.Name) + "\n")
		b.WriteString("    source: " + yamlQuote(opt.Source) + "\n")
		b.WriteString("    sensitivity: " + yamlQuote(opt.Sensitivity) + "\n")
		if opt.Provider != "" {
			b.WriteString("    provider: " + yamlQuote(opt.Provider) + "\n")
		}
		if opt.Kind != "" {
			b.WriteString("    kind: " + yamlQuote(opt.Kind) + "\n")
		}
		if opt.Purpose != "" {
			b.WriteString("    purpose: " + yamlQuote(opt.Purpose) + "\n")
		}
		if opt.RequiredBy != "" {
			b.WriteString("    required_by: " + yamlQuote(opt.RequiredBy) + "\n")
		}
		if len(opt.RenderTargets) > 0 {
			b.WriteString("    render_targets:\n")
			for _, target := range opt.RenderTargets {
				b.WriteString("      - " + yamlQuote(target) + "\n")
			}
		}
	}
	b.WriteString("derived:\n")
	b.WriteString("  owner_email: " + yamlQuote(company.OwnerEmail) + "\n")
	b.WriteString("  organization_name: " + yamlQuote(company.OrganizationName) + "\n")
	b.WriteString("  trust_tier: " + yamlQuote(company.TrustTier) + "\n")
	b.WriteString("  decoupled_from_verself_sh: true\n")
	b.WriteString("  substrate: customer_latitude_bare_metal\n")
	return b.String()
}

func renderSOPSConfig(company CompanyRecord) string {
	recipient := ""
	if company.RootSOPSKey != nil {
		recipient = company.RootSOPSKey.Recipient
	}
	return fmt.Sprintf(`# sops configuration for the generated %s installation.
# The private Age identity is stored outside the repo as %s.

creation_rules:
  - path_regex: \.sops\.yml$
    age: %s
`, company.CompanyName, rootSOPSKeyName, recipient)
}

func renderSiteVars(path string, company CompanyRecord) (string, error) {
	existing, err := os.ReadFile(path)
	if err == nil {
		return overlaySiteVars(string(existing), company), nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	domain := company.ProductDomain
	return fmt.Sprintf(`---
verself_site: %s
verself_domain: %s
company_domain: %s

billing_service_subdomain: billing.api
billing_service_domain: "{{ billing_service_subdomain }}.{{ verself_domain }}"
forgejo_subdomain: git
forgejo_domain: "{{ forgejo_subdomain }}.{{ verself_domain }}"
governance_service_subdomain: governance.api
governance_service_domain: "{{ governance_service_subdomain }}.{{ verself_domain }}"
iam_service_subdomain: iam.api
iam_service_domain: "{{ iam_service_subdomain }}.{{ verself_domain }}"
notifications_service_subdomain: notifications.api
notifications_service_domain: "{{ notifications_service_subdomain }}.{{ verself_domain }}"
projects_service_subdomain: projects.api
projects_service_domain: "{{ projects_service_subdomain }}.{{ verself_domain }}"
secrets_service_subdomain: secrets.api
secrets_service_domain: "{{ secrets_service_subdomain }}.{{ verself_domain }}"
source_code_hosting_service_subdomain: source.api
source_code_hosting_service_domain: "{{ source_code_hosting_service_subdomain }}.{{ verself_domain }}"
zitadel_subdomain: auth
zitadel_domain: "{{ zitadel_subdomain }}.{{ verself_domain }}"

platform_company_name: %s
platform_owner_name: %s
platform_owner_alias: %s
platform_owner_email: %s
platform_organization_name: %s
platform_trust_tier: %s

bootstrap_seed_codebase: verself-sh
bootstrap_runtime_substrate: customer_latitude_bare_metal
`, yamlQuote(company.Site), yamlQuote(domain), yamlQuote(company.CompanyDomain), yamlQuote(company.CompanyName), yamlQuote(company.OwnerName), yamlQuote(company.OwnerAlias), yamlQuote(company.OwnerEmail), yamlQuote(company.OrganizationName), yamlQuote(company.TrustTier)), nil
}

func overlaySiteVars(existing string, company CompanyRecord) string {
	values := siteVarOverlay(company)
	seen := make(map[string]bool, len(values))
	var b strings.Builder
	for _, line := range strings.SplitAfter(existing, "\n") {
		trimmed := strings.TrimSpace(strings.TrimSuffix(line, "\n"))
		key, _, ok := strings.Cut(trimmed, ":")
		if ok {
			if value, exists := values[key]; exists {
				b.WriteString(key + ": " + value)
				if strings.HasSuffix(line, "\n") {
					b.WriteString("\n")
				}
				seen[key] = true
				continue
			}
		}
		b.WriteString(line)
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		if !seen[key] {
			keys = append(keys, key)
		}
	}
	if len(keys) == 0 {
		return b.String()
	}
	if b.Len() > 0 && !strings.HasSuffix(b.String(), "\n") {
		b.WriteString("\n")
	}
	b.WriteString("\n# Generated by verself bootstrap.\n")
	sort.Strings(keys)
	for _, key := range keys {
		b.WriteString(key + ": " + values[key] + "\n")
	}
	return b.String()
}

func siteVarOverlay(company CompanyRecord) map[string]string {
	return map[string]string{
		"verself_site":                  yamlQuote(company.Site),
		"verself_domain":                yamlQuote(company.ProductDomain),
		"company_domain":                yamlQuote(company.CompanyDomain),
		"platform_company_slug":         yamlQuote(company.Name),
		"platform_company_display_name": yamlQuote(company.CompanyName),
		"platform_owner_name":           yamlQuote(company.OwnerName),
		"platform_owner_alias":          yamlQuote(company.OwnerAlias),
		"platform_owner_email":          yamlQuote(company.OwnerEmail),
		"platform_organization_name":    yamlQuote(company.OrganizationName),
		"platform_trust_tier":           yamlQuote(company.TrustTier),
		"bootstrap_seed_codebase":       yamlQuote("verself-sh"),
		"bootstrap_runtime_substrate":   yamlQuote("customer_latitude_bare_metal"),
	}
}

func renderProvisioningTemplate(company CompanyRecord) string {
	type template struct {
		CompanyDomain string `json:"company_domain"`
		ProductDomain string `json:"product_domain"`
		Site          string `json:"site"`
		Latitude      struct {
			ProjectID string `json:"project_id"`
			Region    string `json:"region"`
			Plan      string `json:"plan"`
		} `json:"latitude"`
	}
	var t template
	t.CompanyDomain = company.CompanyDomain
	t.ProductDomain = company.ProductDomain
	t.Site = company.Site
	t.Latitude.ProjectID = optionValue(company, "latitude.project_id")
	t.Latitude.Region = optionValue(company, "latitude.region")
	t.Latitude.Plan = optionValue(company, "latitude.plan")
	data, _ := json.MarshalIndent(t, "", "  ")
	return string(data) + "\n"
}

func renderREADME(company CompanyRecord) string {
	return fmt.Sprintf(`# %s

This repository is a generated seed copy of `+"`verself-sh`"+`. It is fully
decoupled from Verself's production installation after export.

Runtime substrate: customer-provisioned Latitude bare metal.

## First Commands

`+"```text"+`
./scripts/bootstrap-linux-amd64
bazelisk build //src/cli/%s:%s
./bazel-bin/src/cli/%s/%s env get %s --org %s --project %s --environment bootstrap
./bazel-bin/src/cli/%s/%s company inspect %s --json
./bazel-bin/src/cli/%s/%s env run --org %s --project %s --environment bootstrap -- aspect deploy --site=%s --sha=HEAD
`+"```"+`
`, company.CompanyName, company.CLIName, company.CLIName, company.CLIName, company.CLIName, rootSOPSKeyName, company.OrganizationName, company.Project, company.CLIName, company.CLIName, company.Name, company.CLIName, company.CLIName, company.OrganizationName, company.Project, company.Site)
}

func renderCLIEntrypoint(repoRoot string, company CompanyRecord, force bool) error {
	dir := filepath.Join(repoRoot, "src", "cli", company.CLIName)
	mainGo := fmt.Sprintf(`package main

import (
	"os"

	"github.com/verself/verself-cli/internal/app"
)

func main() {
	os.Exit(app.MainWithBinary(%q))
}
`, company.CLIName)
	build := fmt.Sprintf(`load("@io_bazel_rules_go//go:def.bzl", "go_binary", "go_library")

go_library(
    name = "%s_lib",
    srcs = ["main.go"],
    importpath = "github.com/verself/verself-cli/custom/%s",
    visibility = ["//visibility:private"],
    deps = ["//src/cli/verself/internal/app"],
)

go_binary(
    name = "%s",
    embed = [":%s_lib"],
    visibility = ["//visibility:public"],
)
`, company.CLIName, company.CLIName, company.CLIName, company.CLIName)
	if err := writeRenderFile(filepath.Join(dir, "main.go"), []byte(mainGo), 0o644, force); err != nil {
		return err
	}
	return writeRenderFile(filepath.Join(dir, "BUILD.bazel"), []byte(build), 0o644, force)
}

func renderSOPSBags(store *Store, company CompanyRecord, repoRoot string, force bool) error {
	bags, err := collectSecretBags(store, company)
	if err != nil {
		return err
	}
	for rel, values := range bags {
		path := filepath.Join(repoRoot, rel)
		if err := writeEncryptedSOPSBag(repoRoot, path, values, force); err != nil {
			return err
		}
	}
	return nil
}

func collectSecretBags(store *Store, company CompanyRecord) (map[string]map[string]string, error) {
	bags := map[string]map[string]string{
		"src/host/sites/" + company.Site + "/secrets/host.sops.yml":         {},
		"src/host/sites/" + company.Site + "/secrets/external.sops.yml":     {},
		"src/host/sites/" + company.Site + "/secrets/provisioning.sops.yml": {},
	}
	for _, secret := range company.Secrets {
		value, err := store.ReadCredential(secret.ValueRef)
		if err != nil {
			return nil, err
		}
		targets := secretRenderTargets(company.Site, secret)
		for _, target := range targets {
			if _, ok := bags[target]; !ok {
				bags[target] = map[string]string{}
			}
			bags[target][secretYAMLKey(secret.Key)] = value
		}
	}
	for _, opt := range company.Options {
		value := opt.Value
		if opt.ValueRef != "" {
			var err error
			value, err = store.ReadCredential(opt.ValueRef)
			if err != nil {
				return nil, err
			}
		}
		if value == "" || len(opt.RenderTargets) == 0 {
			continue
		}
		for _, target := range opt.RenderTargets {
			if _, ok := bags[target]; !ok {
				bags[target] = map[string]string{}
			}
			bags[target][secretYAMLKey(opt.Name)] = value
		}
	}
	return bags, nil
}

func secretRenderTargets(site string, secret CompanySecret) []string {
	spec := findSecretSpec(site, secret.Key)
	if len(spec.RenderTargets) > 0 {
		return spec.RenderTargets
	}
	return secret.RenderTargets
}

func writeEncryptedSOPSBag(repoRoot, path string, values map[string]string, force bool) error {
	var b strings.Builder
	b.WriteString("---\n")
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		b.WriteString(key + ": " + yamlQuote(values[key]) + "\n")
	}
	if !force {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("render would overwrite %s; pass --force", path)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.sops.yml")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write([]byte(b.String())); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	// SOPS encrypts in place, so use a temp path and rename only after success.
	cmd := exec.Command("sops", "encrypt", "-i", tmpName)
	cmd.Dir = repoRoot
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("sops encrypt %s: %w (%s)", path, err, strings.TrimSpace(stderr.String()))
	}
	return os.Rename(tmpName, path)
}

func optionValue(company CompanyRecord, name string) string {
	for _, opt := range company.Options {
		if opt.Name == name {
			return opt.Value
		}
	}
	return ""
}

func yamlQuote(v string) string {
	v = strings.ReplaceAll(v, `\`, `\\`)
	v = strings.ReplaceAll(v, `"`, `\"`)
	v = strings.ReplaceAll(v, "\n", `\n`)
	return `"` + v + `"`
}

func secretYAMLKey(key string) string {
	r := strings.NewReplacer(".", "_", "-", "_")
	return r.Replace(key)
}

func envSecretRef(scope EnvScope, key string) string {
	return "env://" + scope.Org + "/" + scope.Project + "/" + scope.Environment + "/" + key
}
