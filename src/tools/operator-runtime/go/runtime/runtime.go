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

	InfraHost string
	InfraUser string

	NeedSSH  bool
	NeedOTel bool
}

// Runtime is the per-command resolved operator context. Callers should use
// Run unless they need custom lifetime handling.
type Runtime struct {
	Ctx           context.Context
	RepoRoot      string
	Site          string
	Command       string
	SSHAuthMethod string
	Target        InventoryTarget
	SSH           *SSHClient
	Tracer        trace.Tracer

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
	rt := &Runtime{
		Ctx:      ctx,
		RepoRoot: repoRoot,
		Site:     site,
		Command:  opts.Command,
	}

	if opts.NeedSSH || opts.NeedOTel {
		target := InventoryTarget{}
		if opts.InfraHost != "" || opts.InfraUser != "" {
			target.Host = opts.InfraHost
			target.User = opts.InfraUser
		}
		if target.Host == "" || target.User == "" {
			loaded, err := LoadInfraTarget(InventoryPath(repoRoot, site))
			if err != nil {
				return nil, err
			}
			target = loaded
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
			User: target.User,
			Host: target.Host,
		})
		if err != nil {
			return nil, err
		}
		rt.SSHAuthMethod = ssh.authMethod
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
				attribute.String("ssh.auth_method", rt.SSHAuthMethod),
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
	return filepath.Join(repoRoot, "src", "host-configuration", "sites", site, "inventory.ini")
}

func SecretsPath(repoRoot string) string {
	return HostConfigurationSecretsPath(repoRoot, DefaultSite)
}

func HostConfigurationSecretsPath(repoRoot, site string) string {
	return filepath.Join(repoRoot, "src", "host-configuration", "sites", site, "secrets.sops.yml")
}

func DeploymentSecretsPath(repoRoot, site string) string {
	return filepath.Join(repoRoot, "src", "tools", "deployment", "sites", site, "secrets.sops.yml")
}
