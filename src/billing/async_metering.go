package billing

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"
)

const (
	defaultAsyncMeteringBufferSize   = 4096
	defaultAsyncMeteringBatchSize    = 256
	defaultAsyncMeteringFlushTimeout = 5 * time.Second
	defaultAsyncMeteringFlushEvery   = 250 * time.Millisecond
)

var ErrAsyncMeteringWriterClosed = errors.New("billing: async metering writer is closed")

type meteringBatchSink interface {
	InsertMeteringRows(ctx context.Context, rows []MeteringRow) error
}

// AsyncMeteringWriter decouples Settle from ClickHouse latency by buffering rows
// in memory and flushing them to ClickHouse in batches from a background goroutine.
type AsyncMeteringWriter struct {
	ch           chan MeteringRow
	sink         meteringBatchSink
	batchSize    int
	flushEvery   time.Duration
	flushTimeout time.Duration
	done         chan struct{}
	mu           sync.RWMutex
	closed       bool
}

// AsyncMeteringWriterConfig controls buffering and flush behavior.
type AsyncMeteringWriterConfig struct {
	BufferSize   int
	BatchSize    int
	FlushEvery   time.Duration
	FlushTimeout time.Duration
}

func (cfg AsyncMeteringWriterConfig) withDefaults() AsyncMeteringWriterConfig {
	if cfg.BufferSize <= 0 {
		cfg.BufferSize = defaultAsyncMeteringBufferSize
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = defaultAsyncMeteringBatchSize
	}
	if cfg.FlushEvery <= 0 {
		cfg.FlushEvery = defaultAsyncMeteringFlushEvery
	}
	if cfg.FlushTimeout <= 0 {
		cfg.FlushTimeout = defaultAsyncMeteringFlushTimeout
	}
	return cfg
}

// NewAsyncMeteringWriter wraps a ClickHouse metering sink with an in-memory buffer.
func NewAsyncMeteringWriter(sink *ClickHouseMeteringWriter, cfg AsyncMeteringWriterConfig) *AsyncMeteringWriter {
	return newAsyncMeteringWriterWithSink(sink, cfg)
}

func newAsyncMeteringWriterWithSink(sink meteringBatchSink, cfg AsyncMeteringWriterConfig) *AsyncMeteringWriter {
	if sink == nil {
		panic("billing: async metering sink is nil")
	}
	cfg = cfg.withDefaults()

	w := &AsyncMeteringWriter{
		ch:           make(chan MeteringRow, cfg.BufferSize),
		sink:         sink,
		batchSize:    cfg.BatchSize,
		flushEvery:   cfg.FlushEvery,
		flushTimeout: cfg.FlushTimeout,
		done:         make(chan struct{}),
	}
	go w.run()
	return w
}

// InsertMeteringRow attempts a non-blocking enqueue. If the buffer is full the
// row is dropped and reconciliation must repair the missing ClickHouse write.
func (w *AsyncMeteringWriter) InsertMeteringRow(ctx context.Context, row MeteringRow) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.closed {
		return ErrAsyncMeteringWriterClosed
	}

	select {
	case w.ch <- row:
		return nil
	default:
		log.Printf(
			"billing: async metering buffer full; dropping row org_id=%s product_id=%s source_type=%s source_ref=%s window_seq=%d",
			row.OrgID,
			row.ProductID,
			row.SourceType,
			row.SourceRef,
			row.WindowSeq,
		)
		return nil
	}
}

// Close stops the writer, flushes any queued rows, and waits for the background
// worker to exit.
func (w *AsyncMeteringWriter) Close(ctx context.Context) error {
	w.mu.Lock()
	if !w.closed {
		w.closed = true
		close(w.ch)
	}
	w.mu.Unlock()

	select {
	case <-w.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (w *AsyncMeteringWriter) run() {
	defer close(w.done)

	ticker := time.NewTicker(w.flushEvery)
	defer ticker.Stop()

	batch := make([]MeteringRow, 0, w.batchSize)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		flushCtx, cancel := context.WithTimeout(context.Background(), w.flushTimeout)
		err := w.sink.InsertMeteringRows(flushCtx, batch)
		cancel()
		if err != nil {
			log.Printf("billing: async metering flush failed rows=%d: %v", len(batch), err)
		}
		batch = batch[:0]
	}

	for {
		select {
		case row, ok := <-w.ch:
			if !ok {
				flush()
				return
			}
			batch = append(batch, row)
			if len(batch) >= w.batchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

func (w *AsyncMeteringWriter) String() string {
	return fmt.Sprintf("AsyncMeteringWriter{buffer=%d,batch=%d}", cap(w.ch), w.batchSize)
}
