package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	opch "github.com/verself/operator-runtime/clickhouse"
	opruntime "github.com/verself/operator-runtime/runtime"
)

// Nomad's HTTP API on the worker. We dial through the operator SSH session
// rather than opening a forwarded local listener — the lookup is one-shot at
// dev startup and doesn't need a stable port.
const nomadHTTPRemoteAddr = "127.0.0.1:4646"

type devOptions struct {
	operatorRuntimeOptions
}

type tunnelSpec struct {
	Name string
	// EnvKey is the env var that receives the resulting http://127.0.0.1:<localPort>.
	EnvKey string
	// NomadService is the Nomad service name to resolve (e.g. iam-service-public-http).
	// When empty, RemotePort is used as a static target — for components that don't
	// register with Nomad (Electric, the OTLP collector).
	NomadService string
	RemotePort   int
	Choices      []int
	LocalPort    int
}

// placeholderUnreachableURL is what we set an env var to when the backing
// Nomad service isn't registered (e.g. not yet deployed in this environment).
// .invalid is reserved by RFC 6761 and never resolves, so any code that
// actually uses the URL fails loudly at request time instead of silently
// hitting a stale port.
const placeholderUnreachableURL = "http://service-not-registered.invalid"

func cmdDev(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("dev: missing subcommand (try `verself-web`)")
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "verself-web":
		return cmdDevVerselfWeb(rest)
	default:
		return fmt.Errorf("dev: unknown subcommand: %s", sub)
	}
}

func cmdDevVerselfWeb(args []string) error {
	fs := flagSet("dev verself-web")
	opts := &devOptions{}
	addOperatorRuntimeFlags(&opts.operatorRuntimeOptions)
	fs.StringVar(&opts.site, "site", opts.site, "Deploy site")
	fs.StringVar(&opts.repoRoot, "repo-root", "", "verself-sh checkout root (defaults to cwd)")
	printEnv := fs.Bool("print-env", false, "Print resolved env and exit before starting HMR")
	stateFile := fs.String("state-file", envOr("VERSELF_WEB_DEV_STATE_FILE", "/tmp/verself-web-dev.env"), "State env file path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return runOperatorRuntime("dev.verself_web", opts.operatorRuntimeOptions, !*printEnv, opch.Config{Database: "verself"}, func(rt *opruntime.Runtime, _ *opch.Client) error {
		env, summary, err := resolveVerselfWebDevEnv(rt, *printEnv)
		if err != nil {
			return err
		}
		rendered := renderEnv(env)
		if *printEnv {
			fmt.Print(rendered)
			fmt.Println("vp run @verself/verself-web#dev")
			return nil
		}
		if err := writeStateFile(*stateFile, rendered); err != nil {
			return err
		}
		summary["state"] = *stateFile
		printDevSummary(summary)
		cmd := exec.CommandContext(rt.Ctx, "vp", "run", "@verself/verself-web#dev")
		cmd.Dir = filepath.Join(rt.RepoRoot, "src", "websites")
		cmd.Env = envMapToList(env)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				return exitError{code: exitErr.ExitCode()}
			}
			return fmt.Errorf("vp run @verself/verself-web#dev: %w", err)
		}
		return nil
	})
}

