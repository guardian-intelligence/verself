// Package runtime is the per-subcommand bootstrap that every
// verself-deploy entry point routes through. It owns the controlled
// startup ordering — SSH dial, OTLP forward channel, OTel SDK init —
// and the reverse-order shutdown that flushes spans before tearing
// the channel down.
//
// Boundary: this package wires existing internal packages together;
// it does not re-implement them. SSH lives in sshtun and the OTel SDK
// lives in github.com/verself/observability/otel. Runtime startup must
// not depend on ClickHouse because ClickHouse schema and availability
// are deployable platform state.
package runtime

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	verselfotel "github.com/verself/observability/otel"

	"github.com/verself/deployment-tools/internal/identity"
	"github.com/verself/deployment-tools/internal/sshtun"
)

// otlpForwardRemotePort is the bare-metal otelcol's OTLP gRPC
// receiver. The substrate role binds it; the SSH local-port forward
// proxies to the same port on the remote loopback so the SDK's
// otlptracegrpc exporter dials a 127.0.0.1:<picked-port> address that
// tunnels straight to the receiver.
const otlpForwardRemotePort = 4317

// otelShutdownBudget bounds the SDK's BatchSpanProcessor flush at
// process exit. The export hop is loopback (an SSH local forward over
// an established control session), so a healthy flush completes in
// <1s; the slack here is for upstream backpressure on the bare-metal
// otelcol, not network latency.
const otelShutdownBudget = 30 * time.Second

// Options configure a Bootstrap call. ServiceName is the only
// required field; the rest carry sensible defaults.
type Options struct {
	ServiceName    string
	ServiceVersion string

	// Site selects per-site state (inventory file when InfraHost is
	// unset and baggage attributes). Defaults to "prod".
	Site string

	// RepoRoot is the absolute path to the verself-sh checkout. Used
	// to find the authored substrate inventory.
	// When empty, the process cwd is used.
	RepoRoot string

	// InfraHost overrides inventory resolution. Useful for one-shot
	// invocations that don't run against the authored repo inventory.
	InfraHost string
	InfraUser string

	// SkipOTLPForward suppresses the SSH-forwarded OTLP channel and
	// leaves OTEL_EXPORTER_OTLP_ENDPOINT untouched, so the SDK uses
	// whatever a parent verself-deploy process exported. Used by
	// child invocations spawned inside an existing run (e.g. a
	// reconciler exec'd by `verself-deploy run`) — they inherit the
	// parent's tunnel rather than opening a competing one.
	SkipOTLPForward bool
}

// Runtime is the resolved bootstrap surface. Callers consume Tracer and SSH
// for forwards/exec; the rest is internal bookkeeping.
type Runtime struct {
	Ctx      context.Context
	Tracer   trace.Tracer
	Identity identity.Snapshot
	SSH      *sshtun.Client
	SSHPort  int
	Site     string
	RepoRoot string

	otlpForward  *sshtun.Forward
	otelShutdown func(context.Context) error

	bootstrapSpan  trace.Span
	bootstrapStart time.Time
}

type sshAccessTarget struct {
	Label string
	Alias string
	Host  string
	User  string
	Ports []int
}

func (t sshAccessTarget) display() string {
	label := t.Label
	if label == "" {
		label = "ssh"
	}
	return fmt.Sprintf("%s %s@%s ports %v", label, t.User, t.Host, t.Ports)
}

