package ansible

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/verself/deployment-tools/internal/deploydb"
)

const tracerName = "github.com/verself/deployment-tools/internal/ansible"

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
	// (typically src/host/ansible). Required so include/import
	// paths resolve.
	AnsibleDir string
	// ExtraArgs are appended to the ansible-playbook command line.
	ExtraArgs []string
	// Site and Phase label rows in verself.ansible_task_events and span
	// attributes. The ClickHouse task-event projection still stores the
	// phase label in its historical `layer` column so deploy-time rows
	// remain writable before ClickHouse schema convergence has run.
	Site  string
	Phase string
	// RunKey is the deploy correlation key written with each task
	// event row. Required when a ClickHouse client is passed to Run.
	RunKey string

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
// summed PLAY RECAP changed= column across hosts. ExitCode mirrors the
// child's exit code so callers can decide whether to treat the run as failed.
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
// The ClickHouse client may be nil; the run still emits per-task
// spans via OTel. Persistence to verself.ansible_task_events is
// suppressed when db is nil (used by tests).
func Run(ctx context.Context, db *deploydb.Client, opts Options) (*Result, error) {
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
	sink, err := newTaskEventSink(db, opts)
	if err != nil {
		return nil, err
	}

	tracer := otel.Tracer(tracerName)
	ctx, span := tracer.Start(ctx, "verself_deploy.ansible.run",
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(
			attribute.String("ansible.playbook", opts.Playbook),
			attribute.String("ansible.inventory", opts.Inventory),
			attribute.String("verself.site", opts.Site),
			attribute.String("verself.phase", opts.Phase),
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
		if sink != nil {
			_ = sink.Close(ctx)
		}
		return nil, fmt.Errorf("ansible: stdout pipe: %w", err)
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		if sink != nil {
			_ = sink.Close(ctx)
		}
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
		sink:   sink,
		opts:   opts,
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
	closeCtx, cancelClose := context.WithTimeout(context.WithoutCancel(ctx), taskEventCloseTimeout)
	sinkErr := recorder.closeSink(closeCtx)
	cancelClose()
	waitErr := cmd.Wait()

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
	if sinkErr != nil {
		span.RecordError(sinkErr)
		span.SetStatus(codes.Error, sinkErr.Error())
		return res, fmt.Errorf("ansible: persist task events: %w", sinkErr)
	}
	return res, nil
}

// recorder fans TaskEvents out to per-task spans and a native
// ClickHouse batch sink. Single-goroutine: the consume loop owns the
// counters and first sink error.
type recorder struct {
	ctx    context.Context
	tracer trace.Tracer
	sink   *taskEventSink
	opts   Options

	taskCount   int
	failedCount int
	sinkErr     error
}

func (r *recorder) consume(events <-chan TaskEvent) {
	for ev := range events {
		r.observe(ev)
	}
}

func (r *recorder) closeSink(ctx context.Context) error {
	if r.sink == nil {
		return r.sinkErr
	}
	if err := r.sink.Close(ctx); err != nil && r.sinkErr == nil {
		r.sinkErr = err
	}
	return r.sinkErr
}

func (r *recorder) recordSinkError(err error) {
	if err != nil && r.sinkErr == nil {
		r.sinkErr = err
	}
}

func (r *recorder) recordTaskEvent(ev TaskEvent) {
	if r.sink == nil || r.sinkErr != nil {
		return
	}
	if err := r.sink.Record(r.ctx, ev); err != nil {
		r.recordSinkError(err)
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
			attribute.String("verself.phase", r.opts.Phase),
		),
	)
	if ev.Status == StatusFailed || ev.Status == StatusUnreachable {
		span.SetStatus(codes.Error, ev.Message)
	}
	span.End(trace.WithTimestamp(ev.Time))

	r.recordTaskEvent(ev)
}

// buildChildEnv composes the env for ansible-playbook. The parent's
// env is the base; OTLPEndpoint pins OTEL_EXPORTER_OTLP_ENDPOINT to
// the SSH-forwarded tunnel for any subprocess that itself initialises
// an OTel SDK; AdditionalEnv is appended last.
func buildChildEnv(opts Options) []string {
	env := os.Environ()
	if opts.OTLPEndpoint != "" {
		env = append(env, "OTEL_EXPORTER_OTLP_ENDPOINT=http://"+opts.OTLPEndpoint)
		env = append(env, "VERSELF_OTLP_ENDPOINT="+opts.OTLPEndpoint)
	}
	if opts.Phase != "" {
		env = append(env, "VERSELF_ANSIBLE_PHASE="+opts.Phase)
	}
	if len(opts.AdditionalEnv) > 0 {
		env = append(env, opts.AdditionalEnv...)
	}
	return env
}
