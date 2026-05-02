package ansible

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/verself/deployment-tools/internal/deploydb"
)

const (
	taskEventBatchRows     = 512
	taskEventFlushInterval = 5 * time.Second
	taskEventCloseTimeout  = 5 * time.Second
	taskEventQueueDepth    = 8192
)

type taskEventWriter interface {
	InsertAnsibleTaskEvents(context.Context, []deploydb.AnsibleTaskEventRow) error
}

type taskEventSink struct {
	writer taskEventWriter
	opts   Options
	ctx    context.Context
	rows   chan deploydb.AnsibleTaskEventRow
	done   chan struct{}

	mu        sync.Mutex
	firstErr  error
	closeOnce sync.Once
}

func newTaskEventSink(ctx context.Context, db taskEventWriter, opts Options) (*taskEventSink, error) {
	if db == nil {
		return nil, nil
	}
	if err := validateTaskEventSinkOptions(opts); err != nil {
		return nil, err
	}
	s := &taskEventSink{
		writer: db,
		opts:   opts,
		ctx:    context.WithoutCancel(ctx),
		rows:   make(chan deploydb.AnsibleTaskEventRow, taskEventQueueDepth),
		done:   make(chan struct{}),
	}
	go s.run()
	return s, nil
}

func (s *taskEventSink) Record(ctx context.Context, ev TaskEvent) error {
	if err := s.err(); err != nil {
		return err
	}
	row := taskEventRow(s.opts, ev)
	select {
	case s.rows <- row:
		return nil
	case <-s.done:
		if err := s.err(); err != nil {
			return err
		}
		return fmt.Errorf("ansible: task event sink closed")
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *taskEventSink) Close(ctx context.Context) error {
	s.closeOnce.Do(func() {
		close(s.rows)
	})
	select {
	case <-s.done:
		return s.err()
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *taskEventSink) run() {
	defer close(s.done)

	timer := time.NewTimer(taskEventFlushInterval)
	defer timer.Stop()

	buffered := make([]deploydb.AnsibleTaskEventRow, 0, taskEventBatchRows)
	flush := func() bool {
		if len(buffered) == 0 {
			return true
		}
		rows := buffered
		buffered = make([]deploydb.AnsibleTaskEventRow, 0, taskEventBatchRows)
		if err := s.writer.InsertAnsibleTaskEvents(s.ctx, rows); err != nil {
			s.setErr(err)
			return false
		}
		return true
	}

	for {
		select {
		case row, ok := <-s.rows:
			if !ok {
				_ = flush()
				return
			}
			buffered = append(buffered, row)
			if len(buffered) >= taskEventBatchRows {
				if !flush() {
					return
				}
				resetTimer(timer, taskEventFlushInterval)
			}
		case <-timer.C:
			if !flush() {
				return
			}
			resetTimer(timer, taskEventFlushInterval)
		}
	}
}

func (s *taskEventSink) setErr(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil && s.firstErr == nil {
		s.firstErr = err
	}
}

func (s *taskEventSink) err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.firstErr
}

func taskEventRow(opts Options, ev TaskEvent) deploydb.AnsibleTaskEventRow {
	return deploydb.AnsibleTaskEventRow{
		EventAt:      ev.Time.UTC(),
		DeployRunKey: opts.RunKey,
		Site:         opts.Site,
		Layer:        opts.Phase,
		Playbook:     opts.Playbook,
		Play:         ev.Play,
		Task:         ev.Task,
		Host:         ev.Host,
		Status:       string(ev.Status),
		Item:         ev.Item,
		DurationMS:   durationMS(ev.DurationMs),
		Message:      ev.Message,
	}
}

func validateTaskEventSinkOptions(opts Options) error {
	if opts.RunKey == "" {
		return fmt.Errorf("ansible: RunKey is required when persisting task events")
	}
	if opts.Site == "" {
		return fmt.Errorf("ansible: Site is required when persisting task events")
	}
	if opts.Phase == "" {
		return fmt.Errorf("ansible: Phase is required when persisting task events")
	}
	if opts.Playbook == "" {
		return fmt.Errorf("ansible: Playbook is required when persisting task events")
	}
	return nil
}

func durationMS(n int64) uint32 {
	if n <= 0 {
		return 0
	}
	const maxUint32 = ^uint32(0)
	if n > int64(maxUint32) {
		return maxUint32
	}
	return uint32(n)
}

// resetTimer resets t to fire after d, draining any pending value per
// the time.Timer Reset contract.
func resetTimer(t *time.Timer, d time.Duration) {
	if !t.Stop() {
		select {
		case <-t.C:
		default:
		}
	}
	t.Reset(d)
}
