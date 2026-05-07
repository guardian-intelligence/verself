package deploydb

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/trace"
)

const (
	DeployWaveEventStarted   = "started"
	DeployWaveEventSucceeded = "succeeded"
	DeployWaveEventFailed    = "failed"

	deployWaveEventsTable = "verself.deploy_wave_events"
)

type DeployWaveEvent struct {
	RunKey          string
	Site            string
	Wave            string
	Kind            string
	JobCount        uint16
	ChangedJobCount uint16
	ArtifactCount   uint16
	DurationMs      uint32
	ErrorMessage    string
}

type deployWaveEventRow struct {
	EventAt         time.Time `ch:"event_at"`
	DeployRunKey    string    `ch:"deploy_run_key"`
	Site            string    `ch:"site"`
	Wave            string    `ch:"wave"`
	EventKind       string    `ch:"event_kind"`
	JobCount        uint16    `ch:"job_count"`
	ChangedJobCount uint16    `ch:"changed_job_count"`
	ArtifactCount   uint16    `ch:"artifact_count"`
	DurationMS      uint32    `ch:"duration_ms"`
	ErrorMessage    string    `ch:"error_message"`
	TraceID         string    `ch:"trace_id"`
	SpanID          string    `ch:"span_id"`
}

func (c *Client) RecordDeployWaveEvent(ctx context.Context, ev DeployWaveEvent) error {
	if err := validateDeployWaveEvent(ev); err != nil {
		return err
	}
	sc := trace.SpanContextFromContext(ctx)
	row := deployWaveEventRow{
		EventAt:         time.Now().UTC(),
		DeployRunKey:    ev.RunKey,
		Site:            ev.Site,
		Wave:            ev.Wave,
		EventKind:       ev.Kind,
		JobCount:        ev.JobCount,
		ChangedJobCount: ev.ChangedJobCount,
		ArtifactCount:   ev.ArtifactCount,
		DurationMS:      ev.DurationMs,
		ErrorMessage:    ev.ErrorMessage,
		TraceID:         sc.TraceID().String(),
		SpanID:          sc.SpanID().String(),
	}
	if !sc.IsValid() {
		row.TraceID = ""
		row.SpanID = ""
	}
	return insertStructs(ctx, c, deployWaveEventsTable, []deployWaveEventRow{row})
}

func validateDeployWaveEvent(ev DeployWaveEvent) error {
	if ev.RunKey == "" {
		return fmt.Errorf("deploydb: DeployWaveEvent.RunKey is required")
	}
	if ev.Site == "" {
		return fmt.Errorf("deploydb: DeployWaveEvent.Site is required")
	}
	if !validDeployWave(ev.Wave) {
		return fmt.Errorf("deploydb: unsupported deploy wave %q", ev.Wave)
	}
	switch ev.Kind {
	case DeployWaveEventStarted, DeployWaveEventSucceeded, DeployWaveEventFailed:
	default:
		return fmt.Errorf("deploydb: unsupported deploy wave event kind %q", ev.Kind)
	}
	return nil
}

func validDeployWave(wave string) bool {
	return wave == "pre_artifact" || wave == "artifact"
}