// Init brings up SSH, the OTLP forward, and the OTel SDK in that order. The
// returned Runtime owns shutdown via Close — defer that on the caller and the
// reverse ordering happens automatically.
func Init(ctx context.Context, opts Options) (*Runtime, error) {
	if opts.ServiceName == "" {
		return nil, fmt.Errorf("runtime: ServiceName is required")
	}
	site := opts.Site
	if site == "" {
		site = "prod"
	}
	repoRoot := opts.RepoRoot
	if repoRoot == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("runtime: cwd: %w", err)
		}
		repoRoot = cwd
	}
	bootstrapStart := time.Now()

	var accessTargets []sshAccessTarget
	if opts.InfraHost != "" || opts.InfraUser != "" {
		if opts.InfraHost == "" || opts.InfraUser == "" {
			return nil, fmt.Errorf("runtime: InfraHost and InfraUser must be set together")
		}
		accessTargets = []sshAccessTarget{{
			Label: "override",
			Host:  opts.InfraHost,
			User:  opts.InfraUser,
			Ports: []int{22},
		}}
	} else {
		resolved, err := resolveInfraSSHAccess(repoRoot, site)
		if err != nil {
			return nil, err
		}
		accessTargets = resolved
	}

	rt := &Runtime{
		Site:           site,
		RepoRoot:       repoRoot,
		bootstrapStart: bootstrapStart,
	}

	sshClient, selected, err := dialSSHAccess(ctx, accessTargets)
	if err != nil {
		return nil, err
	}
	rt.SSH = sshClient
	rt.SSHPort = sshClient.Port()

	if !opts.SkipOTLPForward {
		forward, err := sshClient.Forward(ctx, "otlp", otlpForwardRemotePort)
		if err != nil {
			_ = sshClient.Close()
			return nil, fmt.Errorf("runtime: open OTLP forward: %w", err)
		}
		rt.otlpForward = forward

		// Bind the parent's SDK and any child processes (reconciler
		// scripts, ansible-playbook) to the SSH-forwarded loopback
		// address. The OTel SDK reads OTEL_EXPORTER_OTLP_ENDPOINT once
		// at Init, so this must be set before verselfotel.Init.
		_ = os.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://"+forward.ListenAddr)
	}

	otelShutdown, _, err := verselfotel.Init(ctx, verselfotel.Config{
		ServiceName:    opts.ServiceName,
		ServiceVersion: opts.ServiceVersion,
	})
	if err != nil {
		_ = sshClient.Close()
		return nil, fmt.Errorf("runtime: otel init: %w", err)
	}
	rt.otelShutdown = otelShutdown

	// Identity onto baggage; the verselfotel SpanProcessor copies any
	// `verself.` baggage member onto every started span.
	snap := identity.FromEnv()
	rt.Identity = snap
	bag := snap.Baggage()
	if bag.Len() > 0 {
		ctx = baggage.ContextWithBaggage(ctx, bag)
	}
	ctx = otel.GetTextMapPropagator().Extract(ctx, propagation.MapCarrier{
		"traceparent": os.Getenv("TRACEPARENT"),
		"tracestate":  os.Getenv("TRACESTATE"),
	})

	rt.Tracer = otel.Tracer(opts.ServiceName)

	// Emit a single retroactive bootstrap span so the timing of the
	// pre-SDK setup remains queryable. WithStartTime stretches the
	// span back to actual ssh.Dial entry; End is called from Close.
	ctx, rt.bootstrapSpan = rt.Tracer.Start(ctx, "verself_deploy.bootstrap",
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithTimestamp(bootstrapStart),
		trace.WithAttributes(
			attribute.String("verself.site", site),
			attribute.String("ssh.access_label", selected.Label),
			attribute.String("ssh.host", selected.Host),
			attribute.String("ssh.user", selected.User),
			attribute.Bool("verself.skip_otlp_forward", opts.SkipOTLPForward),
		),
	)
	rt.Ctx = ctx
	return rt, nil
}

// Close ends the bootstrap span, drains the OTel SDK over the
// SSH-forwarded OTLP channel, and closes SSH. Idempotent on multiple
// calls; a defer rt.Close() at the call site is the canonical
// pattern.
func (rt *Runtime) Close() {
	if rt == nil {
		return
	}
	// Stamp bootstrap-end now so the span's duration covers the
	// full subcommand body — the bootstrap span is conceptually the
	// "verself-deploy did stuff" parent. Sub-operations create their
	// own children of it through the returned context.
	if rt.bootstrapSpan != nil {
		rt.bootstrapSpan.SetAttributes(
			attribute.Int64("verself.bootstrap.duration_ms", time.Since(rt.bootstrapStart).Milliseconds()),
		)
		rt.bootstrapSpan.End()
		rt.bootstrapSpan = nil
	}
	if rt.otelShutdown != nil {
		flushCtx, cancel := context.WithTimeout(context.Background(), otelShutdownBudget)
		_ = rt.otelShutdown(flushCtx)
		cancel()
		rt.otelShutdown = nil
	}
	if rt.SSH != nil {
		_ = rt.SSH.Close()
		rt.SSH = nil
	}
}

// OTLPEndpoint is the host:port the SSH-forwarded OTLP channel binds
// on the controller's loopback, or the empty string when
// SkipOTLPForward was set. Surfaced so subprocess invocations
// (reconciler scripts, ansible-playbook) can be told where to ship
// their spans.
func (rt *Runtime) OTLPEndpoint() string {
	if rt.otlpForward == nil {
		return ""
	}
	return rt.otlpForward.ListenAddr
}

