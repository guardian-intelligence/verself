package scheduler

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

func TestEventDeliveryProjectArgsInsertOpts(t *testing.T) {
	t.Parallel()

	opts := EventDeliveryProjectArgs{}.InsertOpts()
	assertEqual(t, opts.Queue, QueueEventDelivery, "queue")
	assertEqual(t, opts.MaxAttempts, 5, "max attempts")
	if !opts.UniqueOpts.ByArgs {
		t.Fatal("expected event delivery project jobs to be unique by event/sink/generation args")
	}
	if !opts.UniqueOpts.ByQueue {
		t.Fatal("expected event delivery project jobs to be unique within the delivery queue")
	}
}

func TestEventDeliveryProjectPendingWorkerDelegatesToClient(t *testing.T) {
	t.Parallel()

	client := &fakeBillingWorkClient{pendingDeliveryCount: 3}
	worker := &eventDeliveryProjectPendingWorker{client: client, logger: slog.Default()}
	if err := worker.Work(context.Background(), testJob(EventDeliveryProjectPendingArgs{Limit: 7})); err != nil {
		t.Fatalf("work: %v", err)
	}
	assertEqual(t, client.pendingDeliveryLimit, 7, "pending delivery limit")
}

func TestEventDeliveryProjectWorkerDelegatesToClient(t *testing.T) {
	t.Parallel()

	client := &fakeBillingWorkClient{projected: true}
	worker := &eventDeliveryProjectWorker{client: client, logger: slog.Default()}
	args := EventDeliveryProjectArgs{EventID: "evt_01", Sink: "clickhouse_billing_events", Generation: 2}
	if err := worker.Work(context.Background(), testJob(args)); err != nil {
		t.Fatalf("work: %v", err)
	}
	assertEqual(t, client.eventID, args.EventID, "event id")
	assertEqual(t, client.sink, args.Sink, "sink")
	assertEqual(t, client.generation, args.Generation, "generation")
}

func TestEventDeliveryProjectWorkerReturnsClientError(t *testing.T) {
	t.Parallel()

	boom := errors.New("boom")
	client := &fakeBillingWorkClient{projectErr: boom}
	worker := &eventDeliveryProjectWorker{client: client, logger: slog.Default()}
	err := worker.Work(context.Background(), testJob(EventDeliveryProjectArgs{EventID: "evt_01", Sink: "clickhouse_billing_events", Generation: 1}))
	if !errors.Is(err, boom) {
		t.Fatalf("error = %v, want %v", err, boom)
	}
}

func testJob[T river.JobArgs](args T) *river.Job[T] {
	return &river.Job[T]{
		JobRow: &rivertype.JobRow{ID: 1},
		Args:   args,
	}
}

type fakeBillingWorkClient struct {
	pendingDeliveryLimit int
	pendingDeliveryCount int
	projected            bool
	eventID              string
	sink                 string
	generation           int
	projectErr           error
}

func (f *fakeBillingWorkClient) ProjectPendingWindows(context.Context, int) (int, error) {
	return 0, nil
}

func (f *fakeBillingWorkClient) ProjectPendingBillingEventDeliveries(_ context.Context, limit int) (int, error) {
	f.pendingDeliveryLimit = limit
	return f.pendingDeliveryCount, nil
}

func (f *fakeBillingWorkClient) ProjectBillingEventDelivery(_ context.Context, eventID string, sink string, generation int) (bool, error) {
	f.eventID = eventID
	f.sink = sink
	f.generation = generation
	return f.projected, f.projectErr
}

func (f *fakeBillingWorkClient) ReconcileEntitlements(context.Context, int) (int, error) {
	return 0, nil
}

func assertEqual[T comparable](t *testing.T, got, want T, label string) {
	t.Helper()
	if got != want {
		t.Fatalf("%s = %v, want %v", label, got, want)
	}
}
