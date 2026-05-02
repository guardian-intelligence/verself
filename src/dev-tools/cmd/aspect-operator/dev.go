package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	opch "github.com/verself/operator-runtime/clickhouse"
	opruntime "github.com/verself/operator-runtime/runtime"
)

type devOptions struct {
	operatorRuntimeOptions
}

type nomadJobFile struct {
	Job struct {
		TaskGroups []struct {
			Tasks []struct {
				Name string            `json:"Name"`
				Env  map[string]string `json:"Env"`
			} `json:"Tasks"`
		} `json:"TaskGroups"`
	} `json:"Job"`
}

type tunnelSpec struct {
	Name      string
	EnvKey    string
	Remote    string
	Choices   []int
	LocalPort int
}

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
		cmd.Dir = filepath.Join(rt.RepoRoot, "src", "viteplus-monorepo")
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
	tunnels := []tunnelSpec{
		{Name: "sandbox-rental-service", EnvKey: "SANDBOX_RENTAL_SERVICE_BASE_URL", Remote: "127.0.0.1:4243", Choices: []int{14243, 24243, 34243, 44243, 54243}},
		{Name: "identity-service", EnvKey: "IDENTITY_SERVICE_BASE_URL", Remote: "127.0.0.1:4248", Choices: []int{14248, 24248, 34248, 44248, 54248}},
		{Name: "profile-service", EnvKey: "PROFILE_SERVICE_BASE_URL", Remote: "127.0.0.1:4258", Choices: []int{14258, 24258, 34258, 44258, 54258}},
		{Name: "governance-service", EnvKey: "GOVERNANCE_SERVICE_BASE_URL", Remote: "127.0.0.1:4250", Choices: []int{14250, 24250, 34250, 44250, 54250}},
		{Name: "notifications-service", EnvKey: "NOTIFICATIONS_SERVICE_BASE_URL", Remote: "127.0.0.1:4260", Choices: []int{14260, 24260, 34260, 44260, 54260}},
		{Name: "projects-service", EnvKey: "PROJECTS_SERVICE_BASE_URL", Remote: "127.0.0.1:4264", Choices: []int{14264, 24264, 34264, 44264, 54264}},
		{Name: "source-code-hosting-service", EnvKey: "SOURCE_CODE_HOSTING_SERVICE_BASE_URL", Remote: "127.0.0.1:4261", Choices: []int{14261, 24261, 34261, 44261, 54261}},
		{Name: "Electric", EnvKey: "ELECTRIC_BASE_URL", Remote: "127.0.0.1:3010", Choices: []int{13010, 23010, 33010, 43010, 53010}},
		{Name: "Electric notifications", EnvKey: "ELECTRIC_NOTIFICATIONS_BASE_URL", Remote: "127.0.0.1:3012", Choices: []int{13012, 23012, 33012, 43012, 53012}},
		{Name: "OTLP HTTP", EnvKey: "OTEL_EXPORTER_OTLP_ENDPOINT", Remote: "127.0.0.1:4318", Choices: []int{14318, 24318, 34318, 44318, 54318}},
	}
	for i := range tunnels {
		port, err := chooseLocalPort(devPortEnvName(tunnels[i].EnvKey), tunnels[i].Choices)
		if err != nil {
			return nil, nil, err
		}
		tunnels[i].LocalPort = port
		if !printOnly {
			forward, err := rt.SSH.ForwardLocal(rt.Ctx, tunnels[i].Name, net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), tunnels[i].Remote)
			if err != nil {
				return nil, nil, err
			}
			if err := waitForLocalTCP(forward.ListenAddr); err != nil {
				return nil, nil, err
			}
		}
	}
	appPort, err := chooseLocalPort("CONSOLE_DEV_LOCAL_APP_PORT", []int{4244, 5244, 6244, 7244, 8244})
	if err != nil {
		return nil, nil, err
	}
	domain := envOr("VERSELF_DOMAIN", jobEnv["VERSELF_DOMAIN"])
	if domain == "" {
		domain, err = loadVerselfDomain(rt.RepoRoot)
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
		"IDENTITY_SERVICE_AUTH_AUDIENCE",
		"PROFILE_SERVICE_AUTH_AUDIENCE",
		"NOTIFICATIONS_SERVICE_AUTH_AUDIENCE",
		"PROJECTS_SERVICE_AUTH_AUDIENCE",
		"SOURCE_CODE_HOSTING_SERVICE_AUTH_AUDIENCE",
	} {
		env[key] = firstNonEmpty(os.Getenv(key), jobEnv[key])
	}
	for _, tunnel := range tunnels {
		scheme := "http"
		env[tunnel.EnvKey] = fmt.Sprintf("%s://127.0.0.1:%d", scheme, tunnel.LocalPort)
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
		"identity":               env["IDENTITY_SERVICE_BASE_URL"],
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
	path := filepath.Join(repoRoot, "src", "deployment-tools", "nomad", "sites", site, "jobs", "verself-web.nomad.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var job nomadJobFile
	if err := json.Unmarshal(raw, &job); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	for _, group := range job.Job.TaskGroups {
		for _, task := range group.Tasks {
			if task.Name == "verself-web" {
				return task.Env, nil
			}
		}
	}
	return nil, fmt.Errorf("%s did not contain a verself-web task", path)
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
	case "IDENTITY_SERVICE_BASE_URL":
		return "CONSOLE_DEV_LOCAL_IDENTITY_PORT"
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
