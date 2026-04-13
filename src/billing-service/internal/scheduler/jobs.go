package scheduler

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
)

type MeteringProjectPendingRequest struct {
	Limit       int
	TraceParent string
}

type MeteringProjectPendingArgs struct {
	Limit       int    `json:"limit"`
	TraceParent string `json:"trace_parent,omitempty"`
	SubmittedAt string `json:"submitted_at"`
}

func (MeteringProjectPendingArgs) Kind() string { return KindMeteringProjectPending }

func (MeteringProjectPendingArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: 5, Queue: QueueMetering, Tags: []string{"billing-metering"}}
}

type OutboxProjectPendingRequest struct {
	Limit       int
	TraceParent string
}

type OutboxProjectPendingArgs struct {
	Limit       int    `json:"limit"`
	TraceParent string `json:"trace_parent,omitempty"`
	SubmittedAt string `json:"submitted_at"`
}

func (OutboxProjectPendingArgs) Kind() string { return KindOutboxProjectPending }

func (OutboxProjectPendingArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: 5, Queue: QueueOutbox, Tags: []string{"billing-outbox"}}
}

type EntitlementsReconcileRequest struct {
	Limit       int
	TraceParent string
}

type EntitlementsReconcileArgs struct {
	Limit       int    `json:"limit"`
	TraceParent string `json:"trace_parent,omitempty"`
	SubmittedAt string `json:"submitted_at"`
}

func (EntitlementsReconcileArgs) Kind() string { return KindEntitlementsReconcile }

func (EntitlementsReconcileArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: 5, Queue: QueueReconcile, Tags: []string{"billing-entitlement-reconcile"}}
}

func (r *Runtime) EnqueueMeteringProjectPendingTx(ctx context.Context, tx pgx.Tx, req MeteringProjectPendingRequest) (JobResult, error) {
	return enqueueTx(ctx, r.client, tx, MeteringProjectPendingArgs{
		Limit:       req.Limit,
		TraceParent: strings.TrimSpace(req.TraceParent),
		SubmittedAt: newSubmittedAt(),
	})
}

func (r *Runtime) EnqueueOutboxProjectPendingTx(ctx context.Context, tx pgx.Tx, req OutboxProjectPendingRequest) (JobResult, error) {
	return enqueueTx(ctx, r.client, tx, OutboxProjectPendingArgs{
		Limit:       req.Limit,
		TraceParent: strings.TrimSpace(req.TraceParent),
		SubmittedAt: newSubmittedAt(),
	})
}

func (r *Runtime) EnqueueEntitlementsReconcileTx(ctx context.Context, tx pgx.Tx, req EntitlementsReconcileRequest) (JobResult, error) {
	return enqueueTx(ctx, r.client, tx, EntitlementsReconcileArgs{
		Limit:       req.Limit,
		TraceParent: strings.TrimSpace(req.TraceParent),
		SubmittedAt: newSubmittedAt(),
	})
}
