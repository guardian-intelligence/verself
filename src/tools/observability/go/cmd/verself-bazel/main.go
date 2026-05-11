package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/verself/observability/bazeltelemetry"
)

const (
	defaultMinProfileDurationMs = 50
	uploadAuto                  = "auto"
	uploadRequired              = "required"
	uploadOff                   = "off"
)

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "verself-bazel:", err)
		os.Exit(exitCode(err))
	}
}

type exitError struct {
	code int
	err  error
}

func (e exitError) Error() string {
	if e.err == nil {
		return fmt.Sprintf("exit %d", e.code)
	}
	return e.err.Error()
}

func (e exitError) Unwrap() error { return e.err }

func run(ctx context.Context, args []string) error {
	cfg, command, bazelArgs, err := parseArgs(args)
	if err != nil {
		return err
	}
	if err := rejectConflictingBazelFlags(bazelArgs); err != nil {
		return err
	}
	invocationID, err := newInvocationID()
	if err != nil {
		return err
	}
	workDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve working directory: %w", err)
	}
	tempDir, err := os.MkdirTemp("", "verself-bazel-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	paths := bazeltelemetry.ArtifactPaths{
		Profile:      filepath.Join(tempDir, "profile.json.gz"),
		BEP:          filepath.Join(tempDir, "bep.ndjson"),
		ExecutionLog: filepath.Join(tempDir, "execution-log.json"),
	}
	started := time.Now().UTC()
	bazelExitCode, runErr := runBazel(ctx, invocationID, command, bazelArgs, paths)
	completed := time.Now().UTC()
	invocation := bazeltelemetry.Invocation{
		InvocationID:     invocationID,
		Command:          command,
		Args:             append([]string(nil), bazelArgs...),
		TargetPatterns:   targetPatterns(bazelArgs),
		WorkingDirectory: workDir,
		ExitCode:         bazelExitCode,
		StartedAt:        started,
		CompletedAt:      completed,
		DurationMs:       completed.Sub(started).Milliseconds(),
		GitHubWorkflow:   os.Getenv("GITHUB_WORKFLOW"),
		GitHubJob:        os.Getenv("GITHUB_JOB"),
		GitHubRef:        os.Getenv("GITHUB_REF"),
		GitHubSHA:        os.Getenv("GITHUB_SHA"),
	}
	upload := bazeltelemetry.Collect(invocation, paths, cfg.minProfileDurationMs)
	if uploadErr := uploadTelemetry(ctx, cfg.uploadMode, upload); uploadErr != nil {
		if runErr != nil {
			return runErr
		}
		return uploadErr
	}
	if parseErr := parseFailure(upload); parseErr != nil && runErr == nil {
		return parseErr
	}
	return runErr
}

type config struct {
	minProfileDurationMs int64
	uploadMode           string
}

func parseArgs(args []string) (config, string, []string, error) {
	fs := flag.NewFlagSet("verself-bazel", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var cfg config
	fs.Int64Var(&cfg.minProfileDurationMs, "min-profile-duration-ms", defaultMinProfileDurationMs, "minimum duration for non-package profile spans")
	fs.StringVar(&cfg.uploadMode, "upload", uploadAuto, "telemetry upload mode: auto, required, off")
	if err := fs.Parse(args); err != nil {
		return config{}, "", nil, err
	}
	cfg.uploadMode = strings.TrimSpace(cfg.uploadMode)
	switch cfg.uploadMode {
	case uploadAuto, uploadRequired, uploadOff:
	default:
		return config{}, "", nil, fmt.Errorf("--upload must be auto, required, or off")
	}
	if cfg.minProfileDurationMs < 0 {
		return config{}, "", nil, fmt.Errorf("--min-profile-duration-ms must be non-negative")
	}
	rest := fs.Args()
	if len(rest) == 0 {
		return config{}, "", nil, fmt.Errorf("usage: verself-bazel [wrapper flags] <build|test> [bazel args...]")
	}
	command := strings.TrimSpace(rest[0])
	switch command {
	case "build", "test":
	default:
		return config{}, "", nil, fmt.Errorf("only bazel build and test are instrumented, got %q", command)
	}
	return cfg, command, append([]string(nil), rest[1:]...), nil
}

func rejectConflictingBazelFlags(args []string) error {
	for _, arg := range args {
		switch {
		case strings.HasPrefix(arg, "--profile="),
			strings.HasPrefix(arg, "--build_event_json_file="),
			strings.HasPrefix(arg, "--execution_log_json_file="),
			strings.HasPrefix(arg, "--invocation_id="):
			return fmt.Errorf("instrumentation flag %s is owned by verself-bazel", strings.SplitN(arg, "=", 2)[0])
		}
	}
	return nil
}

func runBazel(ctx context.Context, invocationID, command string, args []string, paths bazeltelemetry.ArtifactPaths) (int, error) {
	bazelArgs := []string{
		command,
		"--invocation_id=" + invocationID,
		"--build_event_json_file=" + paths.BEP,
		"--profile=" + paths.Profile,
		"--execution_log_json_file=" + paths.ExecutionLog,
		"--build_metadata=VERSELF_BAZEL_INVOCATION_ID=" + invocationID,
	}
	if executionID := strings.TrimSpace(os.Getenv("VERSELF_EXECUTION_ID")); executionID != "" {
		bazelArgs = append(bazelArgs, "--build_metadata=VERSELF_EXECUTION_ID="+executionID)
	}
	bazelArgs = append(bazelArgs, args...)

	cmd := exec.CommandContext(ctx, "bazelisk", bazelArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Env = bazelEnv(os.Environ())
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			code := exit.ExitCode()
			return code, exitError{code: code, err: err}
		}
		return 127, exitError{code: 127, err: fmt.Errorf("run bazelisk: %w", err)}
	}
	return 0, nil
}

func bazelEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, entry := range env {
		if strings.HasPrefix(entry, "VERSELF_BAZEL_TELEMETRY_TOKEN=") {
			continue
		}
		out = append(out, entry)
	}
	return out
}

func targetPatterns(args []string) []string {
	out := make([]string, 0, len(args))
	afterDashDash := false
	for _, arg := range args {
		if arg == "--" {
			afterDashDash = true
			continue
		}
		if afterDashDash || strings.HasPrefix(arg, "//") || strings.HasPrefix(arg, "@") || strings.HasPrefix(arg, ":") {
			out = append(out, arg)
		}
	}
	return out
}

func uploadTelemetry(ctx context.Context, mode string, upload bazeltelemetry.Upload) error {
	if mode == uploadOff {
		fmt.Fprintln(os.Stderr, "verself-bazel: telemetry upload disabled")
		return nil
	}
	endpoint, err := telemetryEndpoint()
	if err != nil {
		if mode == uploadAuto {
			fmt.Fprintf(os.Stderr, "verself-bazel: telemetry upload skipped: %v\n", err)
			return nil
		}
		return err
	}
	var body bytes.Buffer
	gz := gzip.NewWriter(&body)
	if err := json.NewEncoder(gz).Encode(upload); err != nil {
		_ = gz.Close()
		return fmt.Errorf("encode telemetry upload: %w", err)
	}
	if err := gz.Close(); err != nil {
		return fmt.Errorf("compress telemetry upload: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, &body)
	if err != nil {
		return fmt.Errorf("create telemetry upload request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "gzip")
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(os.Getenv("VERSELF_BAZEL_TELEMETRY_TOKEN")))
	req.Header.Set("X-Verself-Execution-Id", strings.TrimSpace(os.Getenv("VERSELF_EXECUTION_ID")))
	req.Header.Set("X-Verself-Attempt-Id", strings.TrimSpace(os.Getenv("VERSELF_ATTEMPT_ID")))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("upload telemetry: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("upload telemetry: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(detail)))
	}
	fmt.Fprintf(os.Stderr, "verself-bazel: uploaded telemetry invocation=%s packages=%d spawns=%d targets=%d\n",
		upload.Invocation.InvocationID,
		countPackageSpans(upload.ProfileSpans),
		len(upload.Spawns),
		len(upload.Targets),
	)
	return nil
}

func telemetryEndpoint() (string, error) {
	token := strings.TrimSpace(os.Getenv("VERSELF_BAZEL_TELEMETRY_TOKEN"))
	executionID := strings.TrimSpace(os.Getenv("VERSELF_EXECUTION_ID"))
	attemptID := strings.TrimSpace(os.Getenv("VERSELF_ATTEMPT_ID"))
	origin := strings.TrimRight(strings.TrimSpace(os.Getenv("VERSELF_HOST_SERVICE_HTTP_ORIGIN")), "/")
	path := strings.TrimSpace(os.Getenv("VERSELF_BAZEL_TELEMETRY_PATH"))
	if token == "" || executionID == "" || attemptID == "" || origin == "" || path == "" {
		return "", fmt.Errorf("missing VERSELF_BAZEL_TELEMETRY_TOKEN, VERSELF_EXECUTION_ID, VERSELF_ATTEMPT_ID, VERSELF_HOST_SERVICE_HTTP_ORIGIN, or VERSELF_BAZEL_TELEMETRY_PATH")
	}
	u, err := url.Parse(origin + path)
	if err != nil {
		return "", fmt.Errorf("parse telemetry endpoint: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("telemetry endpoint must be http or https")
	}
	return u.String(), nil
}

func parseFailure(upload bazeltelemetry.Upload) error {
	for _, event := range upload.Events {
		if strings.HasSuffix(event.EventName, ".parse") && event.Result != "succeeded" {
			return fmt.Errorf("%s %s: %s", event.EventName, event.Result, event.Reason)
		}
	}
	return nil
}

func countPackageSpans(spans []bazeltelemetry.ProfileSpan) int {
	count := 0
	for _, span := range spans {
		if span.SpanKind == "package" {
			count++
		}
	}
	return count
}

func newInvocationID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate invocation id: %w", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	hexed := hex.EncodeToString(b[:])
	return hexed[0:8] + "-" + hexed[8:12] + "-" + hexed[12:16] + "-" + hexed[16:20] + "-" + hexed[20:32], nil
}

func exitCode(err error) int {
	var exit exitError
	if errors.As(err, &exit) && exit.code != 0 {
		return exit.code
	}
	return 1
}
