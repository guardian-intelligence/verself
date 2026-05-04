package deploydb

import (
	"context"
	"fmt"
	"time"

	ch "github.com/ClickHouse/clickhouse-go/v2"
	"go.opentelemetry.io/otel/trace"
)

const (
	DeployExecutorAnsible       = "ansible"
	DeployExecutorNomad         = "nomad"
	DeployExecutorSecurityPatch = "security_patch"
	DeployExecutorSupplyChain   = "supply_chain"

	DeployUnitEventDecided   = "decided"
	DeployUnitEventApplied   = "applied"
	DeployUnitEventSkipped   = "skipped"
	DeployUnitEventSucceeded = "succeeded"
	DeployUnitEventFailed    = "failed"

	deployUnitEventsTable = "verself.deploy_unit_events"
)

// DeployUnitEvent is the append-only evidence row for one deployable unit.
// The deploy runner uses it for unit-level no-op decisions where Nomad is not
// already the source of observed live digests.
type DeployUnitEvent struct {
	RunKey          string
	Site            string
	Executor        string
	UnitID          string
	Kind            string
	DesiredDigest   string
	ObservedDigest  string
	NoOp            bool
	DependencyUnits []string
	PayloadKind     string
	DurationMs      uint32
	ErrorMessage    string
}

type DeployUnitLastSucceeded struct {
	RunKey        string
	DesiredDigest string
	EventAt       time.Time
}

type deployUnitEventRow struct {
	EventAt          time.Time `ch:"event_at"`
	DeployRunKey     string    `ch:"deploy_run_key"`
	Site             string    `ch:"site"`
	Executor         string    `ch:"executor"`
	UnitID           string    `ch:"unit_id"`
	EventKind        string    `ch:"event_kind"`
	DesiredDigest    string    `ch:"desired_digest"`
	ObservedDigest   string    `ch:"observed_digest"`
	NoOp             uint8     `ch:"no_op"`
	DependencyUnitID []string  `ch:"dependency_unit_ids"`
	PayloadKind      string    `ch:"payload_kind"`
	DurationMS       uint32    `ch:"duration_ms"`
	ErrorMessage     string    `ch:"error_message"`
	TraceID          string    `ch:"trace_id"`
	SpanID           string    `ch:"span_id"`
}

func (c *Client) RecordDeployUnitEvent(ctx context.Context, ev DeployUnitEvent) error {
	if err := validateDeployUnitEvent(ev); err != nil {
		return err
	}
	sc := trace.SpanContextFromContext(ctx)
	row := deployUnitEventRow{
		EventAt:          time.Now().UTC(),
		DeployRunKey:     ev.RunKey,
		Site:             ev.Site,
		Executor:         ev.Executor,
		UnitID:           ev.UnitID,
		EventKind:        ev.Kind,
		DesiredDigest:    ev.DesiredDigest,
		ObservedDigest:   ev.ObservedDigest,
		NoOp:             boolByte(ev.NoOp),
		DependencyUnitID: append([]string(nil), ev.DependencyUnits...),
		PayloadKind:      ev.PayloadKind,
		DurationMS:       ev.DurationMs,
		ErrorMessage:     ev.ErrorMessage,
		TraceID:          sc.TraceID().String(),
		SpanID:           sc.SpanID().String(),
	}
	if !sc.IsValid() {
		row.TraceID = ""
		row.SpanID = ""
	}
	return insertStructs(ctx, c, deployUnitEventsTable, []deployUnitEventRow{row})
}

func (c *Client) LastSucceededDeployUnit(ctx context.Context, site, executor, unitID string) (DeployUnitLastSucceeded, bool, error) {
	if site == "" {
		return DeployUnitLastSucceeded{}, false, fmt.Errorf("deploydb: site is required")
	}
	if executor == "" {
		return DeployUnitLastSucceeded{}, false, fmt.Errorf("deploydb: executor is required")
	}
	if unitID == "" {
		return DeployUnitLastSucceeded{}, false, fmt.Errorf("deploydb: unit_id is required")
	}
	exists, err := c.HasDeployUnitEvents(ctx)
	if err != nil {
		return DeployUnitLastSucceeded{}, false, err
	}
	if !exists {
		return DeployUnitLastSucceeded{}, false, nil
	}

	var row DeployUnitLastSucceeded
	var count uint64
	if err := c.conn.QueryRow(ctx, `
SELECT
  argMax(deploy_run_key, event_at) AS deploy_run_key,
  argMax(desired_digest, event_at) AS desired_digest,
  max(event_at) AS last_event_at,
  count() AS rows
FROM verself.deploy_unit_events
WHERE site = {site:String}
  AND executor = {executor:String}
  AND unit_id = {unit_id:String}
  AND event_kind = 'succeeded'
`, ch.Named("site", site), ch.Named("executor", executor), ch.Named("unit_id", unitID)).Scan(
		&row.RunKey,
		&row.DesiredDigest,
		&row.EventAt,
		&count,
	); err != nil {
		return DeployUnitLastSucceeded{}, false, fmt.Errorf("deploydb: query last succeeded deploy unit: %w", err)
	}
	if count == 0 {
		return DeployUnitLastSucceeded{}, false, nil
	}
	return row, true, nil
}

func (c *Client) HasDeployUnitEvents(ctx context.Context) (bool, error) {
	return c.tableExists(ctx, "verself", "deploy_unit_events")
}

func (c *Client) tableExists(ctx context.Context, database, table string) (bool, error) {
	var count uint64
	if err := c.conn.QueryRow(ctx, `
SELECT count()
FROM system.tables
WHERE database = {database:String}
  AND name = {table:String}
`, ch.Named("database", database), ch.Named("table", table)).Scan(&count); err != nil {
		return false, fmt.Errorf("deploydb: query table existence: %w", err)
	}
	return count > 0, nil
}

func validateDeployUnitEvent(ev DeployUnitEvent) error {
	if ev.RunKey == "" {
		return fmt.Errorf("deploydb: DeployUnitEvent.RunKey is required")
	}
	if ev.Site == "" {
		return fmt.Errorf("deploydb: DeployUnitEvent.Site is required")
	}
	switch ev.Executor {
	case DeployExecutorAnsible,
		DeployExecutorNomad,
		DeployExecutorSecurityPatch,
		DeployExecutorSupplyChain:
	default:
		return fmt.Errorf("deploydb: unsupported deploy unit executor %q", ev.Executor)
	}
	if ev.UnitID == "" {
		return fmt.Errorf("deploydb: DeployUnitEvent.UnitID is required")
	}
	switch ev.Kind {
	case DeployUnitEventDecided,
		DeployUnitEventApplied,
		DeployUnitEventSkipped,
		DeployUnitEventSucceeded,
		DeployUnitEventFailed:
	default:
		return fmt.Errorf("deploydb: unsupported deploy unit event kind %q", ev.Kind)
	}
	return nil
}
