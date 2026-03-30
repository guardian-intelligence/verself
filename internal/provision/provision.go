package provision

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/forge-metal/forge-metal/internal/config"
	"github.com/forge-metal/forge-metal/internal/latitude"
	"github.com/forge-metal/forge-metal/internal/prompt"
)

// Wizard orchestrates the interactive provision flow.
type Wizard struct {
	Cfg          *config.Config
	Prompter     prompt.Prompter
	Out          io.Writer
	TerraformDir string // default: "terraform"
	LatBaseURL   string // override for testing; empty = production
}

// resolved holds all validated form fields ready for provisioning.
type resolved struct {
	token   string
	email   string
	project latitude.Project
	sshKey  string
	region  latitude.Region
	plan    latitude.Plan
}

// Run executes the provisioning wizard.
//
// Each field is resolved in order: if already configured and valid, it prints
// a ✓ and moves on. If missing or invalid, the user is prompted. This means
// a fully configured second run shows all ✓ marks and goes straight to confirm.
func (w *Wizard) Run(ctx context.Context) error {
	fmt.Fprintln(w.Out, "Provision: Latitude.sh bare metal")
	fmt.Fprintln(w.Out)

	token, email, err := w.resolveToken()
	if err != nil {
		return err
	}

	project, err := w.resolveProject(token)
	if err != nil {
		return err
	}

	sshKey, err := w.resolveSSHKey()
	if err != nil {
		return err
	}

	region, err := w.resolveRegion(token)
	if err != nil {
		return err
	}

	plan, err := w.resolvePlan(token, region)
	if err != nil {
		return err
	}

	r := resolved{
		token: token, email: email,
		project: project, sshKey: sshKey,
		region: region, plan: plan,
	}

	fmt.Fprintln(w.Out)
	if err := w.confirm(r); err != nil {
		return err
	}

	if err := w.save(r); err != nil {
		return err
	}

	return w.provision(r.token)
}

// resolveToken validates the configured token or prompts for a new one.
func (w *Wizard) resolveToken() (string, string, error) {
	token := w.Cfg.Latitude.AuthToken

	if token != "" {
		client := w.latClient(token)
		user, err := client.ValidateToken()
		if err == nil {
			fmt.Fprintf(w.Out, "  ✓ API Token    %s\n", user.Email)
			return token, user.Email, nil
		}
		fmt.Fprintf(w.Out, "  ✗ API Token    saved token is invalid\n\n")
	}

	for {
		fmt.Fprintln(w.Out, "Latitude.sh API token required")
		fmt.Fprintln(w.Out, "  1. Go to https://www.latitude.sh/dashboard/api-keys")
		fmt.Fprintln(w.Out, "  2. Click 'Create API Key'")
		fmt.Fprintln(w.Out, "  3. Copy the token")
		fmt.Fprintln(w.Out)

		input := w.Prompter.AskSecret("Latitude.sh API token: ")
		input = strings.TrimSpace(input)
		if input == "" {
			fmt.Fprintln(w.Out, "  ✗ Token cannot be empty.")
			continue
		}

		client := w.latClient(input)
		user, err := client.ValidateToken()
		if err != nil {
			fmt.Fprintf(w.Out, "  ✗ Token invalid. Check that you copied the full token.\n\n")
			continue
		}

		fmt.Fprintf(w.Out, "  ✓ API Token    %s\n", user.Email)
		return input, user.Email, nil
	}
}

// resolveProject validates the configured project or prompts for selection.
func (w *Wizard) resolveProject(token string) (latitude.Project, error) {
	client := w.latClient(token)

	projects, err := client.ListProjects()
	if err != nil {
		return latitude.Project{}, fmt.Errorf("fetch projects: %w", err)
	}
	if len(projects) == 0 {
		return latitude.Project{}, fmt.Errorf("no projects found — create one at https://dash.latitude.sh")
	}

	// Check if configured project still exists.
	if w.Cfg.Latitude.Project != "" {
		for _, p := range projects {
			if p.ID == w.Cfg.Latitude.Project || p.Slug == w.Cfg.Latitude.Project {
				fmt.Fprintf(w.Out, "  ✓ Project      %s\n", p.Name)
				return p, nil
			}
		}
		fmt.Fprintf(w.Out, "  ✗ Project      %q not found in your account\n\n", w.Cfg.Latitude.Project)
	}

	// Auto-select if there's only one.
	if len(projects) == 1 {
		p := projects[0]
		fmt.Fprintf(w.Out, "  ✓ Project      %s (auto-selected)\n", p.Name)
		return p, nil
	}

	var options []string
	for _, p := range projects {
		options = append(options, fmt.Sprintf("%s (%s)", p.Name, p.ID))
	}

	idx, _ := w.Prompter.Select("Select project:", options)
	chosen := projects[idx]
	fmt.Fprintf(w.Out, "  ✓ Project      %s\n", chosen.Name)
	return chosen, nil
}

