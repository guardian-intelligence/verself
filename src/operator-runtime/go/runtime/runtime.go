// Package runtime contains the shared operator-side bootstrap used by
// dev tooling and deploy tooling. It owns repo/site resolution, operator
// identity, SSH transport, and optional OTel export over the operator SSH
// channel.
package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	verselfotel "github.com/verself/observability/otel"
)

const (
	DefaultSite = "prod"

	otlpRemotePort     = 4317
	otelShutdownBudget = 5 * time.Second
)

// Options describes the operator runtime a command needs. Empty Site
// defaults to prod and empty RepoRoot defaults to the process cwd.
type Options struct {
	ServiceName    string
	ServiceVersion string
	Command        string

	RepoRoot string
	Site     string
	Device   string

	InfraHost string
	InfraUser string

	NeedSSH  bool
	NeedOTel bool
}

// Runtime is the per-command resolved operator context. Callers should use
// Run unless they need custom lifetime handling.
type Runtime struct {
	Ctx       context.Context
	RepoRoot  string
	Site      string
	Command   string
	ConfigDir string
	Device    string
	Target    InventoryTarget
	SSH       *SSHClient
	Tracer    trace.Tracer

	otlpForward  *Forward
	otelShutdown func(context.Context) error
	commandSpan  trace.Span
}

// Run initializes a Runtime, executes fn, records fn's result on the command
// span, and tears down resources in reverse order.
func Run(ctx context.Context, opts Options, fn func(*Runtime) error) error {
	rt, err := Init(ctx, opts)
	if err != nil {
		return err
	}
	runErr := fn(rt)
	closeErr := rt.Finish(runErr)
	return errors.Join(runErr, closeErr)
}

// Init resolves operator state and opens requested shared transports.
func Init(ctx context.Context, opts Options) (*Runtime, error) {
	if opts.ServiceName == "" {
		return nil, errors.New("operator runtime: ServiceName is required")
	}
	site := opts.Site
	if site == "" {
		site = DefaultSite
	}
	repoRoot := opts.RepoRoot
	if repoRoot == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("operator runtime: cwd: %w", err)
		}
		repoRoot = cwd
	}
	cfgDir, err := ConfigDir()
	if err != nil {
		return nil, err
	}
	rt := &Runtime{
		Ctx:       ctx,
		RepoRoot:  repoRoot,
		Site:      site,
		Command:   opts.Command,
		ConfigDir: cfgDir,
	}

	if opts.NeedSSH || opts.NeedOTel {
		device := opts.Device
		if device == "" {
			device, err = InferDeviceName(cfgDir)
			if errors.Is(err, ErrNoOnboardedDevice) {
				return nil, errors.New("no operator device is onboarded; run `aspect operator onboard --device=<name>`")
			}
			if err != nil {
				return nil, err
			}
		}
		if !ValidDeviceName(device) {
			return nil, fmt.Errorf("invalid operator device %q: must match ^[a-z][a-z0-9-]*$", device)
		}
		target := InventoryTarget{}
		if opts.InfraHost != "" || opts.InfraUser != "" {
			target.Host = opts.InfraHost
			target.User = opts.InfraUser
		}
		if target.Host == "" || target.User == "" {
			target, err = LoadInfraTarget(InventoryPath(repoRoot, site))
			if err != nil {
				return nil, err
			}
			if opts.InfraHost != "" {
				target.Host = opts.InfraHost
			}
			if opts.InfraUser != "" {
				target.User = opts.InfraUser
			}
		}
		if err := validateSSHHost(target.Host); err != nil {
			return nil, fmt.Errorf("operator runtime: invalid SSH host %q: %w", target.Host, err)
		}
		if target.User == "" {
			return nil, errors.New("operator runtime: SSH user is required")
		}
		ssh, err := DialSSH(ctx, SSHOptions{
			ConfigDir: cfgDir,
			Device:    device,
			User:      target.User,
			Host:      target.Host,
		})
		if err != nil {
			return nil, err
		}
		rt.Device = device
		rt.Target = target
		rt.SSH = ssh
	}

	if opts.NeedOTel {
		if rt.SSH == nil {
			return nil, errors.New("operator runtime: NeedOTel requires SSH")
		}
		forward, err := rt.SSH.Forward(ctx, "otlp", fmt.Sprintf("127.0.0.1:%d", otlpRemotePort))
		if err != nil {
			_ = rt.SSH.Close()
			return nil, fmt.Errorf("operator runtime: open OTLP forward: %w", err)
		}
		rt.otlpForward = forward
		_ = os.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://"+forward.ListenAddr)
		shutdown, _, err := verselfotel.Init(ctx, verselfotel.Config{
			ServiceName:    opts.ServiceName,
			ServiceVersion: opts.ServiceVersion,
		})
		if err != nil {
			_ = rt.Close()
			return nil, fmt.Errorf("operator runtime: otel init: %w", err)
		}
		rt.otelShutdown = shutdown
	}
	rt.Tracer = otel.Tracer(opts.ServiceName)
	if opts.Command != "" {
		ctx, span := rt.Tracer.Start(rt.Ctx, "verself_operator.command",
			trace.WithSpanKind(trace.SpanKindInternal),
			trace.WithAttributes(
				attribute.String("verself.site", rt.Site),
				attribute.String("verself.command", opts.Command),
				attribute.String("verself.operator_device", rt.Device),
				attribute.String("ssh.host", rt.Target.Host),
				attribute.String("ssh.user", rt.Target.User),
			),
		)
		rt.Ctx = ctx
		rt.commandSpan = span
	}
	return rt, nil
}

