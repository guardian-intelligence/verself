package vmorchestrator

import (
	"context"
	"time"

	"github.com/verself/vm-orchestrator/vmproto"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

func detachedTraceContext(ctx context.Context) context.Context {
	out := context.Background()
	if ctx == nil {
		return out
	}
	if spanContext := trace.SpanContextFromContext(ctx); spanContext.IsValid() {
		out = trace.ContextWithSpanContext(out, spanContext)
	}
	if bg := baggage.FromContext(ctx); bg.Len() > 0 {
		out = baggage.ContextWithBaggage(out, bg)
	}
	return out
}

func startStepSpan(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, func(error)) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, span := tracer.Start(ctx, name, trace.WithAttributes(attrs...))
	return ctx, func(err error) {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}
}

func recordObservedIntervalSpan(ctx context.Context, name string, startedAt, endedAt time.Time, attrs ...attribute.KeyValue) {
	if startedAt.IsZero() || endedAt.IsZero() || endedAt.Before(startedAt) {
		return
	}
	attrs = append(attrs, attribute.Int64("observed.duration_ms", endedAt.Sub(startedAt).Milliseconds()))
	_, span := tracer.Start(ctx, name, trace.WithTimestamp(startedAt), trace.WithAttributes(attrs...))
	span.End(trace.WithTimestamp(endedAt))
}

func recordGuestBootTimingSpans(ctx context.Context, leaseID string, hello vmproto.Hello, observedAt time.Time) {
	timings := hello.BootTimings
	if timings == nil {
		return
	}
	totalMS := maxInt64(
		hello.BootToReadyMS,
		timings.MountVirtualFilesystemsDoneMS,
		timings.ConfigureLoopbackDoneMS,
		timings.SetSubreaperDoneMS,
		timings.StartTelemetryDoneMS,
		timings.SignalNotifyDoneMS,
		timings.VSockListenDoneMS,
		timings.VSockAcceptDoneMS,
		timings.AgentSessionReadyMS,
		timings.AgentIOLoopsStartedMS,
		timings.HelloEnqueueDoneMS,
	)
	if totalMS <= 0 {
		return
	}
	base := observedAt.Add(-time.Duration(totalMS) * time.Millisecond)
	reportCtx, reportSpan := tracer.Start(ctx, "vmorchestrator.guest.boot_report",
		trace.WithTimestamp(base),
		trace.WithAttributes(
			attribute.String("lease.id", leaseID),
			attribute.Int64("guest.boot.total_ms", totalMS),
			attribute.Int64("guest.kernel_boot_to_init_start_ms", timings.KernelBootToInitStartMS),
			attribute.Int64("guest.kernel_boot_to_vsock_listen_done_ms", timings.KernelBootToVSockListenDoneMS),
			attribute.Int64("guest.kernel_boot_to_vsock_accept_done_ms", timings.KernelBootToVSockAcceptDoneMS),
			attribute.Int64("guest.kernel_boot_to_hello_enqueue_start_ms", timings.KernelBootToHelloEnqueueStartMS),
			attribute.Int64("guest.boot.boot_to_ready_ms", hello.BootToReadyMS),
			attribute.Int64("guest.boot.mount_virtual_filesystems_done_ms", timings.MountVirtualFilesystemsDoneMS),
			attribute.Int64("guest.boot.configure_loopback_done_ms", timings.ConfigureLoopbackDoneMS),
			attribute.Int64("guest.boot.set_subreaper_done_ms", timings.SetSubreaperDoneMS),
			attribute.Int64("guest.boot.start_telemetry_done_ms", timings.StartTelemetryDoneMS),
			attribute.Int64("guest.boot.signal_notify_done_ms", timings.SignalNotifyDoneMS),
			attribute.Int64("guest.boot.vsock_listen_done_ms", timings.VSockListenDoneMS),
			attribute.Int64("guest.boot.vsock_accept_done_ms", timings.VSockAcceptDoneMS),
			attribute.Int64("guest.boot.agent_session_ready_ms", timings.AgentSessionReadyMS),
			attribute.Int64("guest.boot.agent_io_loops_started_ms", timings.AgentIOLoopsStartedMS),
			attribute.Int64("guest.boot.hello_enqueue_done_ms", timings.HelloEnqueueDoneMS),
		),
	)
	if timings.KernelBootToInitStartMS > 0 {
		kernelStartedAt := base.Add(-time.Duration(timings.KernelBootToInitStartMS) * time.Millisecond)
		addGuestKernelUptimeSpan(reportCtx, leaseID, kernelStartedAt, "vmorchestrator.guest.kernel_boot_to_pid1", "pid1_start", timings.KernelBootToInitStartMS)
		addGuestKernelUptimeSpan(reportCtx, leaseID, kernelStartedAt, "vmorchestrator.guest.kernel_boot_to_vsock_listen", "vsock_listen_done", timings.KernelBootToVSockListenDoneMS)
		addGuestKernelUptimeSpan(reportCtx, leaseID, kernelStartedAt, "vmorchestrator.guest.kernel_boot_to_vsock_accept", "vsock_accept_done", timings.KernelBootToVSockAcceptDoneMS)
		addGuestKernelUptimeSpan(reportCtx, leaseID, kernelStartedAt, "vmorchestrator.guest.kernel_boot_to_hello_enqueue", "hello_enqueue_start", timings.KernelBootToHelloEnqueueStartMS)
	}
	addGuestBootStepSpan(reportCtx, leaseID, base, "vmorchestrator.guest.boot.mount_virtual_filesystems", "mount_virtual_filesystems", timings.MountVirtualFilesystemsStartMS, timings.MountVirtualFilesystemsDoneMS)
	addGuestBootStepSpan(reportCtx, leaseID, base, "vmorchestrator.guest.boot.configure_loopback", "configure_loopback", timings.ConfigureLoopbackStartMS, timings.ConfigureLoopbackDoneMS)
	addGuestBootStepSpan(reportCtx, leaseID, base, "vmorchestrator.guest.boot.set_subreaper", "set_subreaper", timings.SetSubreaperStartMS, timings.SetSubreaperDoneMS)
	addGuestBootStepSpan(reportCtx, leaseID, base, "vmorchestrator.guest.boot.start_telemetry_process", "start_telemetry_process", timings.StartTelemetryStartMS, timings.StartTelemetryDoneMS)
	addGuestBootStepSpan(reportCtx, leaseID, base, "vmorchestrator.guest.boot.signal_notify", "signal_notify", timings.SignalNotifyStartMS, timings.SignalNotifyDoneMS)
	addGuestBootStepSpan(reportCtx, leaseID, base, "vmorchestrator.guest.boot.vsock_listen", "vsock_listen", timings.VSockListenStartMS, timings.VSockListenDoneMS)
	addGuestBootStepSpan(reportCtx, leaseID, base, "vmorchestrator.guest.boot.vsock_accept_wait", "vsock_accept_wait", timings.VSockAcceptStartMS, timings.VSockAcceptDoneMS)
	addGuestBootStepSpan(reportCtx, leaseID, base, "vmorchestrator.guest.boot.agent_session_init", "agent_session_init", timings.AgentStartMS, timings.AgentSessionReadyMS)
	addGuestBootStepSpan(reportCtx, leaseID, base, "vmorchestrator.guest.boot.agent_io_loops_start", "agent_io_loops_start", timings.AgentSessionReadyMS, timings.AgentIOLoopsStartedMS)
	addGuestBootStepSpan(reportCtx, leaseID, base, "vmorchestrator.guest.boot.hello_enqueue", "hello_enqueue", timings.HelloEnqueueStartMS, timings.HelloEnqueueDoneMS)
	reportSpan.End(trace.WithTimestamp(observedAt))
}

