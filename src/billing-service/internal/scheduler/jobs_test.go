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

func TestProviderEventApplyArgsInsertOpts(t *testing.T) {
	t.Parallel()

	opts := ProviderEventApplyArgs{}.InsertOpts()
	assertEqual(t, opts.Queue, QueueProvider, "queue")
	assertEqual(t, opts.MaxAttempts, 5, "max attempts")
	if !opts.UniqueOpts.ByArgs {
		t.Fatal("expected provider event apply jobs to be unique by provider event id")
	}
	if !opts.UniqueOpts.ByQueue {
		t.Fatal("expected provider event apply jobs to be unique within the provider queue")
	}
}

func TestCycleAndInvoicePendingArgsUseBillingQueue(t *testing.T) {
	t.Parallel()

	cycleOpts := CycleRolloverPendingArgs{}.InsertOpts()
	assertEqual(t, cycleOpts.Queue, QueueBilling, "cycle queue")
	assertEqual(t, cycleOpts.MaxAttempts, 5, "cycle max attempts")

	invoiceOpts := InvoiceFinalizePendingArgs{}.InsertOpts()
	assertEqual(t, invoiceOpts.Queue, QueueBilling, "invoice queue")
	assertEqual(t, invoiceOpts.MaxAttempts, 5, "invoice max attempts")
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

func TestProviderEventApplyPendingWorkerDelegatesToClient(t *testing.T) {
	t.Parallel()

	client := &fakeBillingWorkClient{providerEventApplyCount: 4}
	worker := &providerEventApplyPendingWorker{client: client, logger: slog.Default()}
	if err := worker.Work(context.Background(), testJob(ProviderEventApplyPendingArgs{Limit: 11})); err != nil {
		t.Fatalf("work: %v", err)
	}
	assertEqual(t, client.providerEventApplyLimit, 11, "provider event apply limit")
}

func TestProviderEventApplyWorkerDelegatesToClient(t *testing.T) {
	t.Parallel()

	client := &fakeBillingWorkClient{providerEventApplied: true}
	worker := &providerEventApplyWorker{client: client, logger: slog.Default()}
	args := ProviderEventApplyArgs{EventID: "provider_event_01"}
	if err := worker.Work(context.Background(), testJob(args)); err != nil {
		t.Fatalf("work: %v", err)
	}
	assertEqual(t, client.providerEventID, args.EventID, "provider event id")
}

func TestCycleRolloverPendingWorkerDelegatesToClient(t *testing.T) {
	t.Parallel()

	client := &fakeBillingWorkClient{cycleRolloverCount: 2}
	worker := &cycleRolloverPendingWorker{client: client, logger: slog.Default()}
	if err := worker.Work(context.Background(), testJob(CycleRolloverPendingArgs{Limit: 12})); err != nil {
		t.Fatalf("work: %v", err)
	}
	assertEqual(t, client.cycleRolloverLimit, 12, "cycle rollover limit")
}

func TestInvoiceFinalizePendingWorkerDelegatesToClient(t *testing.T) {
	t.Parallel()

	client := &fakeBillingWorkClient{invoiceFinalizeCount: 5}
	worker := &invoiceFinalizePendingWorker{client: client, logger: slog.Default()}
	if err := worker.Work(context.Background(), testJob(InvoiceFinalizePendingArgs{Limit: 13})); err != nil {
		t.Fatalf("work: %v", err)
	}
	assertEqual(t, client.invoiceFinalizeLimit, 13, "invoice finalize limit")
}

func testJob[T river.JobArgs](args T) *river.Job[T] {
	return &river.Job[T]{
		JobRow: &rivertype.JobRow{ID: 1},
		Args:   args,
	}
}

type fakeBillingWorkClient struct {
	pendingDeliveryLimit    int
	pendingDeliveryCount    int
	projected               bool
	eventID                 string
	sink                    string
	generation              int
	projectErr              error
	providerEventApplyLimit int
	providerEventApplyCount int
	providerEventApplied    bool
	providerEventID         string
	cycleRolloverLimit      int
	cycleRolloverCount      int
	invoiceFinalizeLimit    int
	invoiceFinalizeCount    int
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

func (f *fakeBillingWorkClient) ApplyPendingProviderEvents(_ context.Context, limit int) (int, error) {
	f.providerEventApplyLimit = limit
	return f.providerEventApplyCount, nil
}

func (f *fakeBillingWorkClient) ApplyProviderEvent(_ context.Context, eventID string) (bool, error) {
	f.providerEventID = eventID
	return f.providerEventApplied, nil
}

func (f *fakeBillingWorkClient) RolloverDueBillingCycles(_ context.Context, limit int) (int, error) {
	f.cycleRolloverLimit = limit
	return f.cycleRolloverCount, nil
}

func (f *fakeBillingWorkClient) FinalizeDueBillingCycles(_ context.Context, limit int) (int, error) {
	f.invoiceFinalizeLimit = limit
	return f.invoiceFinalizeCount, nil
}

func assertEqual[T comparable](t *testing.T, got, want T, label string) {
	t.Helper()
	if got != want {
		t.Fatalf("%s = %v, want %v", label, got, want)
	}
}
