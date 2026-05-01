// Package runtime is the per-subcommand bootstrap that every
// verself-deploy entry point routes through. It owns the controlled
// startup ordering — SSH dial, OTLP forward channel, otelcol-contrib
// supervisor, OTel SDK init — and the reverse-order shutdown that
// drains spans before tearing the channel down.
//
// Boundary: this package wires existing internal packages together;
// it does not re-implement them. SSH lives in sshtun, the agent in
// otelagent, the OTel SDK in github.com/verself/otel, ClickHouse
// writes in chwriter. The runtime is the one place that knows the
// correct order to start and stop them.
package runtime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	verselfotel "github.com/verself/otel"

	"github.com/verself/deployment-tooling/internal/chwriter"
	"github.com/verself/deployment-tooling/internal/identity"
	"github.com/verself/deployment-tooling/internal/inventory"
	"github.com/verself/deployment-tooling/internal/otelagent"
	"github.com/verself/deployment-tooling/internal/sshtun"
)

// agentForwardRemotePort matches the bare-metal otelcol's OTLP gRPC
// receiver. The substrate role binds it; the ssh forward proxies to
// the same port on the remote loopback.
const agentForwardRemotePort = 4317

// Options configure a Bootstrap call. ServiceName is the only
// required field; the rest carry sensible defaults.
type Options struct {
	ServiceName    string
	ServiceVersion string

	// Site selects per-site state (inventory file when InfraHost is
	// unset, agent queue dir, baggage attributes). Defaults to "prod".
	Site string

	// RepoRoot is the absolute path to the verself-sh checkout. Used
	// only to find the rendered inventory at .cache/render/<site>/.
	// When empty, the process cwd is used.
	RepoRoot string

	// InfraHost overrides inventory resolution. Useful for one-shot
	// invocations that don't sit on top of a cue-renderer cache.
	InfraHost string
	InfraUser string

	// SkipAgent suppresses otelcol-contrib startup. The OTel SDK
	// still initializes (against whatever OTEL_EXPORTER_OTLP_ENDPOINT
	// the env carries) so spans can flow through a parent's agent.
	// Used by subcommands that run inside a parent verself-deploy
	// process tree (e.g. `ledger record-event` invoked by AXL while
	// the AXL-level run is itself agentless).
	SkipAgent bool
}

// Runtime is the resolved bootstrap surface. Callers consume Tracer,
// SSH (for forwards/exec), and ClickHouse (for typed inserts); the
// rest is internal bookkeeping.
type Runtime struct {
	Ctx        context.Context
	Tracer     trace.Tracer
	Identity   identity.Snapshot
	SSH        *sshtun.Client
	ClickHouse *chwriter.Writer
	Site       string
	RepoRoot   string

	agent       *otelagent.Agent
	otlpForward *sshtun.Forward
	otelShutdown func(context.Context) error

	bootstrapSpan trace.Span
	bootstrapStart time.Time
}

// Init brings up SSH, the OTLP forward, the otelcol supervisor, and
// the OTel SDK in that order. The returned Runtime owns shutdown via
// Close — defer that on the caller and the reverse ordering happens
// automatically.
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

	host, user := opts.InfraHost, opts.InfraUser
	if host == "" || user == "" {
		resolved, err := resolveInfraHost(repoRoot, site)
		if err != nil {
			return nil, err
		}
		if host == "" {
			host = resolved.Host
		}
		if user == "" {
			user = resolved.User
		}
	}

	rt := &Runtime{
		Site:           site,
		RepoRoot:       repoRoot,
		bootstrapStart: bootstrapStart,
	}

	sshClient, err := sshtun.Dial(ctx, host, user)
	if err != nil {
		return nil, err
	}
	rt.SSH = sshClient

	if !opts.SkipAgent {
		forward, err := sshClient.Forward(ctx, "otlp", agentForwardRemotePort)
		if err != nil {
			_ = sshClient.Close()
			return nil, fmt.Errorf("runtime: open OTLP forward: %w", err)
		}
		rt.otlpForward = forward

		agent, err := otelagent.Start(ctx, otelagent.Config{
			ForwardEndpoint: forward.ListenAddr,
			Site:            site,
		})
		if err != nil {
			_ = sshClient.Close()
			return nil, fmt.Errorf("runtime: start agent: %w", err)
		}
		rt.agent = agent

		// Point both the parent's SDK and any spawned children
		// (ansible-playbook with the verself_otel callback) at the
		// local agent. The OTel SDK reads OTEL_EXPORTER_OTLP_ENDPOINT
		// once at Init; setting it before verselfotel.Init ensures
		// the export pipeline is bound to the agent.
		_ = os.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://"+agent.Endpoint())
	}

	otelShutdown, _, err := verselfotel.Init(ctx, verselfotel.Config{
		ServiceName:    opts.ServiceName,
		ServiceVersion: opts.ServiceVersion,
	})
	if err != nil {
		if rt.agent != nil {
			_ = rt.agent.Shutdown(context.Background())
		}
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
	rt.ClickHouse = chwriter.New(sshClient, "verself")

	// Emit a single retroactive bootstrap span so the timing of the
	// pre-SDK setup remains queryable. WithStartTime stretches the
	// span back to actual ssh.Dial entry; End is called from Close.
	ctx, rt.bootstrapSpan = rt.Tracer.Start(ctx, "verself_deploy.bootstrap",
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithTimestamp(bootstrapStart),
		trace.WithAttributes(
			attribute.String("verself.site", site),
			attribute.String("ssh.host", host),
			attribute.String("ssh.user", user),
			attribute.Bool("verself.skip_agent", opts.SkipAgent),
		),
	)
	rt.Ctx = ctx
	return rt, nil
}

// Close drains the OTel SDK (so pending spans hit the agent's
// in-memory batch), tears the agent down (so the queue flushes
// upstream), and closes SSH. Idempotent on multiple calls; a defer
// rt.Close() at the call site is the canonical pattern.
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
		flushCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		_ = rt.otelShutdown(flushCtx)
		cancel()
		rt.otelShutdown = nil
	}
	if rt.agent != nil {
		_ = rt.agent.Shutdown(context.Background())
		rt.agent = nil
	}
	if rt.SSH != nil {
		_ = rt.SSH.Close()
		rt.SSH = nil
	}
}

// AgentEndpoint is the OTLP endpoint the agent is listening on, or
// the empty string when SkipAgent was set. Surfaced so subprocess
// invocations (ansible-playbook) can be told where to ship their
// spans without re-deriving the constant.
func (rt *Runtime) AgentEndpoint() string {
	if rt.agent == nil {
		return ""
	}
	return rt.agent.Endpoint()
}

// AgentLogPath surfaces the agent's stdout/stderr capture file so a
// startup failure can be referenced in error messages. Empty when no
// agent was started.
func (rt *Runtime) AgentLogPath() string {
	if rt.agent == nil {
		return ""
	}
	return rt.agent.LogPath()
}

// resolveInfraHost prefers .cache/render/<site>/inventory/hosts.ini
// (the rendered, deploy-ready inventory) and falls back to the
// authored src/substrate/ansible/inventory/<site>.ini for one-shot
// invocations not preceded by `aspect render`.
func resolveInfraHost(repoRoot, site string) (*inventory.Host, error) {
	cachePath := filepath.Join(repoRoot, ".cache", "render", site, "inventory", "hosts.ini")
	if _, err := os.Stat(cachePath); err == nil {
		return inventory.LoadInfra(cachePath)
	}
	return inventory.LoadInfra(filepath.Join(repoRoot, "src", "substrate", "ansible", "inventory", site+".ini"))
}
