package main

import (
	"bytes"
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
	"strings"
	"time"

	opch "github.com/verself/operator-runtime/clickhouse"
	opruntime "github.com/verself/operator-runtime/runtime"
)

const (
	siteDefaultName            = "prod"
	siteTofuModuleRel          = "src/tools/provisioning/terraform"
	siteTofuStateRootRel       = ".verself/opentofu/sites"
	siteSecretValueMaxBytes    = 1 << 20
	siteSSHCommandBudget       = 5 * time.Minute
	siteProvisionCommandBudget = 30 * time.Minute
)

var (
	siteNameRE   = regexp.MustCompile(`^[a-z][a-z0-9-]{0,30}[a-z0-9]$`)
	siteSecretRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,190}[A-Za-z0-9]$`)
)

type siteOptions struct {
	repoRoot string
	site     string
}

type siteProvisioningVars struct {
	ClusterName      string `json:"cluster_name"`
	ProjectID        string `json:"project_id"`
	SSHPublicKeyPath string `json:"ssh_public_key_path"`
	WorkerCount      int    `json:"worker_count"`
	InfraCount       int    `json:"infra_count"`
}

type tofuOutput struct {
	Value json.RawMessage `json:"value"`
}

type siteTofuOutputs struct {
	WorkerIPs []string
	InfraIPs  []string
}

type siteHost struct {
	Alias string
	Host  string
}

func cmdSite(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("site: missing subcommand")
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "validate":
		return cmdSiteValidate(rest)
	case "allocate":
		return cmdSiteAllocate(rest)
	case "destroy":
		return cmdSiteDestroy(rest)
	case "install-tools":
		return cmdSiteInstallTools(rest)
	case "secret":
		return cmdSiteSecret(rest)
	case "verify":
		return cmdSiteVerify(rest)
	default:
		return fmt.Errorf("site: unknown subcommand: %s", sub)
	}
}

func addSiteFlags(fs *flag.FlagSet, opts *siteOptions) {
	fs.StringVar(&opts.repoRoot, "repo-root", "", "Repository root. Defaults to cwd.")
	fs.StringVar(&opts.site, "site", envOr("VERSELF_SITE", siteDefaultName), "Deployment site.")
}

func normalizeSiteOptions(opts siteOptions) (siteOptions, error) {
	if opts.repoRoot == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return siteOptions{}, fmt.Errorf("site: cwd: %w", err)
		}
		opts.repoRoot = cwd
	}
	opts.repoRoot = filepath.Clean(opts.repoRoot)
	opts.site = strings.TrimSpace(opts.site)
	if opts.site == "" {
		opts.site = siteDefaultName
	}
	if err := validateSiteName(opts.site); err != nil {
		return siteOptions{}, err
	}
	return opts, nil
}

func validateSiteName(site string) error {
	if !siteNameRE.MatchString(site) {
		return fmt.Errorf("site name %q must match %s", site, siteNameRE.String())
	}
	return nil
}

func cmdSiteValidate(args []string) error {
	fs := flagSet("site validate")
	opts := siteOptions{}
	addSiteFlags(fs, &opts)
	requireInventory := fs.Bool("require-inventory", false, "Require src/host/sites/<site>/inventory.ini to exist.")
	if err := fs.Parse(args); err != nil {
		return err
	}
	opts, err := normalizeSiteOptions(opts)
	if err != nil {
		return err
	}
	checks := []struct {
		name string
		path string
	}{
		{name: "vars", path: siteVarsPath(opts.repoRoot, opts.site)},
		{name: "provisioning_tfvars", path: filepath.Join(siteDir(opts.repoRoot, opts.site), "provisioning.tfvars.json")},
		{name: "site_json", path: filepath.Join(siteDir(opts.repoRoot, opts.site), "site.json")},
		{name: "integration_catalog", path: filepath.Join(opts.repoRoot, "src", "integrations", "catalog", "sites", opts.site+".yml")},
	}
	for _, check := range checks {
		if err := requireRegularFile(check.path); err != nil {
			return fmt.Errorf("site validate %s: %w", check.name, err)
		}
		fmt.Printf("%s\t%s\n", check.name, repoRel(opts.repoRoot, check.path))
	}
	if _, err := readSiteProvisioningVars(filepath.Join(siteDir(opts.repoRoot, opts.site), "provisioning.tfvars.json"), false); err != nil {
		return fmt.Errorf("site validate provisioning_tfvars: %w", err)
	}
	inventoryPath := opruntime.InventoryPath(opts.repoRoot, opts.site)
	if fileExists(inventoryPath) {
		fmt.Printf("inventory\t%s\n", repoRel(opts.repoRoot, inventoryPath))
	} else if *requireInventory {
		return fmt.Errorf("site validate inventory: %s not found; run aspect site allocate --site=%s --confirm", repoRel(opts.repoRoot, inventoryPath), opts.site)
	}
	return nil
}

func cmdSiteAllocate(args []string) error {
	fs := flagSet("site allocate")
	opts := siteOptions{}
	addSiteFlags(fs, &opts)
	tofuBin := fs.String("tofu-bin", envOr("TOFU_BIN", ""), "OpenTofu executable resolved by Bazel.")
	tfvarsPath := fs.String("tfvars", "", "Site-local tfvars JSON. Defaults to src/host/sites/<site>/provisioning.tfvars.json.")
	confirm := fs.Bool("confirm", false, "Required confirmation for provider-side allocation.")
	if err := fs.Parse(args); err != nil {
		return err
	}
	opts, err := normalizeSiteOptions(opts)
	if err != nil {
		return err
	}
	if !*confirm {
		return errors.New("site allocate: pass --confirm to create provider resources")
	}
	if strings.TrimSpace(*tofuBin) == "" {
		return errors.New("site allocate: --tofu-bin is required")
	}
	if strings.TrimSpace(os.Getenv("LATITUDESH_AUTH_TOKEN")) == "" {
		return errors.New("site allocate: LATITUDESH_AUTH_TOKEN is required in the controller environment")
	}
	if *tfvarsPath == "" {
		*tfvarsPath = filepath.Join(siteDir(opts.repoRoot, opts.site), "provisioning.tfvars.json")
	}
	tfvars, err := readSiteProvisioningVars(*tfvarsPath, true)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), siteProvisionCommandBudget)
	defer cancel()
	outputs, err := runTofuApply(ctx, opts, *tofuBin, *tfvarsPath)
	if err != nil {
		return err
	}
	if err := writeSiteInventory(opts, tfvars, outputs); err != nil {
		return err
	}
	fmt.Printf("site allocate: wrote %s\n", repoRel(opts.repoRoot, opruntime.InventoryPath(opts.repoRoot, opts.site)))
	return nil
}

func cmdSiteDestroy(args []string) error {
	fs := flagSet("site destroy")
	opts := siteOptions{}
	addSiteFlags(fs, &opts)
	tofuBin := fs.String("tofu-bin", envOr("TOFU_BIN", ""), "OpenTofu executable resolved by Bazel.")
	tfvarsPath := fs.String("tfvars", "", "Site-local tfvars JSON. Defaults to src/host/sites/<site>/provisioning.tfvars.json.")
	confirm := fs.Bool("confirm", false, "Required confirmation for provider-side destruction.")
	if err := fs.Parse(args); err != nil {
		return err
	}
	opts, err := normalizeSiteOptions(opts)
	if err != nil {
		return err
	}
	if !*confirm {
		return errors.New("site destroy: pass --confirm to destroy provider resources")
	}
	if strings.TrimSpace(*tofuBin) == "" {
		return errors.New("site destroy: --tofu-bin is required")
	}
	if strings.TrimSpace(os.Getenv("LATITUDESH_AUTH_TOKEN")) == "" {
		return errors.New("site destroy: LATITUDESH_AUTH_TOKEN is required in the controller environment")
	}
	if *tfvarsPath == "" {
		*tfvarsPath = filepath.Join(siteDir(opts.repoRoot, opts.site), "provisioning.tfvars.json")
	}
	if _, err := readSiteProvisioningVars(*tfvarsPath, true); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), siteProvisionCommandBudget)
	defer cancel()
	if err := runTofuDestroy(ctx, opts, *tofuBin, *tfvarsPath); err != nil {
		return err
	}
	fmt.Printf("site destroy: OpenTofu destroy completed for %s\n", opts.site)
	return nil
}

func cmdSiteInstallTools(args []string) error {
	fs := flagSet("site install-tools")
	opts := siteOptions{}
	addSiteFlags(fs, &opts)
	archive := fs.String("archive", "", "Bazel-built host substrate tools archive.")
	if err := fs.Parse(args); err != nil {
		return err
	}
	opts, err := normalizeSiteOptions(opts)
	if err != nil {
		return err
	}
	if strings.TrimSpace(*archive) == "" {
		return errors.New("site install-tools: --archive is required")
	}
	archivePath, err := filepath.Abs(*archive)
	if err != nil {
		return fmt.Errorf("site install-tools: resolve archive path: %w", err)
	}
	if err := requireRegularFile(archivePath); err != nil {
		return fmt.Errorf("site install-tools: %w", err)
	}
	return runSiteSSH("site.install-tools", opts, siteSSHCommandBudget, func(rt *opruntime.Runtime) error {
		remotePath, err := rt.SSH.UploadExecutable(rt.Ctx, archivePath, "verself-host-substrate-tools")
		if err != nil {
			return err
		}
		defer func() { _ = rt.SSH.RemoveRemotePath(contextWithoutCancel(rt.Ctx), remotePath) }()
		if err := rt.SSH.RunArgv(rt.Ctx, "", []string{"sudo", "/usr/bin/install", "-d", "-m", "0755", "/opt/verself/profile/bin"}, nil, os.Stdout, os.Stderr); err != nil {
			return err
		}
		if err := rt.SSH.RunArgv(rt.Ctx, "", []string{"sudo", "/bin/tar", "-xf", remotePath, "-C", "/"}, nil, os.Stdout, os.Stderr); err != nil {
			return err
		}
		fmt.Printf("site install-tools: installed %s on %s\n", filepath.Base(archivePath), rt.Target.Alias)
		return nil
	})
}

func cmdSiteSecret(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("site secret: missing subcommand (put|get)")
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "put":
		return cmdSiteSecretPut(rest)
	case "get":
		return cmdSiteSecretGet(rest)
	default:
		return fmt.Errorf("site secret: unknown subcommand: %s", sub)
	}
}

func cmdSiteSecretPut(args []string) error {
	fs := flagSet("site secret put")
	opts := siteOptions{}
	addSiteFlags(fs, &opts)
	name := fs.String("name", "", "Site configuration key.")
	valueStdin := fs.Bool("value-stdin", false, "Read the secret value from stdin.")
	valueEnv := fs.String("value-env", "", "Read the secret value from this environment variable.")
	if err := fs.Parse(args); err != nil {
		return err
	}
	opts, err := normalizeSiteOptions(opts)
	if err != nil {
		return err
	}
	secretName, err := normalizeSiteSecretName(*name)
	if err != nil {
		return err
	}
	value, err := readSiteSecretValue(*valueStdin, *valueEnv)
	if err != nil {
		return err
	}
	return runSiteSSH("site.secret.put", opts, siteSSHCommandBudget, func(rt *opruntime.Runtime) error {
		client, err := opruntime.OpenSiteConfig(rt.Ctx, rt)
		if err != nil {
			return err
		}
		if err := client.EnsureMount(rt.Ctx); err != nil {
			return err
		}
		if err := client.WriteValue(rt.Ctx, secretName, value); err != nil {
			return err
		}
		fmt.Printf("site secret put: wrote %s/%s\n", opruntime.SiteConfigKVMount, siteSecretPath(secretName))
		return nil
	})
}

func cmdSiteSecretGet(args []string) error {
	fs := flagSet("site secret get")
	opts := siteOptions{}
	addSiteFlags(fs, &opts)
	name := fs.String("name", "", "Site configuration key.")
	if err := fs.Parse(args); err != nil {
		return err
	}
	opts, err := normalizeSiteOptions(opts)
	if err != nil {
		return err
	}
	secretName, err := normalizeSiteSecretName(*name)
	if err != nil {
		return err
	}
	return runSiteSSH("site.secret.get", opts, siteSSHCommandBudget, func(rt *opruntime.Runtime) error {
		client, err := opruntime.OpenSiteConfig(rt.Ctx, rt)
		if err != nil {
			return err
		}
		value, err := client.ReadValue(rt.Ctx, secretName)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintln(os.Stdout, value); err != nil {
			return fmt.Errorf("write secret value: %w", err)
		}
		return nil
	})
}

func cmdSiteVerify(args []string) error {
	fs := flagSet("site verify")
	opts := operatorRuntimeOptions{}
	addOperatorRuntimeFlags(&opts)
	format := fs.String("format", "table", "Output format: table | json | csv | tsv")
	minutes := fs.Int("minutes", 120, "Lookback window in minutes.")
	fs.StringVar(&opts.repoRoot, "repo-root", "", "Repository root. Defaults to cwd.")
	fs.StringVar(&opts.site, "site", opts.site, "Deployment site.")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := validateSiteName(opts.site); err != nil {
		return err
	}
	if *minutes <= 0 {
		return errors.New("site verify: --minutes must be positive")
	}
	return runOperatorRuntime("site.verify", opts, 45*time.Second, opch.Config{}, func(rt *opruntime.Runtime, ch *opch.Client) error {
		table, err := ch.QueryTableParams(rt.Ctx, `
