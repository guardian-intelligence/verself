package main

import (
	"fmt"
	"os"
	"strings"

	opch "github.com/verself/operator-runtime/clickhouse"
	opruntime "github.com/verself/operator-runtime/runtime"
)

type mailOptions struct {
	operatorRuntimeOptions
}

type mailMainVars struct {
	VerselfDomain string `yaml:"verself_domain"`
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
	return runOperatorRuntime("mail.addresses", opts.operatorRuntimeOptions, false, opch.Config{Database: "verself"}, func(rt *opruntime.Runtime, _ *opch.Client) error {
		cfg, err := loadMailConfig(rt.RepoRoot, rt.Site)
		if err != nil {
			return err
		}
		for _, localPart := range platformCompanyEmailLocalParts() {
			_, _ = fmt.Fprintf(os.Stdout, "%s@%s\n", localPart, cfg.VerselfDomain)
		}
		return nil
	})
}

func loadMailConfig(repoRoot, site string) (mailMainVars, error) {
	path := siteVarsPath(repoRoot, site)
	var cfg mailMainVars
	if err := readYAMLFile(path, &cfg); err != nil {
		return mailMainVars{}, err
	}
	if strings.TrimSpace(cfg.VerselfDomain) == "" {
		return mailMainVars{}, fmt.Errorf("%s missing verself_domain", path)
	}
	cfg.VerselfDomain = strings.TrimSpace(cfg.VerselfDomain)
	return cfg, nil
}