func dialSSHAccess(ctx context.Context, targets []sshAccessTarget) (*sshtun.Client, sshAccessTarget, error) {
	if len(targets) == 0 {
		return nil, sshAccessTarget{}, errors.New("runtime: no SSH access targets resolved")
	}
	failures := make([]string, 0, len(targets))
	for _, target := range targets {
		client, err := sshtun.Dial(ctx, target.Host, target.User, target.Ports)
		if err == nil {
			return client, target, nil
		}
		failures = append(failures, fmt.Sprintf("%s: %v", target.display(), err))
	}
	return nil, sshAccessTarget{}, fmt.Errorf(
		"runtime: unable to establish operator SSH access; tried targets in order: %s",
		strings.Join(failures, "; "),
	)
}

// resolveInfraSSHAccess reads the authored per-site substrate inventory just far
// enough to bootstrap SSH. Full Ansible inventory semantics stay with Ansible.
func resolveInfraSSHAccess(repoRoot, site string) ([]sshAccessTarget, error) {
	path := filepath.Join(repoRoot, "src", "host", "sites", site, "inventory.ini")
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open inventory: %w", err)
	}
	defer func() { _ = f.Close() }()

	var (
		section     string
		ansibleUser string
		first       *sshAccessTarget
		recovery    *sshAccessTarget
	)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.Trim(line, "[]")
			continue
		}
		if strings.HasSuffix(section, ":vars") {
			if k, v, ok := splitInventoryKV(line); ok && k == "ansible_user" {
				ansibleUser = v
			}
			continue
		}
		if section != "infra" || first != nil {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		alias := fields[0]
		host := alias
		user := ""
		port := 0
		recoveryHost := ""
		recoveryUser := ""
		recoveryPort := 0
		for _, field := range fields[1:] {
			if k, v, ok := splitInventoryKV(field); ok {
				switch k {
				case "ansible_host":
					host = v
				case "ansible_user":
					user = v
				case "ansible_port":
					parsed, err := strconv.Atoi(v)
					if err != nil || parsed <= 0 || parsed > 65535 {
						return nil, fmt.Errorf("inventory: invalid ansible_port %q for %s", v, alias)
					}
					port = parsed
				case "verself_recovery_ssh_host":
					recoveryHost = v
				case "verself_recovery_ssh_user":
					recoveryUser = v
				case "verself_recovery_ssh_port":
					parsed, err := strconv.Atoi(v)
					if err != nil || parsed <= 0 || parsed > 65535 {
						return nil, fmt.Errorf("inventory: invalid verself_recovery_ssh_port %q for %s", v, alias)
					}
					recoveryPort = parsed
				}
			}
		}
		ports := []int{22}
		if port > 0 {
			ports = []int{port}
		}
		first = &sshAccessTarget{
			Label: "pomerium",
			Alias: alias,
			Host:  host,
			User:  user,
			Ports: ports,
		}
		if recoveryHost != "" || recoveryUser != "" || recoveryPort != 0 {
			if recoveryHost == "" || recoveryUser == "" || recoveryPort == 0 {
				return nil, fmt.Errorf("inventory: %s recovery SSH requires verself_recovery_ssh_host, verself_recovery_ssh_user, and verself_recovery_ssh_port", alias)
			}
			recovery = &sshAccessTarget{
				Label: "recovery",
				Alias: alias,
				Host:  recoveryHost,
				User:  recoveryUser,
				Ports: []int{recoveryPort},
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read inventory: %w", err)
	}
	if first == nil {
		return nil, fmt.Errorf("inventory %s has no entries under [infra]", path)
	}
	if first.User == "" {
		first.User = ansibleUser
	}
	if first.User == "" {
		return nil, errors.New("inventory: no ansible_user set on the [infra] host or in [all:vars]")
	}
	targets := []sshAccessTarget{*first}
	if recovery != nil {
		targets = append(targets, *recovery)
	}
	return targets, nil
}

func splitInventoryKV(s string) (key, value string, ok bool) {
	idx := strings.Index(s, "=")
	if idx < 0 {
		return "", "", false
	}
	return strings.TrimSpace(s[:idx]), strings.TrimSpace(s[idx+1:]), true
}