// resolveSSHKey checks the configured SSH key path or prompts for one.
func (w *Wizard) resolveSSHKey() (string, error) {
	if err := w.Cfg.ExpandPaths(); err != nil {
		return "", err
	}

	pubPath := w.Cfg.SSH.PublicKeyPath
	if _, err := os.Stat(pubPath); err == nil {
		fmt.Fprintf(w.Out, "  ✓ SSH Key      %s\n", pubPath)
		return pubPath, nil
	}

	fmt.Fprintf(w.Out, "  ✗ SSH Key      not found at %s\n\n", pubPath)

	if w.Prompter.Confirm("Generate a new ed25519 SSH key?") {
		privPath := strings.TrimSuffix(pubPath, ".pub")
		dir := filepath.Dir(privPath)
		if err := os.MkdirAll(dir, 0700); err != nil {
			return "", fmt.Errorf("create ssh dir: %w", err)
		}

		cmd := exec.Command("ssh-keygen", "-t", "ed25519", "-f", privPath, "-N", "")
		cmd.Stdout = w.Out
		cmd.Stderr = w.Out
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("ssh-keygen: %w", err)
		}

		fmt.Fprintf(w.Out, "  ✓ SSH Key      %s\n", pubPath)
		return pubPath, nil
	}

	custom := w.Prompter.Ask("Path to SSH public key (.pub): ")
	custom = strings.TrimSpace(custom)
	if custom == "" {
		return "", fmt.Errorf("SSH key is required for provisioning")
	}
	if _, err := os.Stat(custom); err != nil {
		return "", fmt.Errorf("SSH key not found at %s", custom)
	}
	fmt.Fprintf(w.Out, "  ✓ SSH Key      %s\n", custom)
	return custom, nil
}

// resolveRegion validates the configured region or prompts for selection.
func (w *Wizard) resolveRegion(token string) (latitude.Region, error) {
	client := w.latClient(token)

	regions, err := client.ListRegions()
	if err != nil {
		return latitude.Region{}, fmt.Errorf("fetch regions: %w", err)
	}
	if len(regions) == 0 {
		return latitude.Region{}, fmt.Errorf("no regions available")
	}

	// Check if configured region still exists.
	configSlug := w.Cfg.Latitude.Region
	if configSlug != "" && w.Cfg.Source.Latitude.Region != config.SourceDefault {
		for _, r := range regions {
			if r.Slug == configSlug {
				fmt.Fprintf(w.Out, "  ✓ Region       %s — %s, %s\n", r.Slug, r.Name, r.Country)
				return r, nil
			}
		}
		fmt.Fprintf(w.Out, "  ✗ Region       %q not available\n\n", configSlug)
	}

	regions = prioritizeRegion(regions, configSlug)

	var options []string
	for _, r := range regions {
		options = append(options, fmt.Sprintf("%s — %s, %s", r.Slug, r.Name, r.Country))
	}

	idx, _ := w.Prompter.Select("Select region:", options)
	chosen := regions[idx]
	fmt.Fprintf(w.Out, "  ✓ Region       %s — %s, %s\n", chosen.Slug, chosen.Name, chosen.Country)
	return chosen, nil
}

// resolvePlan validates the configured plan or prompts for selection.
func (w *Wizard) resolvePlan(token string, region latitude.Region) (latitude.Plan, error) {
	client := w.latClient(token)

	plans, err := client.ListPlans(region.Slug)
	if err != nil {
		return latitude.Plan{}, fmt.Errorf("fetch plans: %w", err)
	}
	if len(plans) == 0 {
		return latitude.Plan{}, fmt.Errorf("no plans available for region %s", region.Slug)
	}

	// Check if configured plan is available.
	configSlug := w.Cfg.Latitude.Plan
	if configSlug != "" && w.Cfg.Source.Latitude.Plan != config.SourceDefault {
		for _, p := range plans {
			if p.Slug == configSlug {
				fmt.Fprintf(w.Out, "  ✓ Plan         %s\n", p.Slug)
				return p, nil
			}
		}
		fmt.Fprintf(w.Out, "  ✗ Plan         %q not available in %s\n\n", configSlug, region.Slug)
	}

	plans = prioritizePlan(plans, configSlug)

	var options []string
	for _, p := range plans {
		options = append(options, fmt.Sprintf("%s — %s", p.Slug, p.Name))
	}

	idx, _ := w.Prompter.Select("Select server plan:", options)
	chosen := plans[idx]
	fmt.Fprintf(w.Out, "  ✓ Plan         %s\n", chosen.Slug)
	return chosen, nil
}

// confirm shows the provision summary and asks to proceed.
func (w *Wizard) confirm(r resolved) error {
	fmt.Fprintln(w.Out, "──────────────────────────────────")
	fmt.Fprintln(w.Out, "  Provision Summary")
	fmt.Fprintln(w.Out, "──────────────────────────────────")
	fmt.Fprintf(w.Out, "  Provider:  Latitude.sh\n")
	fmt.Fprintf(w.Out, "  Account:   %s\n", r.email)
	fmt.Fprintf(w.Out, "  Project:   %s (%s)\n", r.project.Name, r.project.ID)
	fmt.Fprintf(w.Out, "  Region:    %s (%s, %s)\n", r.region.Slug, r.region.Name, r.region.Country)
	fmt.Fprintf(w.Out, "  Plan:      %s (%s)\n", r.plan.Slug, r.plan.Name)
	fmt.Fprintf(w.Out, "  OS:        %s\n", w.Cfg.Latitude.OperatingSystem)
	fmt.Fprintf(w.Out, "  Billing:   %s\n", w.Cfg.Latitude.Billing)
	fmt.Fprintf(w.Out, "  SSH Key:   %s\n", r.sshKey)
	fmt.Fprintln(w.Out, "──────────────────────────────────")
	fmt.Fprintln(w.Out)

	if !w.Prompter.Confirm("Proceed?") {
		return fmt.Errorf("aborted")
	}
	return nil
}

