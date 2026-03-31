package domain

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/forge-metal/forge-metal/internal/cloudflare"
	"github.com/forge-metal/forge-metal/internal/prompt"
)

// FieldStatus represents the state of a form field.
type FieldStatus int

const (
	FieldMissing FieldStatus = iota
	FieldInvalid
	FieldValid
)

// Field holds the display and raw state of one form entry.
type Field struct {
	Index       int         // 1-based
	Label       string      // "Domain", "API token"
	Value       string      // display value (masked for token)
	RawValue    string      // actual value (never printed)
	Status      FieldStatus
	StatusLabel string      // "saved", "valid", "missing", "invalid"
	Changed     bool        // true if user set a new value this run
}

const totalFields = 2

// Config holds paths and optional flag overrides for the setup-domain wizard.
type Config struct {
	AnsibleVars string // path to group_vars/all/main.yml
	SecretsFile string // path to secrets.sops.yml
	CFBaseURL   string // override for testing; empty = Cloudflare production

	Domain string // from --domain flag or positional arg
	Token  string // from --token flag

	// Injected for testing (nil = use real sops).
	ReadToken  func(secretsFile string) string
	WriteToken func(secretsFile, token string) error
}

// Run executes the setup-domain wizard.
func Run(cfg Config, p prompt.Prompter, w io.Writer) error {
	// Prerequisite: SOPS must be initialized.
	if _, err := os.Stat(cfg.SecretsFile); os.IsNotExist(err) {
		return fmt.Errorf("secrets not initialized — run: make setup-sops")
	}

	fields := readCurrentState(cfg)
	applyFlags(fields, cfg)

	headless := cfg.Domain != "" && cfg.Token != ""
	if headless {
		return runHeadless(cfg, fields, w)
	}
	return runInteractive(cfg, fields, p, w)
}

func runHeadless(cfg Config, fields [totalFields]*Field, w io.Writer) error {
	// Domain is set via flag — mark valid.
	f := fields[0]
	if f.Changed {
		f.Status = FieldValid
		f.StatusLabel = "saved"
		f.Value = f.RawValue
	}

	// Validate token via API.
	domain := fields[0].RawValue
	token := fields[1].RawValue
	fmt.Fprintf(w, "  Validating API token... ")
	tc := classifyToken(token, domain, cfg.CFBaseURL)
	if tc.Status != cloudflare.TokenValid {
		fmt.Fprintf(w, "✗\n")
		return fmt.Errorf("token validation failed: %s", tc.Message)
	}
	fmt.Fprintf(w, "✓ valid\n")
	fields[1].Status = FieldValid
	fields[1].StatusLabel = "valid"
	fields[1].Value = maskToken(token)

	fmt.Fprintln(w)
	printSummaryBox(w, fields)

	return writeChangedFields(cfg, fields, w)
}

func runInteractive(cfg Config, fields [totalFields]*Field, p prompt.Prompter, w io.Writer) error {
	configured := countConfigured(fields)
	printHeader(w, configured)

	// Field 1: Domain
	f := fields[0]
	if f.Status == FieldValid {
		printFieldRow(w, f)
	} else {
		printFieldRow(w, f)
		fmt.Fprintln(w)
		input := p.Ask("  Enter your Cloudflare-managed domain (e.g. anveio.com): ")
		input = strings.TrimSpace(input)
		if input == "" {
			return fmt.Errorf("domain is required")
		}
		f.RawValue = input
		f.Value = input
		f.Status = FieldValid
		f.StatusLabel = "saved"
		f.Changed = true
		fmt.Fprintf(w, "  ✓ %s\n", input)
	}

	// Field 2: API token — needs domain for validation.
	domain := fields[0].RawValue
	tf := fields[1]

	// If we have a token from disk that hasn't been validated yet (because domain was
	// missing when readCurrentState ran), validate it now.
	if tf.RawValue != "" && tf.Status != FieldValid {
		tc := classifyToken(tf.RawValue, domain, cfg.CFBaseURL)
		if tc.Status == cloudflare.TokenValid {
			tf.Status = FieldValid
			tf.StatusLabel = "valid"
			tf.Value = maskToken(tf.RawValue)
		} else {
			tf.Status = FieldInvalid
			tf.StatusLabel = "invalid"
			tf.RawValue = ""
			tf.Value = "—"
		}
	}

	if tf.Status == FieldValid {
		printFieldRow(w, tf)
	} else {
		printFieldRow(w, tf)
		fmt.Fprintln(w)
		token := promptForToken(cfg, domain, p, w)
		tf.RawValue = token
		tf.Value = maskToken(token)
		tf.Status = FieldValid
		tf.StatusLabel = "valid"
		tf.Changed = true
	}

	fmt.Fprintln(w)

	anyChanged := fields[0].Changed || fields[1].Changed
	if !anyChanged {
		fmt.Fprintf(w, "  All fields configured. Run forge-metal setup-domain --domain [string] --token [string] to configure fields\n")
		fmt.Fprintf(w, "  Next: make deploy\n")
		return nil
	}

	printSummaryBox(w, fields)
	if err := writeChangedFields(cfg, fields, w); err != nil {
		return err
	}
	fmt.Fprintf(w, "  Next: make deploy\n")
	return nil
}

