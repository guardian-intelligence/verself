package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	verselfotel "github.com/verself/observability/otel"
)

type stringList []string

const maxUint32AsInt64 = int64(1<<32 - 1)

func (s *stringList) String() string {
	return strings.Join(*s, ",")
}

func (s *stringList) Set(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return errors.New("empty value")
	}
	*s = append(*s, value)
	return nil
}

type config struct {
	source               string
	dest                 string
	group                string
	haproxyUser          string
	haproxyBin           string
	haproxyConfigs       stringList
	haproxyLDLibraryPath string
	reloadUnit           string
	restartPIDFile       string
	daemon               bool
}

type fileSwap struct {
	path    string
	content []byte
	old     []byte
	existed bool
	group   string
	mode    fs.FileMode
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "haproxy-upstreams-apply: "+err.Error())
		os.Exit(1)
	}
}

func run(args []string) error {
	cfg := config{}
	fs := flag.NewFlagSet("haproxy-upstreams-apply", flag.ContinueOnError)
	fs.StringVar(&cfg.source, "source", "", "Rendered Nomad upstream config path.")
	fs.StringVar(&cfg.dest, "dest", "/etc/haproxy/nomad-upstreams.cfg", "Installed HAProxy upstream config path.")
	fs.StringVar(&cfg.group, "group", "haproxy", "Group owner for installed config.")
	fs.StringVar(&cfg.haproxyUser, "haproxy-user", "haproxy", "User used for HAProxy config validation.")
	fs.StringVar(&cfg.haproxyBin, "haproxy-bin", "haproxy", "Path to the HAProxy binary.")
	fs.Var(&cfg.haproxyConfigs, "haproxy-config", "HAProxy config to validate; repeat in HAProxy load order.")
	fs.StringVar(&cfg.haproxyLDLibraryPath, "haproxy-ld-library-path", "/opt/verself/profile/lib/haproxy", "LD_LIBRARY_PATH used when invoking HAProxy.")
	fs.StringVar(&cfg.reloadUnit, "reload-unit", "haproxy.service", "systemd unit to reload after a valid upstream swap.")
	fs.StringVar(&cfg.restartPIDFile, "restart-pid-file", "", "Process pid file to signal after a valid upstream swap.")
	fs.BoolVar(&cfg.daemon, "daemon", false, "Apply once, then stay alive until SIGINT or SIGTERM.")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if cfg.source == "" {
		return errors.New("--source is required")
	}
	if cfg.dest == "" {
		return errors.New("--dest is required")
	}
	if len(cfg.haproxyConfigs) == 0 {
		cfg.haproxyConfigs = append(cfg.haproxyConfigs, "/etc/haproxy/haproxy.cfg", cfg.dest)
	}
	shutdown, err := initTelemetry()
	if err != nil {
		// HAProxy upstream convergence is the availability path; telemetry
		// failure is reported but must not prevent a valid edge reload.
		fmt.Fprintf(os.Stderr, "haproxy-upstreams-apply: telemetry disabled: %v\n", err)
	} else {
		defer func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := shutdown(shutdownCtx); err != nil {
				fmt.Fprintf(os.Stderr, "haproxy-upstreams-apply: telemetry shutdown: %v\n", err)
			}
		}()
	}
	changed, err := applyOnceWithTelemetry(context.Background(), cfg)
	if err != nil {
		return err
	}
	fmt.Printf("haproxy-upstreams-apply: changed=%t\n", changed)
	if !cfg.daemon {
		return nil
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	return nil
}

func initTelemetry() (func(context.Context) error, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	shutdown, _, err := verselfotel.Init(ctx, verselfotel.Config{
		ServiceName: "haproxy-upstreams-apply",
	})
	if err != nil {
		return nil, err
	}
	return shutdown, nil
}