func resolveVerselfWebDevEnv(rt *opruntime.Runtime, printOnly bool) (map[string]string, map[string]string, error) {
	jobEnv, err := verselfWebJobEnv(rt.RepoRoot, rt.Site)
	if err != nil {
		return nil, nil, err
	}
	// Product services register Nomad-allocated ports under stable service
	// names (see each service's nomad.hcl). verself-web's SSR fetches use the
	// Zitadel-authed public-http variant, not the SPIFFE-mTLS internal one.
	// Electric and the OTLP collector aren't Nomad-managed; they keep static
	// remote ports.
	tunnels := []tunnelSpec{
		{Name: "sandbox-rental-service", EnvKey: "SANDBOX_RENTAL_SERVICE_BASE_URL", NomadService: "sandbox-rental-public-http", Choices: []int{14243, 24243, 34243, 44243, 54243}},
		{Name: "iam-service", EnvKey: "IAM_SERVICE_BASE_URL", NomadService: "iam-service-public-http", Choices: []int{14248, 24248, 34248, 44248, 54248}},
		{Name: "profile-service", EnvKey: "PROFILE_SERVICE_BASE_URL", NomadService: "profile-service-public-http", Choices: []int{14258, 24258, 34258, 44258, 54258}},
		{Name: "governance-service", EnvKey: "GOVERNANCE_SERVICE_BASE_URL", NomadService: "governance-service-public-http", Choices: []int{14250, 24250, 34250, 44250, 54250}},
		{Name: "notifications-service", EnvKey: "NOTIFICATIONS_SERVICE_BASE_URL", NomadService: "notifications-service-public-http", Choices: []int{14260, 24260, 34260, 44260, 54260}},
		{Name: "billing-service", EnvKey: "BILLING_SERVICE_BASE_URL", NomadService: "billing-public-http", Choices: []int{14262, 24262, 34262, 44262, 54262}},
		{Name: "projects-service", EnvKey: "PROJECTS_SERVICE_BASE_URL", NomadService: "projects-service-public-http", Choices: []int{14264, 24264, 34264, 44264, 54264}},
		{Name: "source-code-hosting-service", EnvKey: "SOURCE_CODE_HOSTING_SERVICE_BASE_URL", NomadService: "source-code-hosting-service-public-http", Choices: []int{14261, 24261, 34261, 44261, 54261}},
		{Name: "Electric", EnvKey: "ELECTRIC_BASE_URL", RemotePort: 3010, Choices: []int{13010, 23010, 33010, 43010, 53010}},
		{Name: "Electric notifications", EnvKey: "ELECTRIC_NOTIFICATIONS_BASE_URL", RemotePort: 3012, Choices: []int{13012, 23012, 33012, 43012, 53012}},
		{Name: "OTLP HTTP", EnvKey: "OTEL_EXPORTER_OTLP_ENDPOINT", RemotePort: 4318, Choices: []int{14318, 24318, 34318, 44318, 54318}},
	}
	missing := map[string]bool{}
	for i := range tunnels {
		spec := &tunnels[i]
		port, err := chooseLocalPort(devPortEnvName(spec.EnvKey), spec.Choices)
		if err != nil {
			return nil, nil, err
		}
		spec.LocalPort = port
		if printOnly {
			continue
		}
		var remote string
		if spec.NomadService != "" {
			addr, lookupErr := lookupNomadService(rt.Ctx, rt.SSH, spec.NomadService)
			if lookupErr != nil {
				if errors.Is(lookupErr, errNomadServiceNotRegistered) {
					fmt.Fprintf(os.Stderr, "warning: Nomad service %q not registered; %s tunnel skipped (env set to placeholder)\n", spec.NomadService, spec.Name)
					missing[spec.EnvKey] = true
					continue
				}
				return nil, nil, fmt.Errorf("resolve %s: %w", spec.NomadService, lookupErr)
			}
			remote = addr
		} else {
			remote = net.JoinHostPort("127.0.0.1", strconv.Itoa(spec.RemotePort))
		}
		forward, err := rt.SSH.ForwardLocal(rt.Ctx, spec.Name, net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), remote)
		if err != nil {
			return nil, nil, err
		}
		if err := waitForLocalTCP(forward.ListenAddr); err != nil {
			return nil, nil, err
		}
	}
	appPort, err := chooseLocalPort("CONSOLE_DEV_LOCAL_APP_PORT", []int{4244, 5244, 6244, 7244, 8244})
	if err != nil {
		return nil, nil, err
	}
	domain := envOr("VERSELF_DOMAIN", jobEnv["VERSELF_DOMAIN"])
	if domain == "" {
		domain, err = loadVerselfDomain(rt.RepoRoot, rt.Site)
		if err != nil {
			return nil, nil, err
		}
	}
	electricSecret, err := readRemoteSecretString(rt, jobEnv["VERSELF_CRED_ELECTRIC_API_SECRET"])
	if err != nil {
		return nil, nil, err
	}
	electricNotificationsSecret, err := readRemoteSecretString(rt, jobEnv["VERSELF_CRED_ELECTRIC_NOTIFICATIONS_API_SECRET"])
	if err != nil {
		return nil, nil, err
	}
	env := map[string]string{}
	for _, kv := range os.Environ() {
		key, value, _ := strings.Cut(kv, "=")
		env[key] = value
	}
	env["VERSELF_DOMAIN"] = domain
	env["PRODUCT_BASE_URL"] = firstNonEmpty(os.Getenv("PRODUCT_BASE_URL"), "https://"+domain)
	for _, key := range []string{
		"SANDBOX_RENTAL_SERVICE_AUTH_AUDIENCE",
		"IAM_SERVICE_AUTH_AUDIENCE",
		"PROFILE_SERVICE_AUTH_AUDIENCE",
		"NOTIFICATIONS_SERVICE_AUTH_AUDIENCE",
		"PROJECTS_SERVICE_AUTH_AUDIENCE",
		"SOURCE_CODE_HOSTING_SERVICE_AUTH_AUDIENCE",
	} {
		env[key] = firstNonEmpty(os.Getenv(key), jobEnv[key])
	}
	for _, tunnel := range tunnels {
		if missing[tunnel.EnvKey] {
			env[tunnel.EnvKey] = placeholderUnreachableURL
			continue
		}
		env[tunnel.EnvKey] = fmt.Sprintf("http://127.0.0.1:%d", tunnel.LocalPort)
	}
	env["ELECTRIC_API_SECRET"] = firstNonEmpty(os.Getenv("ELECTRIC_API_SECRET"), electricSecret)
	env["ELECTRIC_NOTIFICATIONS_API_SECRET"] = firstNonEmpty(os.Getenv("ELECTRIC_NOTIFICATIONS_API_SECRET"), electricNotificationsSecret)
	env["OTEL_SERVICE_NAME"] = firstNonEmpty(os.Getenv("OTEL_SERVICE_NAME"), "verself-web")
	env["VERSELF_WEB_DEV_LOCAL_APP_PORT"] = strconv.Itoa(appPort)
	env["CONSOLE_DEV_LOCAL_APP_PORT"] = strconv.Itoa(appPort)
	env["BASE_URL"] = firstNonEmpty(os.Getenv("BASE_URL"), fmt.Sprintf("http://127.0.0.1:%d", appPort))
	env["TEST_BASE_URL"] = env["BASE_URL"]
	summary := map[string]string{
		"app":                    env["BASE_URL"],
		"identity":               env["IAM_SERVICE_BASE_URL"],
		"sandbox":                env["SANDBOX_RENTAL_SERVICE_BASE_URL"],
		"profile":                env["PROFILE_SERVICE_BASE_URL"],
		"governance":             env["GOVERNANCE_SERVICE_BASE_URL"],
		"notifications":          env["NOTIFICATIONS_SERVICE_BASE_URL"],
		"projects":               env["PROJECTS_SERVICE_BASE_URL"],
		"source":                 env["SOURCE_CODE_HOSTING_SERVICE_BASE_URL"],
		"electric":               env["ELECTRIC_BASE_URL"],
		"electric notifications": env["ELECTRIC_NOTIFICATIONS_BASE_URL"],
		"otlp":                   env["OTEL_EXPORTER_OTLP_ENDPOINT"],
	}
	return env, summary, nil
}

