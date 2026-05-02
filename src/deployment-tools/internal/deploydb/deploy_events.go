package deploydb

import (
	"context"
	"fmt"
	"regexp"
	"time"
)

const (
	EventStarted   = "started"
	EventSucceeded = "succeeded"
	EventFailed    = "failed"

	deployEventsTable = "verself.deploy_events"
)

var sha40 = regexp.MustCompile(`^[0-9a-f]{40}$`)

// DeployEvent is the typed projection inserted into
// verself.deploy_events. ClickHouse rows are append-only; callers write
// one lifecycle event per state transition.
type DeployEvent struct {
	RunKey             string
	Site               string
	Sha                string
	Actor              string
	Scope              string
	AffectedComponents []string
	Kind               string
	DurationMs         uint32
	ErrorMessage       string
}

type deployEventRow struct {
	EventAt            time.Time `ch:"event_at"`
	DeployRunKey       string    `ch:"deploy_run_key"`
	Site               string    `ch:"site"`
	Sha                string    `ch:"sha"`
	Actor              string    `ch:"actor"`
	Scope              string    `ch:"scope"`
	AffectedComponents []string  `ch:"affected_components"`
	EventKind          string    `ch:"event_kind"`
	DurationMS         uint32    `ch:"duration_ms"`
	ErrorMessage       string    `ch:"error_message"`
}

func (c *Client) RecordDeployEvent(ctx context.Context, ev DeployEvent) error {
	if err := validateDeployEvent(ev); err != nil {
		return err
	}
	row := deployEventRow{
		EventAt:            time.Now().UTC(),
		DeployRunKey:       ev.RunKey,
		Site:               ev.Site,
		Sha:                ev.Sha,
		Actor:              ev.Actor,
		Scope:              ev.Scope,
		AffectedComponents: ev.AffectedComponents,
		EventKind:          ev.Kind,
		DurationMS:         ev.DurationMs,
		ErrorMessage:       ev.ErrorMessage,
	}
	return insertStructs(ctx, c, deployEventsTable, []deployEventRow{row})
}

func validateDeployEvent(ev DeployEvent) error {
	if ev.RunKey == "" {
		return fmt.Errorf("deploydb: DeployEvent.RunKey is required")
	}
	if ev.Site == "" {
		return fmt.Errorf("deploydb: DeployEvent.Site is required")
	}
	if !sha40.MatchString(ev.Sha) {
		return fmt.Errorf("deploydb: DeployEvent.Sha must be 40 lowercase hex characters: %q", ev.Sha)
	}
	if ev.Scope == "" {
		return fmt.Errorf("deploydb: DeployEvent.Scope is required")
	}
	switch ev.Kind {
	case EventStarted, EventSucceeded, EventFailed:
	default:
		return fmt.Errorf("deploydb: unsupported deploy event kind %q", ev.Kind)
	}
	return nil
}
