package ansible

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/verself/deployment-tools/internal/chwriter"
)

const tracerName = "github.com/verself/deployment-tools/internal/ansible"

// flushBatchSize is the number of buffered task events that triggers
// a ClickHouse insert. A typical layer playbook emits 50–200 tasks;
// flushing in batches keeps insert overhead bounded without waiting
// for the playbook to finish.
const flushBatchSize = 64

// flushInterval is the maximum time between insert flushes. Combined
// with flushBatchSize, this caps the worst-case visibility lag for a
// task event at ~1s.
const flushInterval = 1 * time.Second

// Options configure a Run call. Playbook and Inventory are required;
// the rest are optional contextual labels surfaced as span attributes
// and ClickHouse columns.
type Options struct {
	// Playbook is the playbook path passed to ansible-playbook; it is
	// resolved relative to AnsibleDir.
	Playbook string
	// Inventory is the -i argument; an absolute path is required so
	// the playbook is independent of process cwd.
	Inventory string
	// AnsibleDir is the working directory ansible-playbook runs in
	// (typically src/substrate/ansible). Required so include/import
	// paths resolve.
	AnsibleDir string
	// ExtraArgs are appended to the ansible-playbook command line.
	ExtraArgs []string
	// Site, Layer label rows in verself.ansible_task_events and span
	// attributes. Layer is the substrate layer name when the playbook
	// is one of the L1/L2/L3/L4a layer playbooks; empty for one-shot
	// invocations.
	Site  string
	Layer string

	// OTLPEndpoint, when non-empty, sets OTEL_EXPORTER_OTLP_ENDPOINT
	// on the child process so any OTel SDK it loads (or scripts it
	// invokes) ships through the parent's SSH-forwarded tunnel
	// instead of the SDK default 127.0.0.1:4317. Pass through the
	// listen address from runtime.Runtime.OTLPEndpoint().
	OTLPEndpoint string

	// AdditionalEnv is appended to the child process environment.
	// Identity (VERSELF_DEPLOY_RUN_KEY etc.) and OTel correlation
	// (TRACEPARENT, OTEL_RESOURCE_ATTRIBUTES) come from os.Environ()
	// so the parent's existing env propagation contract is preserved.
	AdditionalEnv []string
}

// Result is the outcome of one playbook run. ChangedCount is the
// summed PLAY RECAP changed= column across hosts — the legacy column
// consumed by verself.deploy_layer_runs. ExitCode mirrors the child's
// exit code so callers can decide whether to treat the run as failed.
type Result struct {
	ExitCode     int
	ChangedCount int
	Recap        PlayRecap
	TaskCount    int
	FailedCount  int
}

// Run spawns ansible-playbook, streams its stdout through the parser,
// fans events out to (a) the operator's terminal and (b) per-task
// span/row emission, and waits for the child to exit.
//
// The ClickHouse writer may be nil; the run still emits per-task
// spans via OTel. Persistence to verself.ansible_task_events is
// suppressed when w is nil (used by tests).
func Run(ctx context.Context, w *chwriter.Writer, opts Options) (*Result, error) {
	if opts.Playbook == "" {
		return nil, fmt.Errorf("ansible: Playbook is required")
	}
	if opts.Inventory == "" {
		return nil, fmt.Errorf("ansible: Inventory is required")
	}
	ansibleDir := opts.AnsibleDir
	if ansibleDir == "" {
		return nil, fmt.Errorf("ansible: AnsibleDir is required")
	}
	if !filepath.IsAbs(opts.Inventory) {
		return nil, fmt.Errorf("ansible: Inventory must be an absolute path: %q", opts.Inventory)
	}

	tracer := otel.Tracer(tracerName)
	ctx, span := tracer.Start(ctx, "verself_deploy.ansible.run",
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(
			attribute.String("ansible.playbook", opts.Playbook),
			attribute.String("ansible.inventory", opts.Inventory),
			attribute.String("verself.site", opts.Site),
			attribute.String("verself.layer", opts.Layer),
		),
	)
	defer span.End()

	args := append([]string{"-i", opts.Inventory, opts.Playbook}, opts.ExtraArgs...)
	cmd := exec.CommandContext(ctx, "ansible-playbook", args...)
	cmd.Dir = ansibleDir

	cmd.Env = buildChildEnv(opts)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("ansible: stdout pipe: %w", err)
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("ansible: start ansible-playbook: %w", err)
	}

	// io.MultiWriter would tee a write side; here we have a read
	// side, so duplicate via TeeReader: anything we read is also
	// written to os.Stdout, preserving the operator's live output.
	tee := io.TeeReader(stdout, os.Stdout)
	parser := NewParser(tee)

	recorder := &recorder{
		ctx:    ctx,
		tracer: tracer,
		writer: w,
		opts:   opts,
		runKey: os.Getenv("VERSELF_DEPLOY_RUN_KEY"),
	}

	parserDone := make(chan error, 1)
	go func() {
		parserDone <- parser.Run()
	}()

	recorder.consume(parser.Events())

	// Drain the parser before reaping the child — TeeReader returning
	// EOF is what closes the events channel, so consume blocks until
	// the parser Run goroutine sees EOF on stdout.
	parserErr := <-parserDone
	waitErr := cmd.Wait()

	// Final flush captures any tail of events buffered below the
	// batch threshold.
	if err := recorder.flush(); err != nil {
		// Parser/exec errors are more interesting; record but don't
		// override.
		span.RecordError(err)
	}

	exitCode := 0
	if waitErr != nil {
		var ee *exec.ExitError
		if errors.As(waitErr, &ee) {
			exitCode = ee.ExitCode()
		} else {
			span.RecordError(waitErr)
			span.SetStatus(codes.Error, waitErr.Error())
			return nil, fmt.Errorf("ansible: ansible-playbook: %w", waitErr)
		}
	}

	recap := parser.Recap()
	res := &Result{
		ExitCode:     exitCode,
		ChangedCount: recap.ChangedTotal(),
		Recap:        recap,
		TaskCount:    recorder.taskCount,
		FailedCount:  recorder.failedCount,
	}

	span.SetAttributes(
		attribute.Int("ansible.exit_code", exitCode),
		attribute.Int("ansible.changed_total", res.ChangedCount),
		attribute.Int("ansible.task_count", res.TaskCount),
		attribute.Int("ansible.failed_count", res.FailedCount),
	)
	if exitCode != 0 {
		span.SetStatus(codes.Error, fmt.Sprintf("ansible-playbook exited %d", exitCode))
	} else {
		span.SetStatus(codes.Ok, "")
	}
	if parserErr != nil {
		span.RecordError(parserErr)
	}
	return res, nil
}

