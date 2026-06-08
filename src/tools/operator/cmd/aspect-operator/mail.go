package main

import (
	"fmt"
	"os"
	"strings"

	"cuelang.org/go/cue"
	opch "github.com/verself/operator-runtime/clickhouse"
	opruntime "github.com/verself/operator-runtime/runtime"
)

type mailOptions struct {
	operatorRuntimeOptions
}

type mailMainVars struct {
	VerselfDomain string
}

func cmdMail(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("mail: missing subcommand (try `addresses` or `test-accounts`)")
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "addresses":
		return cmdMailAddresses(rest)
	case "test-accounts":
		return cmdMailTestAccounts(rest)
	default:
		return fmt.Errorf("mail: unknown subcommand: %s", sub)
	}
}

func cmdMailAddresses(args []string) error {
	fs := flagSet("mail addresses")
	opts := &mailOptions{}
	addOperatorRuntimeFlags(&opts.operatorRuntimeOptions)
	fs.StringVar(&opts.site, "site", opts.site, "Deploy site")
	fs.StringVar(&opts.repoRoot, "repo-root", "", "verself-sh checkout root (defaults to cwd)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return runOperatorRuntime("mail.addresses", opts.operatorRuntimeOptions, 0, opch.Config{Database: "verself"}, func(rt *opruntime.Runtime, _ *opch.Client) error {
		cfg, err := loadMailConfig(rt.RepoRoot, rt.Site)
		if err != nil {
			return err
		}
		for _, localPart := range companyEmailLocalParts() {
			_, _ = fmt.Fprintf(os.Stdout, "%s@%s\n", localPart, cfg.VerselfDomain)
		}
		return nil
	})
}

func companyEmailLocalParts() []string {
	return []string{
		"anveio",
		"hello",
		"feedback",
		"sales",
		"support",
		"security",
		"abuse",
		"privacy",
		"legal",
		"billing",
		"careers",
		"press",
		"postmaster",
		"hostmaster",
		"webmaster",
		"noreply",
		"updates",
		"agents",
	}
}

func loadMailConfig(repoRoot, site string) (mailMainVars, error) {
	facts, path, err := loadSiteFacts(repoRoot, site)
	if err != nil {
		return mailMainVars{}, err
	}
	domain, err := siteFactString(facts, cue.ParsePath("domains.product"), true)
	if err != nil {
		return mailMainVars{}, fmt.Errorf("%s: %w", path, err)
	}
	cfg := mailMainVars{VerselfDomain: domain}
	if strings.TrimSpace(cfg.VerselfDomain) == "" {
		return mailMainVars{}, fmt.Errorf("%s missing domains.product", path)
	}
	cfg.VerselfDomain = strings.TrimSpace(cfg.VerselfDomain)
	return cfg, nil
}
