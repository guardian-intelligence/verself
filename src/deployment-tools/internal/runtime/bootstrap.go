// Package runtime is the per-subcommand bootstrap that every
// verself-deploy entry point routes through. It owns the controlled
// startup ordering — SSH dial, OTLP forward channel, OTel SDK init —
// and the reverse-order shutdown that flushes spans before tearing
// the channel down.
//
// Boundary: this package wires existing internal packages together;
// it does not re-implement them. SSH lives in sshtun, the OTel SDK
// in github.com/verself/observability/otel, ClickHouse writes in chwriter. The
// runtime is the one place that knows the correct order to start
// and stop them.
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

	verselfotel "github.com/verself/observability/otel"

	"github.com/verself/deployment-tools/internal/chwriter"
	"github.com/verself/deployment-tools/internal/identity"
	"github.com/verself/deployment-tools/internal/inventory"
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

	otlpForward  *sshtun.Forward
	otelShutdown func(context.Context) error

	bootstrapSpan  trace.Span
	bootstrapStart time.Time
}

// Init brings up SSH, the OTLP forward, and the OTel SDK in that
// order. The returned Runtime owns shutdown via Close — defer that
// on the caller and the reverse ordering happens automatically.
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

// resolveInfraHost reads the authored per-site substrate inventory.
func resolveInfraHost(repoRoot, site string) (*inventory.Host, error) {
	return inventory.LoadInfra(filepath.Join(repoRoot, "src", "substrate", "ansible", "inventory", site+".ini"))
}
