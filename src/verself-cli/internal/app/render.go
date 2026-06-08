package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
	siteFactsPath := filepath.Join(repoRoot, "src", "guardian-specification", "examples", company.Site, "facts.cue")
	files := map[string][]byte{
		filepath.Join(repoRoot, ".verself", "bootstrap", "manifest.yaml"): []byte(renderManifest(company)),
		siteFactsPath: []byte(renderSiteFacts(company)),
		filepath.Join(repoRoot, "src", "tools", "provisioning", "sites", company.Site, "terraform.tfvars.json.template"): []byte(renderProvisioningTemplate(company)),
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

func renderSiteFacts(company CompanyRecord) string {
	return fmt.Sprintf(`package sitefacts

site: %s
installationID: ""

domains: {
	product: %s
	company: %s
}

cloudflare: {
	productZone: %s
	companyZone: %s
	dnsRecords: []
}

bareMetal: {
	hostAlias: ""
	publicIPv4: "0.0.0.0"
}

spiffe: trustDomain: %s

deployment: {
	githubAllowedRepositories: ""
	githubAllowedRefs: ""
	githubAllowedWorkflowRefs: ""
	repoURL: "https://github.com/guardian-intelligence/verself.git"
}

objectStorage: {
	s3Endpoint: ""
	deploymentArtifactsBucket: ""
}

githubIntegration: {
	appID: "0"
	appSlug: ""
	oauthClientID: ""
	appSettingsURL: ""
	runnerClassPrefix: ""
}

dogfood: {
	orgID: ""
	serviceDiscoveryCanaryOrgSlug: %s
}

bootstrap: {
	sourceCodebase: "verself-sh"
	runtimeSubstrate: "customer_latitude_bare_metal"
}

serviceSubdomains: {
	"billing_service": "billing.api"
	"forgejo": "git"
	"governance_service": "governance.api"
	"iam_service": "iam.api"
	"notifications_service": "notifications.api"
	"projects_service": "projects.api"
	"secrets_service": "secrets.api"
	"source_code_hosting_service": "source.api"
}
`, yamlQuote(company.Site), yamlQuote(company.ProductDomain), yamlQuote(company.CompanyDomain), yamlQuote(company.ProductDomain), yamlQuote(company.CompanyDomain), yamlQuote(company.ProductDomain), yamlQuote(company.Name))
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

This repository is a generated source copy of `+"`verself-sh`"+`. It is fully
decoupled from Verself's production installation after export.

Runtime substrate: customer-provisioned Latitude bare metal.

## First Commands

`+"```text"+`
./src/tools/dev/bootstrap/bootstrap-linux-amd64
export PATH="${HOME}/.cache/verself/bootstrap-bin:${PATH}"
aspect dev install --install-shims --bin-dir="${HOME}/.local/bin"
export PATH="${HOME}/.local/bin:${PATH}"
guardian run bazel -- build //src/%s-cli:%s
./bazel-bin/src/%s-cli/%s company inspect %s --json
./bazel-bin/src/%s-cli/%s env run --org %s --project %s --environment bootstrap -- guardian fly %s
`+"```"+`
`, company.CompanyName, company.CLIName, company.CLIName, company.CLIName, company.CLIName, company.Name, company.CLIName, company.CLIName, company.OrganizationName, company.Project, company.Site)
}

func renderCLIEntrypoint(repoRoot string, company CompanyRecord, force bool) error {
	dir := filepath.Join(repoRoot, "src", company.CLIName+"-cli")
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
    deps = ["//src/verself-cli/internal/app"],
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