SELECT
  formatDateTime(event_at, '%FT%TZ', 'UTC') AS event_at_utc,
  command,
  status,
  target_host,
  trace_id
FROM verself.operator_command_runs
WHERE site = {site:String}
  AND command LIKE 'site.%'
  AND event_at >= now() - toIntervalMinute({minutes:UInt32})
ORDER BY event_at DESC
LIMIT 30`, map[string]string{
			"site":    opts.site,
			"minutes": fmt.Sprintf("%d", *minutes),
		})
		if err != nil {
			return err
		}
		return printTableFormat(os.Stdout, table, *format)
	})
}

func runSiteSSH(command string, opts siteOptions, budget time.Duration, fn func(*opruntime.Runtime) error) error {
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()
	return opruntime.Run(ctx, opruntime.Options{
		ServiceName:    serviceName,
		ServiceVersion: serviceVersion,
		Command:        command,
		RepoRoot:       opts.repoRoot,
		Site:           opts.site,
		NeedSSH:        true,
		NeedOTel:       false,
		Interactive:    false,
		UseRecovery:    operatorUseRecovery(),
	}, fn)
}

func readSiteProvisioningVars(path string, requireProviderInputs bool) (siteProvisioningVars, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return siteProvisioningVars{}, fmt.Errorf("read site provisioning vars %s: %w", path, err)
	}
	var vars siteProvisioningVars
	if err := json.Unmarshal(data, &vars); err != nil {
		return siteProvisioningVars{}, fmt.Errorf("parse site provisioning vars %s: %w", path, err)
	}
	if strings.TrimSpace(vars.ClusterName) == "" {
		return siteProvisioningVars{}, fmt.Errorf("site provisioning vars %s: cluster_name is required", path)
	}
	if !siteNameRE.MatchString(vars.ClusterName) {
		return siteProvisioningVars{}, fmt.Errorf("site provisioning vars %s: cluster_name %q must match %s", path, vars.ClusterName, siteNameRE.String())
	}
	if requireProviderInputs && strings.TrimSpace(vars.ProjectID) == "" && strings.TrimSpace(os.Getenv("LATITUDESH_PROJECT_ID")) == "" && strings.TrimSpace(os.Getenv("TF_VAR_project_id")) == "" {
		return siteProvisioningVars{}, fmt.Errorf("site provisioning vars %s: project_id is required unless LATITUDESH_PROJECT_ID or TF_VAR_project_id is set", path)
	}
	if requireProviderInputs && strings.TrimSpace(vars.SSHPublicKeyPath) == "" && strings.TrimSpace(os.Getenv("VERSELF_PROVISIONING_SSH_PUBLIC_KEY_PATH")) == "" && strings.TrimSpace(os.Getenv("TF_VAR_ssh_public_key_path")) == "" {
		return siteProvisioningVars{}, fmt.Errorf("site provisioning vars %s: ssh_public_key_path is required unless VERSELF_PROVISIONING_SSH_PUBLIC_KEY_PATH or TF_VAR_ssh_public_key_path is set", path)
	}
	if vars.WorkerCount < 0 || vars.InfraCount < 0 {
		return siteProvisioningVars{}, fmt.Errorf("site provisioning vars %s: worker_count and infra_count cannot be negative", path)
	}
	return vars, nil
}

func runTofuApply(ctx context.Context, opts siteOptions, tofuBin, tfvarsPath string) (siteTofuOutputs, error) {
	if err := ensureTofuStateDir(opts); err != nil {
		return siteTofuOutputs{}, err
	}
	if err := runTofu(ctx, opts, tofuBin, "init", "-input=false"); err != nil {
		return siteTofuOutputs{}, err
	}
	if err := runTofu(ctx, opts, tofuBin, "apply", "-input=false", "-auto-approve", "-var-file="+tfvarsPath, "-state="+tofuStatePath(opts)); err != nil {
		return siteTofuOutputs{}, err
	}
	output, err := captureTofu(ctx, opts, tofuBin, "output", "-json", "-state="+tofuStatePath(opts))
	if err != nil {
		return siteTofuOutputs{}, err
	}
	return parseTofuOutputs(output)
}

func runTofuDestroy(ctx context.Context, opts siteOptions, tofuBin, tfvarsPath string) error {
	if err := ensureTofuStateDir(opts); err != nil {
		return err
	}
	if err := runTofu(ctx, opts, tofuBin, "init", "-input=false"); err != nil {
		return err
	}
	return runTofu(ctx, opts, tofuBin, "destroy", "-input=false", "-auto-approve", "-var-file="+tfvarsPath, "-state="+tofuStatePath(opts))
}

func runTofu(ctx context.Context, opts siteOptions, tofuBin string, args ...string) error {
	cmd := exec.CommandContext(ctx, tofuBin, args...)
	cmd.Dir = filepath.Join(opts.repoRoot, siteTofuModuleRel)
	cmd.Env = tofuEnv(opts)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("tofu %s: %w", strings.Join(args, " "), err)
	}
	return nil
}

func captureTofu(ctx context.Context, opts siteOptions, tofuBin string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, tofuBin, args...)
	cmd.Dir = filepath.Join(opts.repoRoot, siteTofuModuleRel)
	cmd.Env = tofuEnv(opts)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("tofu %s: %w (%s)", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

func tofuEnv(opts siteOptions) []string {
	env := os.Environ()
	env = append(env, "TF_DATA_DIR="+filepath.Join(tofuSiteDir(opts), "data"))
	if value := strings.TrimSpace(os.Getenv("LATITUDESH_PROJECT_ID")); value != "" && strings.TrimSpace(os.Getenv("TF_VAR_project_id")) == "" {
		env = append(env, "TF_VAR_project_id="+value)
	}
	if value := strings.TrimSpace(os.Getenv("VERSELF_PROVISIONING_SSH_PUBLIC_KEY_PATH")); value != "" && strings.TrimSpace(os.Getenv("TF_VAR_ssh_public_key_path")) == "" {
		env = append(env, "TF_VAR_ssh_public_key_path="+value)
	}
	return env
}

func ensureTofuStateDir(opts siteOptions) error {
	return os.MkdirAll(tofuSiteDir(opts), 0o700)
}

func tofuSiteDir(opts siteOptions) string {
	return filepath.Join(opts.repoRoot, siteTofuStateRootRel, opts.site)
}

func tofuStatePath(opts siteOptions) string {
	return filepath.Join(tofuSiteDir(opts), "terraform.tfstate")
}

func parseTofuOutputs(raw []byte) (siteTofuOutputs, error) {
	var decoded map[string]tofuOutput
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return siteTofuOutputs{}, fmt.Errorf("parse tofu output json: %w", err)
	}
	workerIPs, err := stringSliceOutput(decoded, "worker_ips")
	if err != nil {
		return siteTofuOutputs{}, err
	}
	infraIPs, err := stringSliceOutput(decoded, "infra_ips")
	if err != nil {
		return siteTofuOutputs{}, err
	}
	if len(workerIPs) == 0 && len(infraIPs) == 0 {
		return siteTofuOutputs{}, errors.New("tofu outputs contain no worker_ips or infra_ips")
	}
	return siteTofuOutputs{WorkerIPs: workerIPs, InfraIPs: infraIPs}, nil
}

func stringSliceOutput(outputs map[string]tofuOutput, name string) ([]string, error) {
	output, ok := outputs[name]
	if !ok || len(output.Value) == 0 || string(output.Value) == "null" {
		return nil, nil
	}
	var values []string
	if err := json.Unmarshal(output.Value, &values); err != nil {
		return nil, fmt.Errorf("parse tofu output %s: %w", name, err)
	}
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("tofu output %s contains an empty address", name)
		}
	}
	return values, nil
}

func writeSiteInventory(opts siteOptions, vars siteProvisioningVars, outputs siteTofuOutputs) error {
	workers := make([]siteHost, 0, len(outputs.WorkerIPs))
	for i, ip := range outputs.WorkerIPs {
		workers = append(workers, siteHost{Alias: fmt.Sprintf("vs-%s-w%d", vars.ClusterName, i), Host: ip})
	}
	infra := make([]siteHost, 0, len(outputs.InfraIPs))
	for i, ip := range outputs.InfraIPs {
		infra = append(infra, siteHost{Alias: fmt.Sprintf("vs-%s-i%d", vars.ClusterName, i), Host: ip})
	}
	if len(infra) == 0 && len(workers) > 0 {
		infra = append(infra, workers[0])
	}
	if len(infra) == 0 {
		return errors.New("site inventory needs at least one infra host")
	}
	path := opruntime.InventoryPath(opts.repoRoot, opts.site)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create site inventory directory: %w", err)
	}
	data := renderSiteInventory(workers, infra)
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		return fmt.Errorf("write site inventory %s: %w", path, err)
	}
	return nil
}

func renderSiteInventory(workers, infra []siteHost) string {
	var b strings.Builder
	b.WriteString("[workers]\n")
	for _, host := range workers {
		b.WriteString(host.Alias)
		b.WriteString(" ansible_host=")
		b.WriteString(host.Host)
		b.WriteByte('\n')
	}
	b.WriteString("\n[infra]\n")
	for _, host := range infra {
		b.WriteString(host.Alias)
		b.WriteString(" ansible_host=")
		b.WriteString(host.Host)
		b.WriteByte('\n')
	}
	b.WriteString("\n[all:vars]\n")
	b.WriteString("ansible_user=ubuntu\n")
	b.WriteString("ansible_python_interpreter=/usr/bin/python3\n")
	b.WriteString("ansible_ssh_extra_args=-o IdentitiesOnly=yes\n")
	return b.String()
}

func normalizeSiteSecretName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if !siteSecretRE.MatchString(name) {
		return "", fmt.Errorf("site secret name %q must match %s", name, siteSecretRE.String())
	}
	return name, nil
}

func readSiteSecretValue(valueStdin bool, valueEnv string) (string, error) {
	sources := 0
	if valueStdin {
		sources++
	}
	if strings.TrimSpace(valueEnv) != "" {
		sources++
	}
	if sources != 1 {
		return "", errors.New("site secret put: set exactly one of --value-stdin or --value-env")
	}
	var raw []byte
	var err error
	if valueStdin {
		raw, err = io.ReadAll(io.LimitReader(os.Stdin, siteSecretValueMaxBytes+1))
		if err != nil {
			return "", fmt.Errorf("read secret value from stdin: %w", err)
		}
	} else {
		raw = []byte(os.Getenv(valueEnv))
	}
	if len(raw) > siteSecretValueMaxBytes {
		return "", fmt.Errorf("site secret value exceeds %d bytes", siteSecretValueMaxBytes)
	}
	value := strings.TrimRight(string(raw), "\r\n")
	if value == "" {
		return "", errors.New("site secret value is empty")
	}
	return value, nil
}

func siteSecretPath(name string) string {
	return opruntime.SiteConfigSecretPrefix + "/" + name
}

func siteDir(repoRoot, site string) string {
	return filepath.Join(repoRoot, "src", "host", "sites", site)
}

func requireRegularFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("%s is a directory", path)
	}
	return nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func repoRel(repoRoot, path string) string {
	rel, err := filepath.Rel(repoRoot, path)
	if err != nil {
		return path
	}
	return rel
}
