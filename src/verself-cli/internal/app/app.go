package app

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	verself "github.com/verself/verself-go"
)

var slugRE = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,78}[a-z0-9])?$`)

type CLI struct {
	binary string
	in     io.Reader
	out    io.Writer
	err    io.Writer
	getenv EnvLookup
}

func MainWithBinary(binary string) int {
	cli := CLI{
		binary: binary,
		in:     os.Stdin,
		out:    os.Stdout,
		err:    os.Stderr,
		getenv: os.Getenv,
	}
	if err := cli.Run(context.Background(), os.Args[1:]); err != nil {
		_, _ = fmt.Fprintln(cli.err, "error:", err)
		return 1
	}
	return 0
}

func (c CLI) Run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return c.usage()
	}
	switch args[0] {
	case "auth":
		return c.runAuth(ctx, args[1:])
	case "company":
		return c.runCompany(ctx, args[1:])
	case "env":
		return c.runEnv(ctx, args[1:])
	case "orgs":
		return c.runOrgs(ctx, args[1:])
	case "projects":
		return c.runProjects(ctx, args[1:])
	case "notifications":
		return c.runNotifications(ctx, args[1:])
	case "audit":
		return c.runAudit(ctx, args[1:])
	case "repos":
		return c.runRepos(ctx, args[1:])
	case "runs":
		return c.runRuns(ctx, args[1:])
	case "github":
		return c.runGitHub(ctx, args[1:])
	case "schedules":
		return c.runSchedules(ctx, args[1:])
	case "billing":
		return c.runBilling(ctx, args[1:])
	case "bootstrap":
		return c.runBootstrap(args[1:])
	case "help", "--help", "-h":
		return c.usage()
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func (c CLI) usage() error {
	return writef(c.out, `Usage:
  %[1]s auth login --token-file PATH [--profile NAME]
  %[1]s auth whoami [--json]
  %[1]s auth token [--audience AUDIENCE]
  %[1]s orgs list [--json]
  %[1]s orgs use <org-id|slug>
  %[1]s orgs inspect [--json]
  %[1]s orgs update --version VERSION [--display-name NAME] [--slug SLUG] [--json]
  %[1]s company configure <name> [flags]
  %[1]s company options add <company> <key> [--from-env KEY|--stdin|--from-file PATH|--value VALUE]
  %[1]s company secret generate <company> --all|--key KEY
  %[1]s projects list [--state active|archived] [--json]
  %[1]s projects get <project-id> [--json]
  %[1]s projects create <display-name> [--slug SLUG] [--description TEXT] [--json]
  %[1]s projects update <project-id> --version VERSION [--slug SLUG] [--display-name NAME] [--description TEXT] [--json]
  %[1]s projects archive <project-id> --version VERSION [--json]
  %[1]s projects restore <project-id> --version VERSION [--json]
  %[1]s projects environments list <project-id> [--json]
  %[1]s projects environments create <project-id> <display-name> --slug SLUG --kind KIND [--json]
  %[1]s notifications list [--limit N] [--json]
  %[1]s notifications summary [--json]
  %[1]s notifications preferences set --version VERSION --enabled true|false [--web-enabled BOOL] [--email-enabled BOOL] [--push-enabled BOOL] [--sms-enabled BOOL] [--json]
  %[1]s notifications read-cursor <sequence> [--json]
  %[1]s notifications read <notification-id> [--json]
  %[1]s notifications dismiss <notification-id> [--json]
  %[1]s notifications clear [--json]
  %[1]s notifications test [--title TEXT] [--body TEXT] [--action-url URL] [--json]
  %[1]s audit events [--high-risk] [--limit N] [--json]
  %[1]s audit exports list [--json]
  %[1]s audit exports create [--scope SCOPE] [--include-logs] [--json]
  %[1]s audit exports get <export-id> [--json]
  %[1]s audit exports download <export-id> [file] [--json]
  %[1]s repos list [--project-id PROJECT_ID] [--json]
  %[1]s repos get <repo-id> [--json]
  %[1]s repos create <project-id> [--description TEXT] [--default-branch BRANCH] [--json]
  %[1]s repos credentials create --scope SCOPE [--label LABEL] [--json]
  %[1]s repos refs <repo-id> [--json]
  %[1]s repos tree <repo-id> [--ref REF] [--path PATH] [--json]
  %[1]s repos blob <repo-id> --path PATH [--ref REF] [--json]
  %[1]s repos checkout-grants create <repo-id> [--ref REF] [--json]
  %[1]s repos workflow-runs list <repo-id> [--json]
  %[1]s repos workflow-runs get <workflow-run-id> [--json]
  %[1]s repos workflow-runs dispatch <repo-id> --project-id PROJECT_ID --workflow-path PATH [--json]
  %[1]s runs list [--status STATUS] [--json]
  %[1]s runs get <execution-id> [--json]
  %[1]s runs get-run <run-id> [--json]
  %[1]s runs logs <execution-id> [--json]
  %[1]s runs search-logs [--query TEXT] [--run-id RUN_ID] [--json]
  %[1]s runs analytics jobs|costs|runner-sizing [--json]
  %[1]s github installations list [--json]
  %[1]s github installations connect [--json]
  %[1]s schedules list [--json]
  %[1]s schedules create --project-id PROJECT_ID --source-repository-id REPO_ID --workflow-path PATH --interval-seconds SECONDS [--json]
  %[1]s schedules get <schedule-id> [--json]
  %[1]s schedules pause <schedule-id> [--json]
  %[1]s schedules resume <schedule-id> [--json]
  %[1]s billing entitlements [--json]
  %[1]s billing contracts [--json]
  %[1]s billing plans --product-id PRODUCT_ID [--json]
  %[1]s billing statement --product-id PRODUCT_ID [--json]
  %[1]s env get <key> --org ORG --project PROJECT --environment ENV
  %[1]s env add <key> --org ORG --project PROJECT --environment ENV --from-env NAME|--from-file PATH|--stdin
  %[1]s env pull [file] --org ORG --project PROJECT --environment ENV
  %[1]s env run --org ORG --project PROJECT --environment ENV -- <command>
  %[1]s env rm <key> --org ORG --project PROJECT --environment ENV
  %[1]s bootstrap --company <name> [--repo-root PATH]