func applyOnceWithTelemetry(ctx context.Context, cfg config) (bool, error) {
	tracer := otel.Tracer("github.com/verself/infrastructure-components/haproxy/cmd/haproxy-upstreams-apply")
	_, span := tracer.Start(ctx, "haproxy_upstreams.apply",
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(
			attribute.String("haproxy.upstreams.source", cfg.source),
			attribute.String("haproxy.upstreams.dest", cfg.dest),
			attribute.String("haproxy.reload_unit", cfg.reloadUnit),
			attribute.String("haproxy.restart_pid_file", cfg.restartPIDFile),
			attribute.Bool("haproxy.upstreams.daemon", cfg.daemon),
		),
	)
	defer span.End()
	changed, err := applyOnce(cfg)
	span.SetAttributes(attribute.Bool("haproxy.upstreams.changed", changed))
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return false, err
	}
	span.SetStatus(codes.Ok, "")
	return changed, nil
}

func applyOnce(cfg config) (bool, error) {
	content, err := os.ReadFile(cfg.source)
	if err != nil {
		return false, fmt.Errorf("read source %s: %w", cfg.source, err)
	}
	if !bytes.Contains(content, []byte("\nbackend ")) {
		return false, fmt.Errorf("%s does not look like an HAProxy backend config", cfg.source)
	}
	swaps := []fileSwap{}
	if swap, changed, err := plannedSwap(cfg.dest, content, cfg.group, 0o640); err != nil {
		return false, err
	} else if changed {
		swaps = append(swaps, swap)
	}
	if len(swaps) == 0 {
		return false, nil
	}
	if err := applySwaps(swaps); err != nil {
		return false, err
	}
	if err := validateHAProxy(cfg); err != nil {
		if restoreErr := restoreSwaps(swaps); restoreErr != nil {
			return false, fmt.Errorf("haproxy validation failed after writing config set: %w; restore failed: %v", err, restoreErr)
		}
		return false, fmt.Errorf("haproxy validation failed after writing config set; previous files restored: %w", err)
	}
	if cfg.reloadUnit != "" {
		if err := reloadOrStartSystemdUnit(cfg.reloadUnit); err != nil {
			return false, err
		}
	}
	if cfg.restartPIDFile != "" {
		if err := signalPIDsFromFile(cfg.restartPIDFile, syscall.SIGTERM); err != nil {
			return false, err
		}
	}
	return true, nil
}

func plannedSwap(path string, content []byte, group string, mode fs.FileMode) (fileSwap, bool, error) {
	oldContent, oldErr := os.ReadFile(path)
	if oldErr != nil && !errors.Is(oldErr, os.ErrNotExist) {
		return fileSwap{}, false, fmt.Errorf("read existing %s: %w", path, oldErr)
	}
	if oldErr == nil && bytes.Equal(oldContent, content) {
		return fileSwap{}, false, nil
	}
	return fileSwap{
		path:    path,
		content: content,
		old:     oldContent,
		existed: oldErr == nil,
		group:   group,
		mode:    mode,
	}, true, nil
}

func applySwaps(swaps []fileSwap) error {
	for _, swap := range swaps {
		if err := atomicWrite(swap.path, swap.content, swap.group, swap.mode); err != nil {
			return err
		}
	}
	return nil
}

func restoreSwaps(swaps []fileSwap) error {
	var restoreErr error
	for i := len(swaps) - 1; i >= 0; i-- {
		swap := swaps[i]
		if swap.existed {
			if err := atomicWrite(swap.path, swap.old, swap.group, swap.mode); err != nil {
				restoreErr = errors.Join(restoreErr, err)
			}
			continue
		}
		if err := os.Remove(swap.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			restoreErr = errors.Join(restoreErr, fmt.Errorf("remove %s: %w", swap.path, err))
		}
	}
	return restoreErr
}

func atomicWrite(path string, content []byte, group string, mode fs.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp for %s: %w", path, err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write %s: %w", tmpName, err)
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod %s: %w", tmpName, err)
	}
	if group != "" {
		gid, err := groupID(group)
		if err != nil {
			_ = tmp.Close()
			return err
		}
		if err := tmp.Chown(0, gid); err != nil {
			_ = tmp.Close()
			return fmt.Errorf("chown %s root:%s: %w", tmpName, group, err)
		}
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename %s to %s: %w", tmpName, path, err)
	}
	return nil
}

func groupID(group string) (int, error) {
	g, err := user.LookupGroup(group)
	if err != nil {
		return 0, fmt.Errorf("lookup group %s: %w", group, err)
	}
	gid, err := strconv.Atoi(g.Gid)
	if err != nil {
		return 0, fmt.Errorf("parse gid for %s: %w", group, err)
	}
	return gid, nil
}

