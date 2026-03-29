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

// Supported bare-metal providers.
var supportedProviders = []string{"latitude.sh"}

// Providers shown in the picker (disabled ones have coming-soon label).
var providerOptions = []struct {
	Name     string
	Enabled  bool
}{
	{"Latitude.sh", true},
	{"Hetzner", false},
	{"Equinix Metal", false},
	{"Vultr Bare Metal", false},
}

// Wizard orchestrates the interactive provision flow.
type Wizard struct {
	Cfg          *config.Config
	Prompter     prompt.Prompter
	Out          io.Writer
	TerraformDir string // default: "terraform"
	LatBaseURL   string // override for testing; empty = production
}

type summary struct {
	Provider string
	Project  latitude.Project
	Region   latitude.Region
	Plan     latitude.Plan
	SSHKey   string
	Billing  string
}

// Run executes the full provisioning wizard.
func (w *Wizard) Run(ctx context.Context) error {
	provider, err := w.checkProvider()           // S0
	if err != nil {
		return err
	}
	_ = provider

	token, err := w.ensureAPIToken()             // S1
	if err != nil {
		return err
	}

	project, err := w.ensureProject(token)       // S2
	if err != nil {
		return err
	}

	sshKey, err := w.ensureSSHKey()              // S3
	if err != nil {
		return err
	}

	region, err := w.selectRegion(token)         // S4
	if err != nil {
		return err
	}

	plan, err := w.selectPlan(token, region)     // S5
	if err != nil {
		return err
	}

	s := summary{
		Provider: "Latitude.sh",
		Project:  project,
		Region:   region,
		Plan:     plan,
		SSHKey:   sshKey,
		Billing:  w.Cfg.Latitude.Billing,
	}

	if err := w.confirm(s); err != nil {         // S6
		return err
	}

	if err := w.writeConfig(s); err != nil {     // S7
		return err
	}

	return w.provision()                          // S8
}

// S0: Check if a valid bare-metal provider is configured.
func (w *Wizard) checkProvider() (string, error) {
	// For now the only valid value is "latitude.sh" (or unset, which defaults to it).
	// If the user has something else in config, tell them.
	current := strings.TrimSpace(w.Cfg.Latitude.Region)

	// We don't have a "provider" field yet — presence of a Latitude auth token
	// or project implies latitude.sh. For the picker, we always show it.
	fmt.Fprintln(w.Out, "Bare metal provider")
	fmt.Fprintln(w.Out)

	var options []string
	for _, p := range providerOptions {
		label := p.Name
		if !p.Enabled {
			label += "  (coming soon)"
		}
		options = append(options, label)
	}

	idx, _ := w.Prompter.Select("Select provider:", options)
	chosen := providerOptions[idx]

	if !chosen.Enabled {
		return "", fmt.Errorf("%s is not yet supported. Supported providers: %s",
			chosen.Name, strings.Join(supportedProviders, ", "))
	}

	_ = current
	fmt.Fprintf(w.Out, "  Provider: %s\n\n", chosen.Name)
	return strings.ToLower(chosen.Name), nil
}

// S1: Ensure we have a valid Latitude.sh API token.
func (w *Wizard) ensureAPIToken() (string, error) {
	token := w.Cfg.Latitude.AuthToken

	client := w.latClient(token)

	// Try existing token first.
	if token != "" {
		fmt.Fprintf(w.Out, "Validating Latitude.sh API token...\n")
		user, err := client.ValidateToken()
		if err == nil {
			fmt.Fprintf(w.Out, "  ✓ Authenticated as %s\n\n", user.Email)
			return token, nil
		}
		fmt.Fprintf(w.Out, "  ✗ Existing token is invalid.\n\n")
	}

	// Prompt loop.
	for {
		fmt.Fprintln(w.Out, "Latitude.sh API token required")
		fmt.Fprintln(w.Out, "  1. Go to https://dash.latitude.sh/api-keys")
		fmt.Fprintln(w.Out, "  2. Create a new API key")
		fmt.Fprintln(w.Out, "  3. Copy the token")
		fmt.Fprintln(w.Out)

		input := w.Prompter.AskSecret("Latitude.sh API token: ")
		input = strings.TrimSpace(input)
		if input == "" {
			fmt.Fprintln(w.Out, "  ✗ Token cannot be empty.")
			continue
		}

		fmt.Fprintf(w.Out, "Validating...\n")
		client = w.latClient(input)
		user, err := client.ValidateToken()
		if err != nil {
			fmt.Fprintf(w.Out, "  ✗ Token invalid. Check that you copied the full token.\n\n")
			continue
		}

		fmt.Fprintf(w.Out, "  ✓ Authenticated as %s\n\n", user.Email)
		return input, nil
	}
}