`, c.binary)
}

func (c CLI) runCompany(ctx context.Context, args []string) error {
	_ = ctx
	if len(args) == 0 {
		return errors.New("company command is required")
	}
	switch args[0] {
	case "configure":
		return c.companyConfigure(args[1:])
	case "use":
		return c.companyUse(args[1:])
	case "inspect":
		return c.companyInspect(args[1:])
	case "list":
		return c.companyList()
	case "remove", "rm":
		return c.companyRemove(args[1:])
	case "options":
		return c.companyOptions(args[1:])
	case "secret":
		return c.companySecret(args[1:])
	default:
		return fmt.Errorf("unknown company command %q", args[0])
	}
}

func (c CLI) companyConfigure(args []string) error {
	fs := flag.NewFlagSet("company configure", flag.ContinueOnError)
	fs.SetOutput(c.err)
	var site, productDomain, companyDomain, companyName, ownerAlias, ownerName, cliName, project string
	fs.StringVar(&site, "site", "prod", "site to render")
	fs.StringVar(&productDomain, "product-domain", "", "product domain")
	fs.StringVar(&companyDomain, "company-domain", "", "company domain")
	fs.StringVar(&companyName, "company-name", "", "company display name")
	fs.StringVar(&ownerAlias, "owner-alias", "", "owner email alias")
	fs.StringVar(&ownerName, "owner-name", "", "owner display name")
	fs.StringVar(&cliName, "cli-name", "", "generated cli name")
	fs.StringVar(&project, "project", defaultProject, "project slug")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: company configure <name>")
	}
	name := fs.Arg(0)
	if !slugRE.MatchString(name) {
		return fmt.Errorf("company name must match %s", slugRE.String())
	}
	if cliName == "" {
		cliName = name
	}
	if err := requireConfigureFields(productDomain, companyDomain, companyName, ownerAlias, ownerName, cliName, project); err != nil {
		return err
	}
	if !slugRE.MatchString(cliName) {
		return fmt.Errorf("cli name must match %s", slugRE.String())
	}
	now := time.Now().UTC()
	company := CompanyRecord{
		Version:          currentCompanyVersion,
		Name:             name,
		Site:             site,
		ProductDomain:    productDomain,
		CompanyDomain:    companyDomain,
		CompanyName:      companyName,
		OwnerAlias:       ownerAlias,
		OwnerName:        ownerName,
		CLIName:          cliName,
		Project:          project,
		OrganizationName: companyDomain,
		OwnerEmail:       ownerAlias + "@" + companyDomain,
		TrustTier:        defaultTrustTier,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	store, err := newStore(c.getenv)
	if err != nil {
		return err
	}
	if err := store.SaveCompany(company); err != nil {
		return err
	}
	cfg, err := store.LoadConfig()
	if err != nil {
		return err
	}
	cfg.ActiveCompany = name
	if err := store.SaveConfig(cfg); err != nil {
		return err
	}
	return writef(c.out, "configured company %s\n", name)
}

func requireConfigureFields(values ...string) error {
	for _, v := range values {
		if strings.TrimSpace(v) == "" {
			return errors.New("company configure requires product domain, company domain, company name, owner alias, owner name, cli name, and project")
		}
	}
	return nil
}

func (c CLI) companyUse(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: company use <name>")
	}
	store, err := newStore(c.getenv)
	if err != nil {
		return err
	}
	if _, err := store.LoadCompany(args[0]); err != nil {
		return err
	}
	cfg, err := store.LoadConfig()
	if err != nil {
		return err
	}
	cfg.ActiveCompany = args[0]
	if err := store.SaveConfig(cfg); err != nil {
		return err
	}
	return writef(c.out, "active company %s\n", args[0])
}

func (c CLI) companyInspect(args []string) error {
	fs := flag.NewFlagSet("company inspect", flag.ContinueOnError)
	fs.SetOutput(c.err)
	jsonOut := fs.Bool("json", false, "json output")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	name := ""
	if fs.NArg() > 1 {
		return errors.New("usage: company inspect [name]")
	}
	if fs.NArg() == 1 {
		name = fs.Arg(0)
	}
	store, err := newStore(c.getenv)
	if err != nil {
		return err
	}
	company, err := store.LoadCompany(name)
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(c.out, company)
	}
	return writef(c.out, "%s\n  org: %s\n  owner: %s\n  site: %s\n  cli: %s\n", company.Name, company.OrganizationName, company.OwnerEmail, company.Site, company.CLIName)
}

func (c CLI) companyList() error {
	store, err := newStore(c.getenv)
	if err != nil {
		return err
	}
	companies, err := store.ListCompanies()
	if err != nil {
		return err
	}
	for _, name := range companies {
		if err := writeln(c.out, name); err != nil {
			return err
		}
	}
	return nil
}

func (c CLI) companyRemove(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: company remove <name>")
	}
	store, err := newStore(c.getenv)
	if err != nil {
		return err
	}
	if err := os.Remove(store.paths.companyPath(args[0])); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	cfg, err := store.LoadConfig()
	if err != nil {
		return err
	}
	if cfg.ActiveCompany == args[0] {
		cfg.ActiveCompany = ""
		if err := store.SaveConfig(cfg); err != nil {
			return err
		}
	}
	return writef(c.out, "removed company %s\n", args[0])
}

func (c CLI) companyOptions(args []string) error {
	if len(args) == 0 {
		return errors.New("company options command is required")
	}
	switch args[0] {
	case "add", "set":
		return c.companyOptionSet(args[1:], args[0] == "add")
	case "list":
		return c.companyOptionList(args[1:])
	case "remove", "rm":
		return c.companyOptionRemove(args[1:])
	default:
		return fmt.Errorf("unknown company options command %q", args[0])
	}
}

func (c CLI) companyOptionSet(args []string, allowSecret bool) error {
	fs := flag.NewFlagSet("company options set", flag.ContinueOnError)
	fs.SetOutput(c.err)
	fromEnv := fs.String("from-env", "", "read value from environment variable")
	fromFile := fs.String("from-file", "", "read value from file")
	stdin := fs.Bool("stdin", false, "read value from stdin")
	valueFlag := fs.String("value", "", "literal non-secret value")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 2 {
		return errors.New("usage: company options set <company> <key>")
	}
	companyName, key := fs.Arg(0), fs.Arg(1)
	value, source, secret, err := c.readInputValue(*fromEnv, *fromFile, *stdin, *valueFlag, allowSecret)
	if err != nil {
		return err
	}
	store, err := newStore(c.getenv)
	if err != nil {
		return err
	}
	company, err := store.LoadCompany(companyName)
	if err != nil {
		return err
	}
	opt := classifyOption(company, key)
	opt.Source = source
	opt.UpdatedAt = time.Now().UTC()
	if secret {
		ref, err := store.SaveCredential(value)
		if err != nil {
			return err
		}
		opt.Sensitivity = "secret"
		opt.ValueRef = ref
		opt.Value = ""
	} else {
		opt.Sensitivity = "public"
		opt.Value = value
	}
	upsertOption(&company, opt)
	if err := store.SaveCompany(company); err != nil {
		return err
	}
	return writef(c.out, "set company option %s\n", key)
}

func (c CLI) companyOptionList(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: company options list <company>")
	}
	store, err := newStore(c.getenv)
	if err != nil {
		return err
	}
	company, err := store.LoadCompany(args[0])
	if err != nil {
		return err
	}
	return writeJSON(c.out, company.Options)
}

func (c CLI) companyOptionRemove(args []string) error {
	if len(args) != 2 {
		return errors.New("usage: company options remove <company> <key>")
	}
	store, err := newStore(c.getenv)
	if err != nil {
		return err
	}
	company, err := store.LoadCompany(args[0])
	if err != nil {
		return err
	}
	out := company.Options[:0]
	for _, opt := range company.Options {
		if opt.Name != args[1] {
			out = append(out, opt)
		}
	}
	company.Options = out
	return store.SaveCompany(company)
}

func (c CLI) companySecret(args []string) error {
	if len(args) == 0 {
		return errors.New("company secret command is required")
	}
	switch args[0] {
	case "generate":
		return c.companySecretGenerate(args[1:])
	case "reveal":
		return c.companySecretReveal(args[1:])
	case "set":
		return c.companySecretSet(args[1:])
	case "list":
		return c.companySecretList(args[1:])
	case "remove", "rm":
		return c.companySecretRemove(args[1:])
	default:
		return fmt.Errorf("unknown company secret command %q", args[0])
	}
}

func (c CLI) companySecretGenerate(args []string) error {
	fs := flag.NewFlagSet("company secret generate", flag.ContinueOnError)
	fs.SetOutput(c.err)
	all := fs.Bool("all", false, "generate all known secrets")
	key := fs.String("key", "", "generate one known secret")
	reveal := fs.Bool("reveal-once", false, "print plaintext after writing encrypted/local stores")
	jsonOut := fs.Bool("json", false, "json output")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 || (*all == (*key != "")) {
		return errors.New("usage: company secret generate <company> --all|--key <key>")
	}
	store, err := newStore(c.getenv)
	if err != nil {
		return err
	}
	company, err := store.LoadCompany(fs.Arg(0))
	if err != nil {
		return err
	}
	var items []SecretInventoryItem
	if *all {
		items, err = ensureAllGeneratedSecrets(store, &company, *reveal)
		if err != nil {
			return err
		}
	} else {
		item, err := ensureGeneratedSecret(store, &company, *key, *reveal)
		if err != nil {
			return err
		}
		items = []SecretInventoryItem{item}
	}
	if err := store.SaveCompany(company); err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(c.out, items)
	}
	for _, item := range items {
		if item.Plaintext != "" {
			if err := writef(c.out, "%s: %s\n", item.Key, item.Plaintext); err != nil {
				return err
			}
			continue
		}
		if err := writef(c.out, "generated  %-36s -> %s\n", item.Key, item.Destination); err != nil {
			return err
		}
	}
	return nil
}

func (c CLI) companySecretReveal(args []string) error {
	fs := flag.NewFlagSet("company secret reveal", flag.ContinueOnError)
	fs.SetOutput(c.err)
	key := fs.String("key", "", "secret key")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 || *key == "" {
		return errors.New("usage: company secret reveal <company> --key <key>")
	}
	store, err := newStore(c.getenv)
	if err != nil {
		return err
	}
	company, err := store.LoadCompany(fs.Arg(0))
	if err != nil {
		return err
	}
	secret := findSecret(&company, *key)
	if secret == nil {
		return fmt.Errorf("secret %s not found", *key)
	}
	value, err := store.ReadCredential(secret.ValueRef)
	if err != nil {
		return err
	}
	return writeln(c.out, value)
}

func (c CLI) companySecretSet(args []string) error {
	fs := flag.NewFlagSet("company secret set", flag.ContinueOnError)
	fs.SetOutput(c.err)
	key := fs.String("key", "", "secret key")
	fromEnv := fs.String("from-env", "", "read value from environment variable")
	fromFile := fs.String("from-file", "", "read value from file")
	stdin := fs.Bool("stdin", false, "read value from stdin")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 || *key == "" {
		return errors.New("usage: company secret set <company> --key <key>")
	}
	value, source, _, err := c.readInputValue(*fromEnv, *fromFile, *stdin, "", true)
	if err != nil {
		return err
	}
	_ = source
	store, err := newStore(c.getenv)
	if err != nil {
		return err
	}
	company, err := store.LoadCompany(fs.Arg(0))
	if err != nil {
		return err
	}
	ref, err := store.SaveCredential(value)
	if err != nil {
		return err
	}
	spec := findSecretSpec(company.Site, *key)
	secret := CompanySecret{
		Key:           *key,
		Kind:          spec.Kind,
		Sensitivity:   "secret",
		ValueRef:      ref,
		RenderTargets: spec.RenderTargets,
		RevealCommand: fmt.Sprintf("%s company secret reveal %s --key %s", c.binary, company.Name, *key),
		UpdatedAt:     time.Now().UTC(),
	}
	upsertSecret(&company, secret)
	return store.SaveCompany(company)
}

func (c CLI) companySecretList(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: company secret list <company>")
	}
	store, err := newStore(c.getenv)
	if err != nil {
		return err
	}
	company, err := store.LoadCompany(args[0])
	if err != nil {
		return err
	}
	return writeJSON(c.out, company.Secrets)
}

func (c CLI) companySecretRemove(args []string) error {
	fs := flag.NewFlagSet("company secret remove", flag.ContinueOnError)
	fs.SetOutput(c.err)
	key := fs.String("key", "", "secret key")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 || *key == "" {
		return errors.New("usage: company secret remove <company> --key <key>")
	}
	store, err := newStore(c.getenv)
	if err != nil {
		return err
	}
	company, err := store.LoadCompany(fs.Arg(0))
	if err != nil {
		return err
	}
	out := company.Secrets[:0]
	for _, secret := range company.Secrets {
		if secret.Key != *key {
			out = append(out, secret)
		}
	}
	company.Secrets = out
	return store.SaveCompany(company)
}

func (c CLI) runEnv(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("env command is required")
	}
	switch args[0] {
	case "get":
		return c.envGet(ctx, args[1:])
	case "run":
		return c.envRun(ctx, args[1:])
	case "pull":
		return c.envPull(ctx, args[1:])
	case "ls", "list":
		return c.envList(ctx, args[1:])
	case "add", "set", "update":
		return c.envSet(ctx, args[1:])
	case "rm", "remove":
		return c.envRemove(ctx, args[1:])
	default:
		return fmt.Errorf("unknown env command %q", args[0])
	}
}

func (c CLI) envGet(ctx context.Context, args []string) error {
	fs, scope, serviceFlags, err := envFlagSet("env get", c.err)
	if err != nil {
		return err
	}
	reveal := fs.Bool("reveal-secret", false, "allow secret reveal in non-interactive output")
	jsonOut := fs.Bool("json", false, "json output")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: env get <key> --org <org> --project <project> --environment <env>")
	}
	if err := requireScope(*scope); err != nil {
		return err
	}
	key := fs.Arg(0)
	if !*reveal && !outputIsTerminal(c.out) {
		return errors.New("env get refuses to print secrets to non-terminal stdout without --reveal-secret")
	}
	if isBootstrapScope(*scope) {
		value, err := c.localEnvValue(*scope, key)
		if err != nil {
			return err
		}
		if *jsonOut {
			return writeJSON(c.out, map[string]string{"key": key, "value": value})
		}
		return writeln(c.out, value)
	}
	client, err := c.serviceClient(*serviceFlags)
	if err != nil {
		return err
	}
	resolved, err := client.Secrets.Resolve(ctx, verself.ResolveSecretsInput{
		Scope: secretScopeFromEnvScope(*scope),
		Names: []string{key},
	})
	if err != nil {
		return err
	}
	if len(resolved.Values) == 0 {
		return fmt.Errorf("env key %s not found", key)
	}
	value := resolved.Values[0].Value
	if *jsonOut {
		return writeJSON(c.out, map[string]string{"key": key, "value": value})
	}
	return writeln(c.out, value)
}

func (c CLI) localEnvValue(scope EnvScope, key string) (string, error) {
	store, err := newStore(c.getenv)
	if err != nil {
		return "", err
	}
	env, err := store.LoadEnv(scope)
	if err != nil {
		return "", err
	}
	item, ok := env.Values[key]
	if !ok {
		return "", fmt.Errorf("env key %s not found", key)
	}
	value, err := store.ReadCredential(item.ValueRef)
	if err != nil {
		return "", err
	}
	return value, nil
}

func (c CLI) envRun(ctx context.Context, args []string) error {
	fs, scope, serviceFlags, err := envFlagSet("env run", c.err)
	if err != nil {
		return err
	}
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if err := requireScope(*scope); err != nil {
		return err
	}
	cmdArgs := fs.Args()
	if len(cmdArgs) == 0 {
		return errors.New("usage: env run [flags] -- <command>")
	}
	values, err := c.envValues(ctx, *scope, *serviceFlags)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, cmdArgs[0], cmdArgs[1:]...)
	cmd.Stdin = c.in
	cmd.Stdout = c.out
	cmd.Stderr = c.err
	cmd.Env = os.Environ()
	for key, value := range values {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	return cmd.Run()
}

func (c CLI) envPull(ctx context.Context, args []string) error {
	fs, scope, serviceFlags, err := envFlagSet("env pull", c.err)
	if err != nil {
		return err
	}
	yes := fs.Bool("yes", false, "overwrite output file")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if err := requireScope(*scope); err != nil {
		return err
	}
	file := ".env.local"
	if fs.NArg() > 1 {
		return errors.New("usage: env pull [file]")
	}
	if fs.NArg() == 1 {
		file = fs.Arg(0)
	}
	if !*yes {
		if _, err := os.Stat(file); err == nil {
			return fmt.Errorf("%s exists; pass --yes", file)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	values, err := c.envValues(ctx, *scope, *serviceFlags)
	if err != nil {
		return err
	}
	var b strings.Builder
	for _, key := range sortedKeys(values) {
		b.WriteString(key + "=" + shellQuote(values[key]) + "\n")
	}
	return atomicWriteFile(file, []byte(b.String()), 0o600)
}

func (c CLI) envList(ctx context.Context, args []string) error {
	fs, scope, serviceFlags, err := envFlagSet("env ls", c.err)
	if err != nil {
		return err
	}
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if err := requireScope(*scope); err != nil {
		return err
	}
	values, err := c.envValues(ctx, *scope, *serviceFlags)
	if err != nil {
		return err
	}
	for _, key := range sortedKeys(values) {
		if err := writeln(c.out, key); err != nil {
			return err
		}
	}
	return nil
}

func (c CLI) envSet(ctx context.Context, args []string) error {
	fs, scope, serviceFlags, err := envFlagSet("env add", c.err)
	if err != nil {
		return err
	}
	fromEnv := fs.String("from-env", "", "read value from environment variable")
	fromFile := fs.String("from-file", "", "read value from file")
	stdin := fs.Bool("stdin", false, "read value from stdin")
	idempotencyKey := fs.String("idempotency-key", "", "stable mutation key")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: env add <key> --org <org> --project <project> --environment <env>")
	}
	if err := requireScope(*scope); err != nil {
		return err
	}
	value, _, _, err := c.readInputValue(*fromEnv, *fromFile, *stdin, "", true)
	if err != nil {
		return err
	}
	if isBootstrapScope(*scope) {
		store, err := newStore(c.getenv)
		if err != nil {
			return err
		}
		_, err = store.PutEnvSecret(*scope, fs.Arg(0), value)
		return err
	}
	client, err := c.serviceClient(*serviceFlags)
	if err != nil {
		return err
	}
	_, err = client.Secrets.Put(ctx, fs.Arg(0), verself.PutSecretInput{
		Scope:          secretScopeFromEnvScope(*scope),
		Value:          value,
		IdempotencyKey: *idempotencyKey,
	})
	return err
}

func (c CLI) envRemove(ctx context.Context, args []string) error {
	fs, scope, serviceFlags, err := envFlagSet("env rm", c.err)
	if err != nil {
		return err
	}
	idempotencyKey := fs.String("idempotency-key", "", "stable mutation key")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: env rm <key> --org <org> --project <project> --environment <env>")
	}
	if err := requireScope(*scope); err != nil {
		return err
	}
	if isBootstrapScope(*scope) {
		store, err := newStore(c.getenv)
		if err != nil {
			return err
		}
		env, err := store.LoadEnv(*scope)
		if err != nil {
			return err
		}
		delete(env.Values, fs.Arg(0))
		return store.SaveEnv(env)
	}
	client, err := c.serviceClient(*serviceFlags)
	if err != nil {
		return err
	}
	_, err = client.Secrets.Delete(ctx, fs.Arg(0), verself.DeleteSecretInput{
		Scope:          secretScopeFromEnvScope(*scope),
		IdempotencyKey: *idempotencyKey,
	})
	return err
}

func (c CLI) envValues(ctx context.Context, scope EnvScope, serviceFlags serviceClientFlags) (map[string]string, error) {
	if isBootstrapScope(scope) {
		return c.localEnvValues(scope)
	}
	client, err := c.serviceClient(serviceFlags)
	if err != nil {
		return nil, err
	}
	resolved, err := client.Secrets.Resolve(ctx, verself.ResolveSecretsInput{
		Scope: secretScopeFromEnvScope(scope),
	})
	if err != nil {
		return nil, err
	}
	values := make(map[string]string, len(resolved.Values))
	for _, value := range resolved.Values {
		values[value.Name] = value.Value
	}
	return values, nil
}

func (c CLI) localEnvValues(scope EnvScope) (map[string]string, error) {
	store, err := newStore(c.getenv)
	if err != nil {
		return nil, err
	}
	env, err := store.LoadEnv(scope)
	if err != nil {
		return nil, err
	}
	values := make(map[string]string, len(env.Values))
	for key, item := range env.Values {
		value, err := store.ReadCredential(item.ValueRef)
		if err != nil {
			return nil, err
		}
		values[key] = value
	}
	return values, nil
}

func (c CLI) runBootstrap(args []string) error {
	fs := flag.NewFlagSet("bootstrap", flag.ContinueOnError)
	fs.SetOutput(c.err)
	companyName := fs.String("company", "", "company name")
	repoRoot := fs.String("repo-root", ".", "repo root")
	site := fs.String("site", "", "override site")
	force := fs.Bool("force", false, "overwrite existing render files")
	jsonOut := fs.Bool("json", false, "json output")
	var setFlags keyValueFlags
	fs.Var(&setFlags, "set", "one-run public company option override as key=value")
	fs.Var(&setFlags, "option", "one-run public company option override as key=value")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	overrides, err := setFlags.optionOverrides()
	if err != nil {
		return err
	}
	store, err := newStore(c.getenv)
	if err != nil {
		return err
	}
	company, err := store.LoadCompany(*companyName)
	if err != nil {
		return err
	}
	run, err := renderBootstrap(store, company, RenderOptions{RepoRoot: *repoRoot, Site: *site, Force: *force, OptionOverrides: overrides})
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(c.out, run)
	}
	return writef(c.out, "rendered bootstrap artifacts for %s at %s\n", run.Company, run.RepoRoot)
}

func envFlagSet(name string, stderr io.Writer) (*flag.FlagSet, *EnvScope, *serviceClientFlags, error) {
	fs, serviceFlags := serviceFlagSet(name, stderr)
	scope := &EnvScope{}
	fs.StringVar(&scope.Org, "org", "", "organization")
	fs.StringVar(&scope.Project, "project", "", "project")
	fs.StringVar(&scope.Environment, "environment", "development", "environment")
	return fs, scope, serviceFlags, nil
}

func requireScope(scope EnvScope) error {
	if scope.Org == "" || scope.Project == "" || scope.Environment == "" {
		return errors.New("org, project, and environment are required")
	}
	return nil
}

func isBootstrapScope(scope EnvScope) bool {
	return strings.TrimSpace(scope.Environment) == envBootstrap
}

func secretScopeFromEnvScope(scope EnvScope) verself.SecretScope {
	return verself.SecretScope{
		Level:    verself.SecretScopeEnvironment,
		SourceID: scope.Project,
		EnvID:    scope.Environment,
	}
}

func sortedKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func (c CLI) readInputValue(fromEnv, fromFile string, stdin bool, literal string, allowSecret bool) (value string, source string, secret bool, err error) {
	count := 0
	for _, set := range []bool{fromEnv != "", fromFile != "", stdin, literal != ""} {
		if set {
			count++
		}
	}
	if count != 1 {
		return "", "", false, errors.New("provide exactly one of --from-env, --from-file, --stdin, or --value")
	}
	switch {
	case fromEnv != "":
		if !allowSecret {
			return "", "", false, errors.New("--from-env is only accepted for secret-valued add commands")
		}
		value = c.getenv(fromEnv)
		if value == "" {
			return "", "", false, fmt.Errorf("environment variable %s is empty", fromEnv)
		}
		return value, "env:" + fromEnv, true, nil
	case fromFile != "":
		if !allowSecret {
			return "", "", false, errors.New("--from-file is only accepted for secret-valued add commands")
		}
		data, err := os.ReadFile(fromFile)
		if err != nil {
			return "", "", false, err
		}
		return strings.TrimRight(string(data), "\n"), "file:" + fromFile, true, nil
	case stdin:
		if !allowSecret {
			return "", "", false, errors.New("--stdin is only accepted for secret-valued add commands")
		}
		data, err := io.ReadAll(c.in)
		if err != nil {
			return "", "", false, err
		}
		return strings.TrimRight(string(data), "\n"), "stdin", true, nil
	default:
		return literal, "literal", false, nil
	}
}

func classifyOption(company CompanyRecord, name string) CompanyOption {
	parts := strings.SplitN(name, ".", 2)
	opt := CompanyOption{Name: name}
	if len(parts) == 2 {
		opt.Provider = parts[0]
		opt.Kind = parts[1]
	}
	target := func(file string) []string {
		return []string{filepath.ToSlash(filepath.Join("src", "host", "sites", company.Site, "secrets", file))}
	}
	switch {
	case name == "latitude.api_token":
		opt.Purpose = "infrastructure"
		opt.RenderTargets = target("provisioning.sops.yml")
		opt.RequiredBy = "provisioning"
	case strings.HasPrefix(name, "cloudflare."):
		opt.Purpose = "infrastructure"
		opt.RenderTargets = target("external.sops.yml")
		opt.RequiredBy = "dns"
	case strings.HasPrefix(name, "stripe."):
		opt.Purpose = "billing"
		opt.RenderTargets = target("external.sops.yml")
		opt.RequiredBy = "billing-service.runtime"
	case strings.HasPrefix(name, "aws."):
		opt.Purpose = "backup"
		opt.RenderTargets = target("external.sops.yml")
		opt.RequiredBy = "backup.runtime"
	case strings.HasPrefix(name, "resend."):
		opt.Purpose = "notification"
		opt.RenderTargets = target("external.sops.yml")
		opt.RequiredBy = "notifications-service.runtime"
	default:
		opt.Purpose = "runtime_integration"
		opt.RenderTargets = target("external.sops.yml")
	}
	return opt
}

func upsertOption(company *CompanyRecord, opt CompanyOption) {
	for i := range company.Options {
		if company.Options[i].Name == opt.Name {
			company.Options[i] = opt
			return
		}
	}
	company.Options = append(company.Options, opt)
}

func findSecretSpec(site, key string) SecretSpec {
	for _, spec := range secretCatalog(site) {
		if spec.Key == key {
			return spec
		}
	}
	return SecretSpec{
		Key:           key,
		Kind:          "secret",
		RenderTargets: []string{"src/host/sites/" + site + "/secrets/external.sops.yml"},
		Generator:     func() (string, error) { return randomSecret(32) },
	}
}

func writeJSON(w io.Writer, value any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(value)
}

func writef(w io.Writer, format string, args ...any) error {
	_, err := fmt.Fprintf(w, format, args...)
	return err
}

func writeln(w io.Writer, args ...any) error {
	_, err := fmt.Fprintln(w, args...)
	return err
}

func outputIsTerminal(w io.Writer) bool {
	file, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func parseInterspersed(fs *flag.FlagSet, args []string) error {
	reordered := make([]string, 0, len(args))
	positionals := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			positionals = append(positionals, args[i:]...)
			break
		}
		if strings.HasPrefix(arg, "-") && arg != "-" {
			reordered = append(reordered, arg)
			name := strings.TrimLeft(arg, "-")
			if before, _, ok := strings.Cut(name, "="); ok {
				name = before
			}
			known := fs.Lookup(name)
			if known != nil && !flagTakesNoValue(known) && !strings.Contains(arg, "=") {
				if i+1 < len(args) {
					i++
					reordered = append(reordered, args[i])
				}
			}
			continue
		}
		positionals = append(positionals, arg)
	}
	reordered = append(reordered, positionals...)
	return fs.Parse(reordered)
}

func flagTakesNoValue(f *flag.Flag) bool {
	value, ok := f.Value.(interface{ IsBoolFlag() bool })
	return ok && value.IsBoolFlag()
}

type keyValueFlags []string

func (f *keyValueFlags) String() string {
	return strings.Join(*f, ",")
}

func (f *keyValueFlags) Set(value string) error {
	key, raw, ok := strings.Cut(value, "=")
	if !ok || strings.TrimSpace(key) == "" {
		return fmt.Errorf("override must be key=value: %s", value)
	}
	if isSecretLikeOption(key) {
		return fmt.Errorf("secret-looking option %s must be supplied with company options add, not bootstrap --set", key)
	}
	*f = append(*f, key+"="+raw)
	return nil
}

func (f keyValueFlags) optionOverrides() ([]OptionOverride, error) {
	out := make([]OptionOverride, 0, len(f))
	for _, value := range f {
		key, raw, ok := strings.Cut(value, "=")
		if !ok {
			return nil, fmt.Errorf("override must be key=value: %s", value)
		}
		out = append(out, OptionOverride{Name: key, Value: raw})
	}
	return out, nil
}

func isSecretLikeOption(name string) bool {
	lower := strings.ToLower(name)
	for _, marker := range []string{"password", "secret", "token", "private_key", "access_key"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}