func addGuestKernelUptimeSpan(ctx context.Context, leaseID string, kernelStartedAt time.Time, name, marker string, endMS int64) {
	if endMS <= 0 {
		return
	}
	endedAt := kernelStartedAt.Add(time.Duration(endMS) * time.Millisecond)
	_, span := tracer.Start(ctx, name,
		trace.WithTimestamp(kernelStartedAt),
		trace.WithAttributes(
			attribute.String("lease.id", leaseID),
			attribute.String("guest.kernel.marker", marker),
			attribute.Int64("guest.kernel.uptime_end_ms", endMS),
		),
	)
	span.End(trace.WithTimestamp(endedAt))
}

func addGuestBootStepSpan(ctx context.Context, leaseID string, base time.Time, name, step string, startMS, endMS int64) {
	if startMS < 0 || endMS < startMS {
		return
	}
	if startMS == 0 && endMS == 0 {
		return
	}
	startedAt := base.Add(time.Duration(startMS) * time.Millisecond)
	endedAt := base.Add(time.Duration(endMS) * time.Millisecond)
	_, span := tracer.Start(ctx, name,
		trace.WithTimestamp(startedAt),
		trace.WithAttributes(
			attribute.String("lease.id", leaseID),
			attribute.String("guest.boot.step", step),
			attribute.Int64("guest.boot.start_ms", startMS),
			attribute.Int64("guest.boot.end_ms", endMS),
			attribute.Int64("guest.boot.duration_ms", endMS-startMS),
		),
	)
	span.End(trace.WithTimestamp(endedAt))
}

func maxInt64(values ...int64) int64 {
	var max int64
	for _, value := range values {
		if value > max {
			max = value
		}
	}
	return max
}