// recorder fans TaskEvents out to per-task spans and a buffered
// ClickHouse insert. Single-goroutine: the consume loop owns the
// state so no mutex is needed.
type recorder struct {
	ctx    context.Context
	tracer trace.Tracer
	writer *chwriter.Writer
	opts   Options
	runKey string

	buffered    []chwriter.Row
	taskCount   int
	failedCount int
}

func (r *recorder) consume(events <-chan TaskEvent) {
	timer := time.NewTimer(flushInterval)
	defer timer.Stop()

	for {
		select {
		case ev, ok := <-events:
			if !ok {
				return
			}
			r.observe(ev)
			if len(r.buffered) >= flushBatchSize {
				_ = r.flush()
				resetTimer(timer, flushInterval)
			}
		case <-timer.C:
			_ = r.flush()
			resetTimer(timer, flushInterval)
		}
	}
}

func (r *recorder) observe(ev TaskEvent) {
	r.taskCount++
	if ev.Status == StatusFailed || ev.Status == StatusUnreachable {
		r.failedCount++
	}

	// Per-task span emitted by the Go-side parser. Sole producer of
	// `verself_deploy.ansible.task` — the playbook itself no longer
	// loads an OTel callback.
	_, span := r.tracer.Start(r.ctx, "verself_deploy.ansible.task",
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithTimestamp(ev.Time),
		trace.WithAttributes(
			attribute.String("ansible.play", ev.Play),
			attribute.String("ansible.task", ev.Task),
			attribute.String("ansible.host", ev.Host),
			attribute.String("ansible.status", string(ev.Status)),
			attribute.String("ansible.item", ev.Item),
			attribute.Int64("ansible.duration_ms", ev.DurationMs),
			attribute.String("verself.layer", r.opts.Layer),
		),
	)
	if ev.Status == StatusFailed || ev.Status == StatusUnreachable {
		span.SetStatus(codes.Error, ev.Message)
	}
	span.End(trace.WithTimestamp(ev.Time))

	if r.writer == nil {
		return
	}
	r.buffered = append(r.buffered, chwriter.Row{
		"event_at":       chwriter.DateTime(ev.Time),
		"deploy_run_key": chwriter.String(r.runKey),
		"site":           chwriter.String(r.opts.Site),
		"layer":          chwriter.String(r.opts.Layer),
		"playbook":       chwriter.String(r.opts.Playbook),
		"play":           chwriter.String(ev.Play),
		"task":           chwriter.String(ev.Task),
		"host":           chwriter.String(ev.Host),
		"status":         chwriter.String(string(ev.Status)),
		"item":           chwriter.String(ev.Item),
		"duration_ms":    chwriter.UInt(uint64Of(ev.DurationMs)),
		"message":        chwriter.String(ev.Message),
	})
}

func (r *recorder) flush() error {
	if r.writer == nil || len(r.buffered) == 0 {
		r.buffered = nil
		return nil
	}
	rows := r.buffered
	r.buffered = nil
	return r.writer.InsertRows(r.ctx, "ansible_task_events", rows)
}

// resetTimer resets t to fire after d, draining any pending value
// per the time.Timer Reset contract.
func resetTimer(t *time.Timer, d time.Duration) {
	if !t.Stop() {
		select {
		case <-t.C:
		default:
		}
	}
	t.Reset(d)
}

// buildChildEnv composes the env for ansible-playbook. The parent's
// env is the base; OTLPEndpoint pins OTEL_EXPORTER_OTLP_ENDPOINT to
// the SSH-forwarded tunnel for any subprocess that itself initialises
// an OTel SDK; AdditionalEnv layers on top.
func buildChildEnv(opts Options) []string {
	env := os.Environ()
	if opts.OTLPEndpoint != "" {
		env = append(env, "OTEL_EXPORTER_OTLP_ENDPOINT=http://"+opts.OTLPEndpoint)
		env = append(env, "VERSELF_OTLP_ENDPOINT="+opts.OTLPEndpoint)
	}
	if opts.Layer != "" {
		env = append(env, "VERSELF_LAYER="+opts.Layer)
	}
	if len(opts.AdditionalEnv) > 0 {
		env = append(env, opts.AdditionalEnv...)
	}
	return env
}

// uint64Of clamps a possibly-negative int64 to zero before widening.
// DurationMs can in theory be negative if the system clock jumps; we
// don't want to insert a wraparound value into a UInt32 column.
func uint64Of(n int64) uint64 {
	if n < 0 {
		return 0
	}
	return uint64(n)
}
