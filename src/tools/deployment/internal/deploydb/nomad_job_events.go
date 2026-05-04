package deploydb

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/trace"
)

const (
	NomadJobEventDecided             = "decided"
	NomadJobEventSubmitted           = "submitted"
	NomadJobEventDeploymentSucceeded = "deployment_succeeded"
	NomadJobEventDeploymentFailed    = "deployment_failed"
	NomadJobEventSubmitFailed        = "submit_failed"

	nomadJobEventsTable = "verself.nomad_job_events"
)

// NomadJobEvent is the typed projection inserted into
// verself.nomad_job_events. Rows are append-only so one job submission writes
// each meaningful state transition rather than updating a mutable status row.
type NomadJobEvent struct {
	RunKey              string
	Site                string
	JobID               string
	Kind                string
	SpecSHA256          string
	ArtifactSHA256      string
	PriorJobModifyIndex uint64
	PriorVersion        uint64
	PriorStopped        bool
	NoOp                bool
	EvalID              string
	DeploymentID        string
	JobModifyIndex      uint64
	DesiredTotal        uint16
	HealthyTotal        uint16
	UnhealthyTotal      uint16
	PlacedTotal         uint16
	TerminalStatus      string
	DurationMs          uint32
	ErrorMessage        string
}

type nomadJobEventRow struct {
	EventAt             time.Time `ch:"event_at"`
	DeployRunKey        string    `ch:"deploy_run_key"`
	Site                string    `ch:"site"`
	JobID               string    `ch:"job_id"`
	EventKind           string    `ch:"event_kind"`
	SpecSHA256          string    `ch:"spec_sha256"`
	ArtifactSHA256      string    `ch:"artifact_sha256"`
	PriorJobModifyIndex uint64    `ch:"prior_job_modify_index"`
	PriorVersion        uint64    `ch:"prior_version"`
	PriorStopped        uint8     `ch:"prior_stopped"`
	NoOp                uint8     `ch:"no_op"`
	EvalID              string    `ch:"eval_id"`
	DeploymentID        string    `ch:"deployment_id"`
	JobModifyIndex      uint64    `ch:"job_modify_index"`
	DesiredTotal        uint16    `ch:"desired_total"`
	HealthyTotal        uint16    `ch:"healthy_total"`
	UnhealthyTotal      uint16    `ch:"unhealthy_total"`
	PlacedTotal         uint16    `ch:"placed_total"`
	TerminalStatus      string    `ch:"terminal_status"`
	DurationMS          uint32    `ch:"duration_ms"`
	ErrorMessage        string    `ch:"error_message"`
	TraceID             string    `ch:"trace_id"`
	SpanID              string    `ch:"span_id"`
}

func (c *Client) RecordNomadJobEvent(ctx context.Context, ev NomadJobEvent) error {
	if err := validateNomadJobEvent(ev); err != nil {
		return err
	}
	sc := trace.SpanContextFromContext(ctx)
	row := nomadJobEventRow{
		EventAt:             time.Now().UTC(),
		DeployRunKey:        ev.RunKey,
		Site:                ev.Site,
		JobID:               ev.JobID,
		EventKind:           ev.Kind,
		SpecSHA256:          ev.SpecSHA256,
		ArtifactSHA256:      ev.ArtifactSHA256,
		PriorJobModifyIndex: ev.PriorJobModifyIndex,
		PriorVersion:        ev.PriorVersion,
		PriorStopped:        boolByte(ev.PriorStopped),
		NoOp:                boolByte(ev.NoOp),
		EvalID:              ev.EvalID,
		DeploymentID:        ev.DeploymentID,
		JobModifyIndex:      ev.JobModifyIndex,
		DesiredTotal:        ev.DesiredTotal,
		HealthyTotal:        ev.HealthyTotal,
		UnhealthyTotal:      ev.UnhealthyTotal,
		PlacedTotal:         ev.PlacedTotal,
		TerminalStatus:      ev.TerminalStatus,
		DurationMS:          ev.DurationMs,
		ErrorMessage:        ev.ErrorMessage,
		TraceID:             sc.TraceID().String(),
		SpanID:              sc.SpanID().String(),
	}
	if !sc.IsValid() {
		row.TraceID = ""
		row.SpanID = ""
	}
	return insertStructs(ctx, c, nomadJobEventsTable, []nomadJobEventRow{row})
}

func validateNomadJobEvent(ev NomadJobEvent) error {
	if ev.RunKey == "" {
		return fmt.Errorf("deploydb: NomadJobEvent.RunKey is required")
	}
	if ev.Site == "" {
		return fmt.Errorf("deploydb: NomadJobEvent.Site is required")
	}
	if ev.JobID == "" {
		return fmt.Errorf("deploydb: NomadJobEvent.JobID is required")
	}
	switch ev.Kind {
	case NomadJobEventDecided,
		NomadJobEventSubmitted,
		NomadJobEventDeploymentSucceeded,
		NomadJobEventDeploymentFailed,
		NomadJobEventSubmitFailed:
	default:
		return fmt.Errorf("deploydb: unsupported Nomad job event kind %q", ev.Kind)
	}
	return nil
}

func boolByte(value bool) uint8 {
	if value {
		return 1
	}
	return 0
}