// S2: Ensure we have a project selected.
func (w *Wizard) ensureProject(token string) (latitude.Project, error) {
	client := w.latClient(token)

	projects, err := client.ListProjects()
	if err != nil {
		return latitude.Project{}, fmt.Errorf("fetch projects: %w", err)
	}
	if len(projects) == 0 {
		return latitude.Project{}, fmt.Errorf("no projects found — create one at https://dash.latitude.sh")
	}

	// If the config already has a project ID, try to match it.
	if w.Cfg.Latitude.Project != "" {
		for _, p := range projects {
			if p.ID == w.Cfg.Latitude.Project || p.Slug == w.Cfg.Latitude.Project {
				fmt.Fprintf(w.Out, "  Project: %s (%s)\n\n", p.Name, p.ID)
				return p, nil
			}
		}
		fmt.Fprintf(w.Out, "  ⚠ Configured project %q not found in your account.\n\n", w.Cfg.Latitude.Project)
	}

	// Auto-select if there's only one.
	if len(projects) == 1 {
		p := projects[0]
		fmt.Fprintf(w.Out, "  Project: %s (%s) — auto-selected (only project)\n\n", p.Name, p.ID)
		return p, nil
	}

	// Prompt to pick.
	var options []string
	for _, p := range projects {
		options = append(options, fmt.Sprintf("%s (%s)", p.Name, p.ID))
	}

	idx, _ := w.Prompter.Select("Select project:", options)
	chosen := projects[idx]
	fmt.Fprintf(w.Out, "  Project: %s (%s)\n\n", chosen.Name, chosen.ID)
	return chosen, nil
}

// S3: Ensure SSH key exists.
func (w *Wizard) ensureSSHKey() (string, error) {
	if err := w.Cfg.ExpandPaths(); err != nil {
		return "", err
	}

	pubPath := w.Cfg.SSH.PublicKeyPath
	if _, err := os.Stat(pubPath); err == nil {
		fmt.Fprintf(w.Out, "  SSH key: %s\n\n", pubPath)
		return pubPath, nil
	}

	fmt.Fprintf(w.Out, "  SSH key not found at %s\n\n", pubPath)

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

		fmt.Fprintf(w.Out, "  ✓ Generated %s\n\n", pubPath)
		return pubPath, nil
	}

	// Ask for custom path.
	custom := w.Prompter.Ask("Path to SSH public key (.pub): ")
	custom = strings.TrimSpace(custom)
	if custom == "" {
		return "", fmt.Errorf("SSH key is required for provisioning")
	}
	if _, err := os.Stat(custom); err != nil {
		return "", fmt.Errorf("SSH key not found at %s", custom)
	}
	return custom, nil
}

// S4: Select datacenter region.
func (w *Wizard) selectRegion(token string) (latitude.Region, error) {
	client := w.latClient(token)

	regions, err := client.ListRegions()
	if err != nil {
		return latitude.Region{}, fmt.Errorf("fetch regions: %w", err)
	}
	if len(regions) == 0 {
		return latitude.Region{}, fmt.Errorf("no regions available from Latitude.sh API")
	}

	defaultSlug := w.Cfg.Latitude.Region

	var options []string
	defaultIdx := 0
	for i, r := range regions {
		label := fmt.Sprintf("%s — %s, %s", r.Slug, r.City, r.Country)
		if r.Slug == defaultSlug {
			label += "  (default)"
			defaultIdx = i
		}
		options = append(options, label)
	}

	// Move default to top for convenience.
	if defaultIdx > 0 {
		regions[0], regions[defaultIdx] = regions[defaultIdx], regions[0]
		options[0], options[defaultIdx] = options[defaultIdx], options[0]
	}

	idx, _ := w.Prompter.Select("Select region:", options)
	chosen := regions[idx]
	fmt.Fprintf(w.Out, "  Region: %s (%s, %s)\n\n", chosen.Slug, chosen.City, chosen.Country)
	return chosen, nil
}