func verselfWebJobEnv(repoRoot, site string) (map[string]string, error) {
	path := filepath.Join(repoRoot, "src", "websites", "apps", "verself-web", "nomad.hcl")
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	env, err := parseNomadTaskEnv(raw, "verself-web")
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return env, nil
}

func parseNomadTaskEnv(raw []byte, taskName string) (map[string]string, error) {
	scanner := bufio.NewScanner(strings.NewReader(string(raw)))
	taskHeader := "task " + strconv.Quote(taskName) + " {"
	inTask := false
	inEnv := false
	env := map[string]string{}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case !inTask:
			if line == taskHeader {
				inTask = true
			}
		case !inEnv:
			if line == "env {" {
				inEnv = true
			}
		default:
			if line == "}" {
				if len(env) == 0 {
					return nil, fmt.Errorf("%s env block is empty", taskName)
				}
				return env, nil
			}
			key, value, ok := strings.Cut(line, "=")
			if !ok {
				return nil, fmt.Errorf("invalid env assignment %q", line)
			}
			parsed, err := strconv.Unquote(strings.TrimSpace(value))
			if err != nil {
				return nil, err
			}
			env[strings.TrimSpace(key)] = strings.ReplaceAll(parsed, "$${", "${")
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("%s task env not found", taskName)
}

func chooseLocalPort(envName string, choices []int) (int, error) {
	if raw := strings.TrimSpace(os.Getenv(envName)); raw != "" {
		port, err := strconv.Atoi(raw)
		if err != nil || port <= 0 || port > 65535 {
			return 0, fmt.Errorf("%s must be a TCP port", envName)
		}
		if err := ensureLocalPortFree(port); err != nil {
			return 0, err
		}
		return port, nil
	}
	for _, port := range choices {
		if ensureLocalPortFree(port) == nil {
			return port, nil
		}
	}
	return 0, fmt.Errorf("no free local port available from candidate set: %v", choices)
}