// save persists selections to forge-metal.toml and terraform.tfvars.json.
func (w *Wizard) save(r resolved) error {
	// Persist to forge-metal.toml so next run skips prompts.
	if err := config.SaveLatitude(r.token, r.project.ID, r.region.Slug, r.plan.Slug); err != nil {
		fmt.Fprintf(w.Out, "  ⚠ Could not save config: %v\n", err)
		// Non-fatal — provisioning can still proceed.
	}

	// Write terraform.tfvars.json.
	tfDir := w.terraformDir()
	tfvars := map[string]interface{}{
		"cluster_name":        "dev",
		"project_id":          r.project.ID,
		"worker_count":        1,
		"infra_count":         0,
		"region":              r.region.Slug,
		"plan":                r.plan.Slug,
		"billing":             w.Cfg.Latitude.Billing,
		"ssh_public_key_path": r.sshKey,
	}

	data, err := json.MarshalIndent(tfvars, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal tfvars: %w", err)
	}

	path := filepath.Join(tfDir, "terraform.tfvars.json")
	if err := os.WriteFile(path, append(data, '\n'), 0644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}

	fmt.Fprintf(w.Out, "Wrote %s\n\n", path)
	return nil
}

// provision runs tofu init + apply, then generates inventory.
func (w *Wizard) provision(token string) error {
	tfDir := w.terraformDir()
	env := append(os.Environ(), "LATITUDESH_AUTH_TOKEN="+token)

	fmt.Fprintln(w.Out, "Initializing Terraform...")
	init := exec.Command("tofu", "init", "-input=false")
	init.Dir = tfDir
	init.Stdout = w.Out
	init.Stderr = w.Out
	init.Env = env
	if err := init.Run(); err != nil {
		return fmt.Errorf("tofu init failed: %w", err)
	}

	fmt.Fprintln(w.Out)
	fmt.Fprintln(w.Out, "Provisioning bare metal (this may take several minutes)...")
	apply := exec.Command("tofu", "apply", "-var-file=terraform.tfvars.json", "-auto-approve")
	apply.Dir = tfDir
	apply.Stdout = w.Out
	apply.Stderr = w.Out
	apply.Env = env
	if err := apply.Run(); err != nil {
		return fmt.Errorf("tofu apply failed: %w\n\nRun `bmci doctor` to check your environment.", err)
	}

	fmt.Fprintln(w.Out)
	fmt.Fprintln(w.Out, "Generating Ansible inventory...")
	gen := exec.Command("./scripts/generate-inventory.sh")
	gen.Stdout = w.Out
	gen.Stderr = w.Out
	if err := gen.Run(); err != nil {
		return fmt.Errorf("inventory generation failed: %w", err)
	}

	fmt.Fprintln(w.Out)
	fmt.Fprintln(w.Out, "Done! Server provisioned.")
	fmt.Fprintln(w.Out)
	fmt.Fprintln(w.Out, "Next steps:")
	fmt.Fprintln(w.Out, "  bmci setup-domain   Configure DNS (optional)")
	fmt.Fprintln(w.Out, "  make deploy         Deploy the stack")
	return nil
}

func (w *Wizard) latClient(token string) *latitude.Client {
	return &latitude.Client{Token: token, BaseURL: w.LatBaseURL}
}

func (w *Wizard) terraformDir() string {
	if w.TerraformDir != "" {
		return w.TerraformDir
	}
	return "terraform"
}

func prioritizeRegion(regions []latitude.Region, slug string) []latitude.Region {
	if slug == "" {
		return regions
	}
	for i, region := range regions {
		if region.Slug != slug {
			continue
		}
		if i == 0 {
			return regions
		}
		prioritized := make([]latitude.Region, 0, len(regions))
		prioritized = append(prioritized, region)
		prioritized = append(prioritized, regions[:i]...)
		prioritized = append(prioritized, regions[i+1:]...)
		return prioritized
	}
	return regions
}

func prioritizePlan(plans []latitude.Plan, slug string) []latitude.Plan {
	if slug == "" {
		return plans
	}
	for i, plan := range plans {
		if plan.Slug != slug {
			continue
		}
		if i == 0 {
			return plans
		}
		prioritized := make([]latitude.Plan, 0, len(plans))
		prioritized = append(prioritized, plan)
		prioritized = append(prioritized, plans[:i]...)
		prioritized = append(prioritized, plans[i+1:]...)
		return prioritized
	}
	return plans
}
