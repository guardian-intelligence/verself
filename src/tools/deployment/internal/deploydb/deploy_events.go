package deploydb

import (
	"context"
	"fmt"
	"regexp"
	"time"

	ch "github.com/ClickHouse/clickhouse-go/v2"
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
	EventAt            time.Time
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

// LastSucceededDeploy returns the most recent successful deploy for a
// site/scope pair. The boolean is false when the site has not completed a
// deploy through this scope yet.
type LastSucceededDeploy struct {
	RunKey  string
	Sha     string
	EventAt time.Time
}

func (c *Client) RecordDeployEvent(ctx context.Context, ev DeployEvent) error {
	if err := validateDeployEvent(ev); err != nil {
		return err
	}
	eventAt := ev.EventAt
	if eventAt.IsZero() {
		eventAt = time.Now().UTC()
	} else {
		eventAt = eventAt.UTC()
	}
	row := deployEventRow{
		EventAt:            eventAt,
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

func (c *Client) LastSucceededDeploy(ctx context.Context, site, scope string) (LastSucceededDeploy, bool, error) {
	if site == "" {
		return LastSucceededDeploy{}, false, fmt.Errorf("deploydb: site is required")
	}
	if scope == "" {
		return LastSucceededDeploy{}, false, fmt.Errorf("deploydb: scope is required")
	}
	var row LastSucceededDeploy
	var count uint64
	if err := c.conn.QueryRow(ctx, `
SELECT
  argMax(deploy_run_key, event_at) AS deploy_run_key,
  argMax(sha, event_at) AS sha,
  max(event_at) AS last_event_at,
  count() AS rows
FROM verself.deploy_events
WHERE site = {site:String}
  AND scope = {scope:String}
  AND event_kind = 'succeeded'
`, ch.Named("site", site), ch.Named("scope", scope)).Scan(
		&row.RunKey,
		&row.Sha,
		&row.EventAt,
		&count,
	); err != nil {
		return LastSucceededDeploy{}, false, fmt.Errorf("deploydb: query last succeeded deploy: %w", err)
	}
	if count == 0 {
		return LastSucceededDeploy{}, false, nil
	}
	return row, true, nil
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