// S5: Select server plan.
func (w *Wizard) selectPlan(token string, region latitude.Region) (latitude.Plan, error) {
	client := w.latClient(token)

	plans, err := client.ListPlans(region.Slug)
	if err != nil {
		return latitude.Plan{}, fmt.Errorf("fetch plans: %w", err)
	}
	if len(plans) == 0 {
		return latitude.Plan{}, fmt.Errorf("no plans available for region %s", region.Slug)
	}

	defaultSlug := w.Cfg.Latitude.Plan

	var options []string
	defaultIdx := 0
	for i, p := range plans {
		label := fmt.Sprintf("%s — %s", p.Slug, p.Name)
		if p.Slug == defaultSlug {
			label += "  (default)"
			defaultIdx = i
		}
		options = append(options, label)
	}

	if defaultIdx > 0 {
		plans[0], plans[defaultIdx] = plans[defaultIdx], plans[0]
		options[0], options[defaultIdx] = options[defaultIdx], options[0]
	}

	idx, _ := w.Prompter.Select("Select server plan:", options)
	chosen := plans[idx]
	fmt.Fprintf(w.Out, "  Plan: %s (%s)\n\n", chosen.Slug, chosen.Name)
	return chosen, nil
}

// S6: Show summary and ask for confirmation.
func (w *Wizard) confirm(s summary) error {
	fmt.Fprintln(w.Out, "──────────────────────────────────")
	fmt.Fprintln(w.Out, "  Provision Summary")
	fmt.Fprintln(w.Out, "──────────────────────────────────")
	fmt.Fprintf(w.Out, "  Provider:  %s\n", s.Provider)
	fmt.Fprintf(w.Out, "  Project:   %s (%s)\n", s.Project.Name, s.Project.ID)
	fmt.Fprintf(w.Out, "  Region:    %s (%s, %s)\n", s.Region.Slug, s.Region.City, s.Region.Country)
	fmt.Fprintf(w.Out, "  Plan:      %s (%s)\n", s.Plan.Slug, s.Plan.Name)
	fmt.Fprintf(w.Out, "  OS:        %s\n", w.Cfg.Latitude.OperatingSystem)
	fmt.Fprintf(w.Out, "  Billing:   %s\n", s.Billing)
	fmt.Fprintf(w.Out, "  SSH Key:   %s\n", s.SSHKey)
	fmt.Fprintln(w.Out, "──────────────────────────────────")
	fmt.Fprintln(w.Out)

	if !w.Prompter.Confirm("Proceed?") {
		return fmt.Errorf("aborted")
	}
	return nil
}

// S7: Write terraform.tfvars.json.
func (w *Wizard) writeConfig(s summary) error {
	tfDir := w.terraformDir()

	tfvars := map[string]interface{}{
		"cluster_name":       "dev",
		"project_id":         s.Project.ID,
		"worker_count":       1,
		"infra_count":        0,
		"region":             s.Region.Slug,
		"plan":               s.Plan.Slug,
		"billing":            s.Billing,
		"ssh_public_key_path": s.SSHKey,
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

// S8: Run tofu init + apply, then generate inventory.
func (w *Wizard) provision() error {
	tfDir := w.terraformDir()

	fmt.Fprintln(w.Out, "Initializing Terraform...")
	init := exec.Command("tofu", "init", "-input=false")
	init.Dir = tfDir
	init.Stdout = w.Out
	init.Stderr = w.Out
	if err := init.Run(); err != nil {
		return fmt.Errorf("tofu init failed: %w", err)
	}

	fmt.Fprintln(w.Out)
	fmt.Fprintln(w.Out, "Provisioning bare metal (this may take several minutes)...")
	apply := exec.Command("tofu", "apply", "-var-file=terraform.tfvars.json", "-auto-approve")
	apply.Dir = tfDir
	apply.Stdout = w.Out
	apply.Stderr = w.Out
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
	fmt.Fprintln(w.Out, "✓ Server provisioned!")
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