// Finish records err on the command span and closes all resources.
func (rt *Runtime) Finish(err error) error {
	if rt == nil {
		return nil
	}
	if rt.commandSpan != nil {
		if err != nil {
			rt.commandSpan.RecordError(err)
			rt.commandSpan.SetStatus(codes.Error, err.Error())
		} else {
			rt.commandSpan.SetStatus(codes.Ok, "")
		}
		rt.commandSpan.End()
		rt.commandSpan = nil
	}
	return rt.Close()
}

// Close releases runtime resources. Prefer Finish when a command result is
// available so the command span records success or failure.
func (rt *Runtime) Close() error {
	if rt == nil {
		return nil
	}
	var closeErr error
	if rt.otelShutdown != nil {
		flushCtx, cancel := context.WithTimeout(context.Background(), otelShutdownBudget)
		closeErr = errors.Join(closeErr, rt.otelShutdown(flushCtx))
		cancel()
		rt.otelShutdown = nil
	}
	if rt.otlpForward != nil {
		closeErr = errors.Join(closeErr, rt.otlpForward.Close())
		rt.otlpForward = nil
	}
	if rt.SSH != nil {
		closeErr = errors.Join(closeErr, rt.SSH.Close())
		rt.SSH = nil
	}
	return closeErr
}

func (rt *Runtime) TraceID() string {
	if rt == nil {
		return ""
	}
	sc := trace.SpanContextFromContext(rt.Ctx)
	if !sc.IsValid() {
		return ""
	}
	return sc.TraceID().String()
}

func InventoryPath(repoRoot, site string) string {
	return filepath.Join(repoRoot, "src", "host-configuration", "ansible", "inventory", site+".ini")
}

func SecretsPath(repoRoot string) string {
	return filepath.Join(repoRoot, "src", "host-configuration", "ansible", "group_vars", "all", "secrets.sops.yml")
}

func ConfigDir() (string, error) {
	if v := os.Getenv("VERSELF_CONFIG_HOME"); v != "" {
		return v, nil
	}
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return filepath.Join(v, "verself"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "verself"), nil
}

var ErrNoOnboardedDevice = errors.New("no onboarded devices on this machine")

func InferDeviceName(cfg string) (string, error) {
	entries, err := os.ReadDir(filepath.Join(cfg, "ssh"))
	if err != nil {
		if os.IsNotExist(err) {
			return "", ErrNoOnboardedDevice
		}
		return "", err
	}
	var devices []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, ".pub") || strings.HasSuffix(name, "-cert.pub") {
			continue
		}
		devices = append(devices, name)
	}
	if len(devices) == 0 {
		return "", ErrNoOnboardedDevice
	}
	if len(devices) > 1 {
		return "", fmt.Errorf("multiple onboarded devices on this machine (%s); pass --device=<name>", strings.Join(devices, ", "))
	}
	return devices[0], nil
}

func ValidDeviceName(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case i > 0 && r >= '0' && r <= '9':
		case i > 0 && r == '-':
		default:
			return false
		}
	}
	return true
}
