package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	opch "github.com/verself/operator-runtime/clickhouse"
	opruntime "github.com/verself/operator-runtime/runtime"
)

const (
	billingSetUserStateTarget = "//src/services/billing-service/cmd/billing-set-user-state:billing-set-user-state"
	productAPIProjectName     = "verself-api"
)

type personaOptions struct {
	operatorRuntimeOptions
}

type personaDefinition struct {
	Name               string
	HumanEmail         string
	HumanPasswordURI   string
	MachineUsername    string
	MachineSecretPath  string
	EmailLocalPart     string
	IncludePlatformOps bool
	TokenProjects      []string
}

type hostMainVars struct {
	VerselfDomain string `yaml:"verself_domain"`
}

func cmdPersona(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("persona: missing subcommand (try `assume` or `user-state`)")
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "assume":
		return cmdPersonaAssume(rest)
	case "user-state":
		return cmdPersonaUserState(rest)
	default:
		return fmt.Errorf("persona: unknown subcommand: %s", sub)
	}
}

func cmdPersonaUserState(args []string) error {
	fs := flagSet("persona user-state")
	opts := &personaOptions{}
	addOperatorRuntimeFlags(&opts.operatorRuntimeOptions)
	fs.StringVar(&opts.site, "site", opts.site, "Deploy site")
	fs.StringVar(&opts.repoRoot, "repo-root", "", "verself-sh checkout root (defaults to cwd)")
	email := fs.String("email", "", "User email")
	org := fs.String("org", "", "Org slug")
	orgID := fs.String("org-id", "", "Verself public org ID")
	orgName := fs.String("org-name", "", "Org display name")
	state := fs.String("state", "", "Billing state")
	planID := fs.String("plan-id", "", "Plan ID")
	productID := fs.String("product-id", billingProductDefault, "Billing product ID")
	balanceUnits := fs.String("balance-units", "", "Balance in product units")
	balanceCents := fs.String("balance-cents", "", "Balance in cents")
	businessNow := fs.String("business-now", "", "Business-time override")
	overagePolicy := fs.String("overage-policy", "", "Overage policy")
	trustTier := fs.String("trust-tier", "", "Trust tier")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *email == "" {
		return errors.New("persona user-state: --email is required")
	}
	if *org == "" && *orgID == "" {
		return errors.New("persona user-state: --org or --org-id is required")
	}
	if *balanceUnits != "" && *balanceCents != "" {
		return errors.New("persona user-state: set only one of --balance-units or --balance-cents")
	}
	return runOperatorRuntime("persona.user_state", opts.operatorRuntimeOptions, 0, opch.Config{Database: "verself"}, func(rt *opruntime.Runtime, _ *opch.Client) error {
		remoteArgs := []string{
			"--pg-dsn", "postgres://billing@/billing?host=/var/run/postgresql&sslmode=disable",
			"--email", *email,
			"--product-id", *productID,
		}
		addStringFlag := func(name, value string) {
			if value != "" {
				remoteArgs = append(remoteArgs, name, value)
			}
		}
		addStringFlag("--org-id", *orgID)
		addStringFlag("--org", *org)
		addStringFlag("--org-name", *orgName)
		addStringFlag("--state", *state)
		addStringFlag("--plan-id", *planID)
		addStringFlag("--balance-units", *balanceUnits)
		addStringFlag("--balance-cents", *balanceCents)
		addStringFlag("--business-now", *businessNow)
		addStringFlag("--overage-policy", *overagePolicy)
		addStringFlag("--trust-tier", *trustTier)
		return runRemoteBazelExecutable(rt, billingSetUserStateTarget, "verself-billing-set-user-state", "billing", remoteArgs)
	})
}

func cmdPersonaAssume(args []string) error {
	fs := flagSet("persona assume")
	opts := &personaOptions{}
	addOperatorRuntimeFlags(&opts.operatorRuntimeOptions)
	fs.StringVar(&opts.site, "site", opts.site, "Deploy site")
	fs.StringVar(&opts.repoRoot, "repo-root", "", "verself-sh checkout root (defaults to cwd)")
	outputPath := fs.String("output", "", "Output env file path")
	printEnv := fs.Bool("print", false, "Print env vars to stdout")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("persona assume: persona name is required")
	}
	name := fs.Arg(0)
	return runOperatorRuntime("persona.assume", opts.operatorRuntimeOptions, 0, opch.Config{Database: "verself"}, func(rt *opruntime.Runtime, _ *opch.Client) error {
		def, err := resolvePersona(rt.RepoRoot, rt.Site, name)
		if err != nil {
			return err
		}
		if *outputPath == "" && !*printEnv {
			*outputPath = filepath.Join(rt.RepoRoot, "smoke-artifacts", "personas", def.Name+".env")
		}
		return assumePersona(rt, def, *outputPath, *printEnv)
	})
}

func resolvePersona(repoRoot, site, name string) (personaDefinition, error) {
	domain, err := loadVerselfDomain(repoRoot, site)
	if err != nil {
		return personaDefinition{}, err
	}
	switch name {
	case "platform-admin":
		return personaDefinition{
			Name:               name,
			HumanEmail:         "agent@" + domain,
			HumanPasswordURI:   "openbao://kv-runtime/secret/org/seed-system.platform_agent_password",
			MachineUsername:    "assume-platform-admin",
			MachineSecretPath:  "openbao://kv-runtime/secret/org/seed-system.assume_platform_admin_client_secret",
			EmailLocalPart:     "agents",
			IncludePlatformOps: true,
			TokenProjects:      []string{productAPIProjectName, "forgejo"},
		}, nil
	case "acme-admin":
		return personaDefinition{
			Name:              name,
			HumanEmail:        "acme-admin@" + domain,
			HumanPasswordURI:  "openbao://kv-runtime/secret/org/seed-system.acme_admin_password",
			MachineUsername:   "assume-acme-admin",
			MachineSecretPath: "openbao://kv-runtime/secret/org/seed-system.assume_acme_admin_client_secret",
			TokenProjects:     []string{productAPIProjectName},
		}, nil
	case "acme-member":
		return personaDefinition{
			Name:              name,
			HumanEmail:        "acme-user@" + domain,
			HumanPasswordURI:  "openbao://kv-runtime/secret/org/seed-system.acme_user_password",
			MachineUsername:   "assume-acme-member",
			MachineSecretPath: "openbao://kv-runtime/secret/org/seed-system.assume_acme_member_client_secret",
			TokenProjects:     []string{productAPIProjectName},
		}, nil
	default:
		return personaDefinition{}, fmt.Errorf("persona must be one of platform-admin, acme-admin, acme-member")
	}
}

func loadVerselfDomain(repoRoot, site string) (string, error) {
	path := siteVarsPath(repoRoot, site)
	var vars hostMainVars
	if err := readYAMLFile(path, &vars); err != nil {
		return "", err
	}
	if strings.TrimSpace(vars.VerselfDomain) == "" {
		return "", fmt.Errorf("%s did not define verself_domain", path)
	}
	return strings.TrimSpace(vars.VerselfDomain), nil
}

func assumePersona(rt *opruntime.Runtime, def personaDefinition, outputPath string, printEnv bool) error {
	return errors.New("persona assume requires an OpenBao-backed operator reveal flow")
}