func promptForToken(cfg Config, domain string, p prompt.Prompter, w io.Writer) string {
	for {
		fmt.Fprintln(w, "  Cloudflare API token required")
		fmt.Fprintln(w, "    1. Log into Cloudflare → Profile → API Tokens")
		fmt.Fprintln(w, "    2. Click 'Create Token'")
		fmt.Fprintln(w, "    3. Use the 'Edit zone DNS' template")
		fmt.Fprintf(w, "    4. Under Zone Resources, select: %s\n", domain)
		fmt.Fprintln(w, "    5. Click 'Continue to summary' → 'Create Token'")
		fmt.Fprintln(w)

		input := p.AskSecret("  API token: ")
		input = strings.TrimSpace(input)
		if input == "" {
			fmt.Fprintln(w, "  ✗ Token cannot be empty.")
			fmt.Fprintln(w)
			continue
		}

		fmt.Fprintf(w, "  Validating... ")
		tc := classifyToken(input, domain, cfg.CFBaseURL)
		if tc.Status == cloudflare.TokenValid {
			fmt.Fprintf(w, "✓ valid\n")
			return input
		}
		fmt.Fprintf(w, "✗\n")
		fmt.Fprintf(w, "  %s\n\n", tc.Message)
	}
}

// --- Display helpers ---

func printHeader(w io.Writer, configured int) {
	fmt.Fprintf(w, "  Domain setup                                [%d/%d configured]\n", configured, totalFields)
	fmt.Fprintln(w)
}

func printFieldRow(w io.Writer, f *Field) {
	icon := "✓"
	if f.Status == FieldMissing || f.Status == FieldInvalid {
		icon = "✗"
	}
	fmt.Fprintf(w, "  [%d/%d]  %-16s %-20s %s %s\n",
		f.Index, totalFields, f.Label, f.Value, icon, f.StatusLabel)
}

func printSummaryBox(w io.Writer, fields [totalFields]*Field) {
	fmt.Fprintln(w, "  ──────────────────────────────────")
	for _, f := range fields {
		fmt.Fprintf(w, "    %-16s %s\n", f.Label, f.Value)
	}
	fmt.Fprintln(w, "  ──────────────────────────────────")
	fmt.Fprintln(w)
}

func maskToken(token string) string {
	if len(token) <= 7 {
		return "****"
	}
	return token[:3] + "****" + token[len(token)-4:]
}

func countConfigured(fields [totalFields]*Field) int {
	n := 0
	for _, f := range fields {
		if f.Status == FieldValid {
			n++
		}
	}
	return n
}

// --- State reading ---

func readCurrentState(cfg Config) [totalFields]*Field {
	var fields [totalFields]*Field

	// Field 1: Domain
	domain := readExistingDomain(cfg.AnsibleVars)
	f1 := &Field{Index: 1, Label: "Domain"}
	if domain != "" {
		f1.RawValue = domain
		f1.Value = domain
		f1.Status = FieldValid
		f1.StatusLabel = "saved"
	} else {
		f1.Value = "—"
		f1.Status = FieldMissing
		f1.StatusLabel = "missing"
	}
	fields[0] = f1

	// Field 2: API token
	readFn := readExistingToken
	if cfg.ReadToken != nil {
		readFn = cfg.ReadToken
	}
	token := readFn(cfg.SecretsFile)
	f2 := &Field{Index: 2, Label: "API token"}
	if token != "" {
		// If we have a domain, validate now. Otherwise defer to interactive flow.
		if domain != "" {
			tc := classifyToken(token, domain, cfg.CFBaseURL)
			if tc.Status == cloudflare.TokenValid {
				f2.RawValue = token
				f2.Value = maskToken(token)
				f2.Status = FieldValid
				f2.StatusLabel = "valid"
			} else {
				f2.RawValue = token
				f2.Value = maskToken(token)
				f2.Status = FieldInvalid
				f2.StatusLabel = "invalid"
			}
		} else {
			// Token exists but can't validate without domain — defer.
			f2.RawValue = token
			f2.Value = maskToken(token)
			f2.Status = FieldMissing
			f2.StatusLabel = "needs domain"
		}
	} else {
		f2.Value = "—"
		f2.Status = FieldMissing
		f2.StatusLabel = "missing"
	}
	fields[1] = f2

	return fields
}

var domainValueRe = regexp.MustCompile(`(?m)^forge_metal_domain:\s*"([^"]*)"`)

func readExistingDomain(varsFile string) string {
	data, err := os.ReadFile(varsFile)
	if err != nil {
		return ""
	}
	m := domainValueRe.FindSubmatch(data)
	if m == nil || len(m) < 2 || string(m[1]) == "" {
		return ""
	}
	return string(m[1])
}

func applyFlags(fields [totalFields]*Field, cfg Config) {
	if cfg.Domain != "" {
		f := fields[0]
		f.RawValue = cfg.Domain
		f.Value = cfg.Domain
		f.Status = FieldValid
		f.StatusLabel = "saved"
		if f.RawValue != readExistingDomain(cfg.AnsibleVars) {
			f.Changed = true
		}
	}
	if cfg.Token != "" {
		f := fields[1]
		f.RawValue = cfg.Token
		f.Value = maskToken(cfg.Token)
		f.Status = FieldMissing // will be validated in headless/interactive path
		f.StatusLabel = "pending"
		f.Changed = true
	}
}

// --- Write helpers ---

func writeChangedFields(cfg Config, fields [totalFields]*Field, w io.Writer) error {
	if fields[0].Changed {
		if err := writeDomain(cfg.AnsibleVars, fields[0].RawValue); err != nil {
			return fmt.Errorf("write domain: %w", err)
		}
		fmt.Fprintf(w, "  Updated: %s\n", cfg.AnsibleVars)
	}
	if fields[1].Changed {
		writeFn := saveToken
		if cfg.WriteToken != nil {
			writeFn = cfg.WriteToken
		}
		if err := writeFn(cfg.SecretsFile, fields[1].RawValue); err != nil {
			return fmt.Errorf("save token: %w", err)
		}
		fmt.Fprintf(w, "  Updated: %s\n", cfg.SecretsFile)
	}
	return nil
}

// --- Cloudflare helpers ---

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

// --- YAML write helpers ---

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
