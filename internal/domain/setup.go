package domain

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/forge-metal/forge-metal/internal/cloudflare"
)

type Config struct {
	Domain      string // may be empty — will prompt
	AnsibleVars string // path to group_vars/all/main.yml
	SecretsFile string // path to secrets.sops.yml
	CFBaseURL   string // override for testing; empty = Cloudflare production
}

// Prompter abstracts stdin/stdout for testability.
type Prompter interface {
	Ask(prompt string) string
	AskSecret(prompt string) string // no echo
	Confirm(prompt string) bool
}

// Run executes the full setup-domain wizard.
func Run(cfg Config, p Prompter, w io.Writer) error {
	// 1. Get domain
	domain := cfg.Domain
	if domain == "" {
		domain = p.Ask("Enter your Cloudflare-managed domain (e.g. anveio.com): ")
	}
	domain = strings.TrimSpace(domain)
	if domain == "" {
		return fmt.Errorf("domain is required")
	}

	fmt.Fprintf(w, "Domain: %s\n", domain)
	fmt.Fprintf(w, "  admin.%s  → ClickStack dashboard\n", domain)
	fmt.Fprintf(w, "  git.%s    → Forgejo (when enabled)\n", domain)
	fmt.Fprintln(w)

	// 2. Ensure SOPS is initialized
	if _, err := os.Stat(cfg.SecretsFile); os.IsNotExist(err) {
		return fmt.Errorf("secrets not initialized — run: make setup-sops")
	}

	// 3. Read or prompt for Cloudflare token
	token := readExistingToken(cfg.SecretsFile)

	if token != "" {
		fmt.Fprintf(w, "Validating existing token...\n")
		tc := classifyToken(token, domain, cfg.CFBaseURL)
		if tc.Status == cloudflare.TokenValid {
			fmt.Fprintf(w, "Cloudflare API token: valid\n")
		} else {
			fmt.Fprintf(w, "Existing token is invalid.\n")
			token = ""
		}
	}

	for token == "" {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Cloudflare API token required")
		fmt.Fprintln(w, "  1. Log into Cloudflare, go to your profile → API Tokens")
		fmt.Fprintln(w, "  2. Click 'Create Token'")
		fmt.Fprintln(w, "  3. Use the 'Edit zone DNS' template")
		fmt.Fprintf(w, "  4. Under Zone Resources, select: %s\n", domain)
		fmt.Fprintln(w, "  5. Click 'Continue to summary' → 'Create Token'")
		fmt.Fprintln(w)

		input := p.AskSecret("Cloudflare API token: ")
		input = strings.TrimSpace(input)
		if input == "" {
			fmt.Fprintln(w, "ERROR: Token cannot be empty.")
			continue
		}

		fmt.Fprintf(w, "Validating...\n")
		tc := classifyToken(input, domain, cfg.CFBaseURL)
		switch tc.Status {
		case cloudflare.TokenValid:
			fmt.Fprintf(w, "Cloudflare API token: valid\n")
			token = input
		case cloudflare.TokenInvalid:
			fmt.Fprintln(w, tc.Message)
		case cloudflare.TokenWrongPerms:
			fmt.Fprintln(w, tc.Message)
		case cloudflare.TokenWrongZone:
			fmt.Fprintln(w, tc.Message)
		}
	}

	// 4. Save token to SOPS secrets
	if err := saveToken(cfg.SecretsFile, token); err != nil {
		return fmt.Errorf("save token: %w", err)
	}

	// 5. Write domain to ansible vars
	if err := writeDomain(cfg.AnsibleVars, domain); err != nil {
		return fmt.Errorf("write domain: %w", err)
	}

	fmt.Fprintf(w, "Updated: %s\n\n", cfg.AnsibleVars)
	fmt.Fprintf(w, "Domain:  %s\n", domain)
	fmt.Fprintf(w, "Token:   valid\n\n")
	fmt.Fprintf(w, "Next: make deploy\n")
	return nil
}

func classifyToken(token, domain, baseURL string) cloudflare.TokenCheck {
	c := &cloudflare.Client{Token: token, BaseURL: baseURL}
	return c.ClassifyTokenFromAPI(domain)
}

func readExistingToken(secretsFile string) string {
	out, err := exec.Command("sops", "-d", "--extract", `["cloudflare_api_token"]`, secretsFile).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func saveToken(secretsFile, token string) error {
	cmd := exec.Command("sops", "--set", fmt.Sprintf(`["cloudflare_api_token"] "%s"`, token), secretsFile)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

var domainLineRe = regexp.MustCompile(`(?m)^forge_metal_domain:.*$`)

func writeDomain(varsFile, domain string) error {
	data, err := os.ReadFile(varsFile)
	if err != nil {
		return err
	}

	content := string(data)
	newLine := fmt.Sprintf(`forge_metal_domain: "%s"`, domain)

	if domainLineRe.MatchString(content) {
		content = domainLineRe.ReplaceAllString(content, newLine)
	} else {
		content = strings.TrimRight(content, "\n") + "\n" + newLine + "\n"
	}

	return os.WriteFile(varsFile, []byte(content), 0644)
}

// TTYPrompter reads from stdin/stdout.
type TTYPrompter struct {
	In  io.Reader
	Out io.Writer
}

func (p *TTYPrompter) Ask(prompt string) string {
	fmt.Fprint(p.Out, prompt)
	scanner := bufio.NewScanner(p.In)
	if scanner.Scan() {
		return scanner.Text()
	}
	return ""
}

func (p *TTYPrompter) AskSecret(prompt string) string {
	// In a real terminal, disable echo. For now, same as Ask.
	return p.Ask(prompt)
}

func (p *TTYPrompter) Confirm(prompt string) bool {
	answer := p.Ask(prompt + " [y/N]: ")
	return strings.ToLower(strings.TrimSpace(answer)) == "y"
}