func validateHAProxy(cfg config) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	argv := []string{"-c"}
	for _, config := range cfg.haproxyConfigs {
		argv = append(argv, "-f", config)
	}
	haproxyBin, err := commandPath(cfg.haproxyBin)
	if err != nil {
		return err
	}
	ldLibraryPath, err := relativePathToAbs(cfg.haproxyLDLibraryPath)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, haproxyBin, argv...)
	// HAProxy validates after dropping privileges; do not inherit an operator-only cwd.
	cmd.Dir = "/"
	cmd.Env = withLDLibraryPath(os.Environ(), ldLibraryPath)
	if cfg.haproxyUser != "" {
		credential, err := userCredential(cfg.haproxyUser)
		if err != nil {
			return err
		}
		cmd.SysProcAttr = &syscall.SysProcAttr{Credential: credential}
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %w: %s", haproxyBin, strings.Join(argv, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func commandPath(path string) (string, error) {
	return relativePathToAbs(path)
}

func relativePathToAbs(path string) (string, error) {
	if path == "" || filepath.IsAbs(path) || !strings.ContainsRune(path, '/') {
		return path, nil
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve path %s: %w", path, err)
	}
	return absPath, nil
}

func userCredential(name string) (*syscall.Credential, error) {
	u, err := user.Lookup(name)
	if err != nil {
		return nil, fmt.Errorf("lookup user %s: %w", name, err)
	}
	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return nil, fmt.Errorf("parse uid for %s: %w", name, err)
	}
	gid, err := strconv.Atoi(u.Gid)
	if err != nil {
		return nil, fmt.Errorf("parse gid for %s: %w", name, err)
	}
	credentialUID, err := uint32FromInt(uid, "uid", name)
	if err != nil {
		return nil, err
	}
	credentialGID, err := uint32FromInt(gid, "gid", name)
	if err != nil {
		return nil, err
	}
	return &syscall.Credential{Uid: credentialUID, Gid: credentialGID}, nil
}

func uint32FromInt(value int, field string, userName string) (uint32, error) {
	if value < 0 || int64(value) > maxUint32AsInt64 {
		return 0, fmt.Errorf("%s for %s exceeds uint32 range: %d", field, userName, value)
	}
	return uint32(value), nil // #nosec G115 -- value is checked against uint32 range above.
}

func withLDLibraryPath(env []string, path string) []string {
	if path == "" {
		return env
	}
	for i, kv := range env {
		if strings.HasPrefix(kv, "LD_LIBRARY_PATH=") {
			env[i] = kv + ":" + path
			return env
		}
	}
	return append(env, "LD_LIBRARY_PATH="+path)
}

func signalPIDsFromFile(path string, signal syscall.Signal) error {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read pid file %s: %w", path, err)
	}
	fields := strings.Fields(string(raw))
	if len(fields) == 0 {
		return nil
	}
	for _, field := range fields {
		pid, err := strconv.Atoi(field)
		if err != nil || pid <= 0 {
			return fmt.Errorf("invalid pid %q in %s", field, path)
		}
		if err := syscall.Kill(pid, signal); err != nil && !errors.Is(err, syscall.ESRCH) {
			return fmt.Errorf("signal pid %d from %s: %w", pid, path, err)
		}
	}
	return nil
}

func reloadOrStartSystemdUnit(unit string) error {
	active, err := systemdUnitActive(unit)
	if err != nil {
		return err
	}
	action := "start"
	if active {
		action = "reload"
	}
	return systemctl(action, unit)
}

func systemdUnitActive(unit string) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "systemctl", "is-active", "--quiet", unit).CombinedOutput()
	if err == nil {
		return true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 3 {
		return false, nil
	}
	return false, fmt.Errorf("systemctl is-active --quiet %s: %w: %s", unit, err, strings.TrimSpace(string(out)))
}

func systemctl(action, unit string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "systemctl", action, unit).CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl %s %s: %w: %s", action, unit, err, strings.TrimSpace(string(out)))
	}
	return nil
}
