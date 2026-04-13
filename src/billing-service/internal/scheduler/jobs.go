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

type EventDeliveryProjectPendingRequest struct {
	Limit       int
	TraceParent string
}

type EventDeliveryProjectPendingArgs struct {
	Limit       int    `json:"limit"`
	TraceParent string `json:"trace_parent,omitempty"`
	SubmittedAt string `json:"submitted_at"`
}

func (EventDeliveryProjectPendingArgs) Kind() string { return KindEventDeliveryProjectPending }

func (EventDeliveryProjectPendingArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: 5, Queue: QueueEventDelivery, Tags: []string{"billing-event-delivery"}}
}

type EventDeliveryProjectRequest struct {
	EventID     string
	Sink        string
	Generation  int
	TraceParent string
}

type EventDeliveryProjectArgs struct {
	EventID     string `json:"event_id" river:"unique"`
	Sink        string `json:"sink" river:"unique"`
	Generation  int    `json:"generation" river:"unique"`
	TraceParent string `json:"trace_parent,omitempty"`
	SubmittedAt string `json:"submitted_at"`
}

func (EventDeliveryProjectArgs) Kind() string { return KindEventDeliveryProject }

func (EventDeliveryProjectArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		MaxAttempts: 5,
		Queue:       QueueEventDelivery,
		Tags:        []string{"billing-event-delivery"},
		UniqueOpts: river.UniqueOpts{
			ByArgs:  true,
			ByQueue: true,
		},
	}
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

func (r *Runtime) EnqueueEventDeliveryProjectPendingTx(ctx context.Context, tx pgx.Tx, req EventDeliveryProjectPendingRequest) (JobResult, error) {
	return enqueueTx(ctx, r.client, tx, EventDeliveryProjectPendingArgs{
		Limit:       req.Limit,
		TraceParent: strings.TrimSpace(req.TraceParent),
		SubmittedAt: newSubmittedAt(),
	})
}

func (r *Runtime) EnqueueEventDeliveryProjectTx(ctx context.Context, tx pgx.Tx, req EventDeliveryProjectRequest) (JobResult, error) {
	return enqueueTx(ctx, r.client, tx, EventDeliveryProjectArgs{
		EventID:     strings.TrimSpace(req.EventID),
		Sink:        strings.TrimSpace(req.Sink),
		Generation:  req.Generation,
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
