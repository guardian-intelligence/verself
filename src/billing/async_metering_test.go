package billing

import (
	"context"
	"sync"
	"testing"
	"time"
)

type recordingMeteringSink struct {
	mu        sync.Mutex
	batches   [][]MeteringRow
	startedCh chan struct{}
	releaseCh chan struct{}
}

func newRecordingMeteringSink() *recordingMeteringSink {
	return &recordingMeteringSink{
		startedCh: make(chan struct{}, 1),
	}
}

func (s *recordingMeteringSink) InsertMeteringRows(ctx context.Context, rows []MeteringRow) error {
	select {
	case s.startedCh <- struct{}{}:
	default:
	}

	if s.releaseCh != nil {
		select {
		case <-s.releaseCh:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	copied := make([]MeteringRow, len(rows))
	copy(copied, rows)

	s.mu.Lock()
	s.batches = append(s.batches, copied)
	s.mu.Unlock()
	return nil
}

func (s *recordingMeteringSink) snapshot() [][]MeteringRow {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([][]MeteringRow, len(s.batches))
	for i := range s.batches {
		out[i] = append([]MeteringRow(nil), s.batches[i]...)
	}
	return out
}

func TestAsyncMeteringWriterInsertReturnsBeforeBlockedFlush(t *testing.T) {
	t.Parallel()

	sink := newRecordingMeteringSink()
	sink.releaseCh = make(chan struct{})
	writer := newAsyncMeteringWriterWithSink(sink, AsyncMeteringWriterConfig{
		BufferSize:   2,
		BatchSize:    1,
		FlushEvery:   time.Hour,
		FlushTimeout: time.Second,
	})

	row1 := MeteringRow{OrgID: "1", ProductID: "sandbox", SourceType: "job", SourceRef: "job-1", WindowSeq: 0}
	row2 := MeteringRow{OrgID: "1", ProductID: "sandbox", SourceType: "job", SourceRef: "job-2", WindowSeq: 1}

	start := time.Now()
	if err := writer.InsertMeteringRow(context.Background(), row1); err != nil {
		t.Fatalf("insert first row: %v", err)
	}
	if time.Since(start) > 50*time.Millisecond {
		t.Fatalf("first insert blocked for %s", time.Since(start))
	}

	select {
	case <-sink.startedCh:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for first flush to start")
	}

	start = time.Now()
	if err := writer.InsertMeteringRow(context.Background(), row2); err != nil {
		t.Fatalf("insert second row: %v", err)
	}
	if time.Since(start) > 50*time.Millisecond {
		t.Fatalf("second insert blocked for %s", time.Since(start))
	}

	close(sink.releaseCh)

	closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := writer.Close(closeCtx); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	batches := sink.snapshot()
	if len(batches) != 2 {
		t.Fatalf("expected 2 flushed batches, got %d", len(batches))
	}
	if got := batches[0][0].SourceRef; got != "job-1" {
		t.Fatalf("first batch row = %q, want job-1", got)
	}
	if got := batches[1][0].SourceRef; got != "job-2" {
		t.Fatalf("second batch row = %q, want job-2", got)
	}
}

func TestAsyncMeteringWriterFlushesPartialBatchOnClose(t *testing.T) {
	t.Parallel()

	sink := newRecordingMeteringSink()
	writer := newAsyncMeteringWriterWithSink(sink, AsyncMeteringWriterConfig{
		BufferSize:   4,
		BatchSize:    3,
		FlushEvery:   time.Hour,
		FlushTimeout: time.Second,
	})

	rows := []MeteringRow{
		{OrgID: "1", ProductID: "sandbox", SourceRef: "job-1"},
		{OrgID: "1", ProductID: "sandbox", SourceRef: "job-2"},
	}
	for i := range rows {
		if err := writer.InsertMeteringRow(context.Background(), rows[i]); err != nil {
			t.Fatalf("insert row %d: %v", i, err)
		}
	}

	closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := writer.Close(closeCtx); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	batches := sink.snapshot()
	if len(batches) != 1 {
		t.Fatalf("expected 1 flushed batch, got %d", len(batches))
	}
	if len(batches[0]) != 2 {
		t.Fatalf("expected partial batch of 2 rows, got %d", len(batches[0]))
	}
	if got := batches[0][0].SourceRef; got != "job-1" {
		t.Fatalf("first row = %q, want job-1", got)
	}
	if got := batches[0][1].SourceRef; got != "job-2" {
		t.Fatalf("second row = %q, want job-2", got)
	}
}
