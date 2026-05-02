package ansible

import (
	"context"
	"fmt"
	"time"

	"github.com/verself/deployment-tools/internal/deploydb"
)

const (
	taskEventCloseTimeout = 5 * time.Second
	taskEventInitialRows  = 2048
)

type taskEventWriter interface {
	InsertAnsibleTaskEvents(context.Context, []deploydb.AnsibleTaskEventRow) error
}

type taskEventSink struct {
	writer taskEventWriter
	opts   Options
	rows   []deploydb.AnsibleTaskEventRow
	closed bool
}

func newTaskEventSink(db taskEventWriter, opts Options) (*taskEventSink, error) {
	if db == nil {
		return nil, nil
	}
	if err := validateTaskEventSinkOptions(opts); err != nil {
		return nil, err
	}
	s := &taskEventSink{
		writer: db,
		opts:   opts,
		rows:   make([]deploydb.AnsibleTaskEventRow, 0, taskEventInitialRows),
	}
	return s, nil
}

func (s *taskEventSink) Record(ctx context.Context, ev TaskEvent) error {
	if s.closed {
		return fmt.Errorf("ansible: task event sink closed")
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	s.rows = append(s.rows, taskEventRow(s.opts, ev))
	return nil
}

func (s *taskEventSink) Close(ctx context.Context) error {
	if s.closed {
		return nil
	}
	s.closed = true
	if len(s.rows) == 0 {
		return nil
	}
	rows := s.rows
	s.rows = nil
	// The per-insert round trip dominates small ClickHouse writes over
	// the SSH tunnel, so deploy task events persist as one close-time batch.
	return s.writer.InsertAnsibleTaskEvents(ctx, rows)
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