func ensureLocalPortFree(port int) error {
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return fmt.Errorf("local port %d is already in use", port)
	}
	return ln.Close()
}

// errNomadServiceNotRegistered means Nomad's API responded successfully but
// returned no entries — the service either isn't deployed or its allocations
// are unhealthy.
var errNomadServiceNotRegistered = errors.New("nomad service not registered")

type nomadServiceEntry struct {
	Address string `json:"Address"`
	Port    int    `json:"Port"`
}

// lookupNomadService resolves a Nomad service name to a host:port using the
// Nomad HTTP API at 127.0.0.1:4646, dialed through the operator SSH session.
// Returns the first registered allocation; multi-allocation services are
// fronted by HAProxy in production, so the dev shortcut picking one is fine.
func lookupNomadService(ctx context.Context, ssh *opruntime.SSHClient, service string) (string, error) {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return ssh.DialContext(ctx, "tcp", nomadHTTPRemoteAddr)
		},
		DisableKeepAlives: true,
	}
	client := &http.Client{Transport: transport, Timeout: 10 * time.Second}

	url := "http://" + nomadHTTPRemoteAddr + "/v1/service/" + service
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("build nomad request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("query nomad: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("nomad %s returned %d", service, resp.StatusCode)
	}
	var entries []nomadServiceEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return "", fmt.Errorf("decode nomad response: %w", err)
	}
	if len(entries) == 0 {
		return "", errNomadServiceNotRegistered
	}
	entry := entries[0]
	if entry.Address == "" || entry.Port == 0 {
		return "", fmt.Errorf("nomad %s entry missing address/port", service)
	}
	return net.JoinHostPort(entry.Address, strconv.Itoa(entry.Port)), nil
}

func waitForLocalTCP(addr string) error {
	deadline := time.Now().Add(5 * time.Second)
	for {
		conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("local tunnel did not open in time on %s", addr)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func devPortEnvName(baseURLKey string) string {
	switch baseURLKey {
	case "SANDBOX_RENTAL_SERVICE_BASE_URL":
		return "CONSOLE_DEV_LOCAL_SANDBOX_PORT"
	case "IAM_SERVICE_BASE_URL":
		return "CONSOLE_DEV_LOCAL_IAM_PORT"
	case "PROFILE_SERVICE_BASE_URL":
		return "CONSOLE_DEV_LOCAL_PROFILE_PORT"
	case "GOVERNANCE_SERVICE_BASE_URL":
		return "CONSOLE_DEV_LOCAL_GOVERNANCE_PORT"
	case "NOTIFICATIONS_SERVICE_BASE_URL":
		return "CONSOLE_DEV_LOCAL_NOTIFICATIONS_PORT"
	case "PROJECTS_SERVICE_BASE_URL":
		return "CONSOLE_DEV_LOCAL_PROJECTS_PORT"
	case "SOURCE_CODE_HOSTING_SERVICE_BASE_URL":
		return "CONSOLE_DEV_LOCAL_SOURCE_PORT"
	case "ELECTRIC_BASE_URL":
		return "CONSOLE_DEV_LOCAL_ELECTRIC_PORT"
	case "ELECTRIC_NOTIFICATIONS_BASE_URL":
		return "CONSOLE_DEV_LOCAL_ELECTRIC_NOTIFICATIONS_PORT"
	case "OTEL_EXPORTER_OTLP_ENDPOINT":
		return "CONSOLE_DEV_LOCAL_OTEL_HTTP_PORT"
	default:
		return baseURLKey + "_PORT"
	}
}

func writeStateFile(path, rendered string) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	finalized := false
	defer func() {
		if !finalized {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.WriteString(rendered); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	finalized = true
	return nil
}

func printDevSummary(values map[string]string) {
	fmt.Fprintln(os.Stderr, "verself-web local dev")
	for _, key := range []string{"app", "identity", "sandbox", "profile", "governance", "notifications", "projects", "source", "electric", "electric notifications", "otlp", "state"} {
		if values[key] != "" {
			fmt.Fprintf(os.Stderr, "  %-22s %s\n", key+":", values[key])
		}
	}
}

func envMapToList(env map[string]string) []string {
	out := make([]string, 0, len(env))
	for key, value := range env {
		out = append(out, key+"="+value)
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
