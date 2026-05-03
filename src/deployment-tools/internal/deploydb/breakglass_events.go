package deploydb

import (
	"context"
	"fmt"
	"time"
)

const breakglassEventsTable = "verself.breakglass_events"

type BreakglassEventRow struct {
	EventAt           time.Time `ch:"event_at"`
	DeployRunKey      string    `ch:"deploy_run_key"`
	Site              string    `ch:"site"`
	Sha               string    `ch:"sha"`
	Actor             string    `ch:"actor"`
	ExceptionID       string    `ch:"exception_id"`
	ExpiresAt         time.Time `ch:"expires_at"`
	Reason            string    `ch:"reason"`
	AllowedResults    []string  `ch:"allowed_results"`
	PolicyRejected    uint32    `ch:"policy_rejected"`
	PolicyProvisional uint32    `ch:"policy_provisional"`
	TraceID           string    `ch:"trace_id"`
	SpanID            string    `ch:"span_id"`
	Evidence          string    `ch:"evidence"`
}

func (c *Client) InsertBreakglassEvents(ctx context.Context, rows []BreakglassEventRow) error {
	for _, row := range rows {
		if row.DeployRunKey == "" || row.Site == "" || row.ExceptionID == "" {
			return fmt.Errorf("deploydb: incomplete breakglass event identity")
		}
		if !sha40.MatchString(row.Sha) {
			return fmt.Errorf("deploydb: BreakglassEventRow.Sha must be 40 lowercase hex characters: %q", row.Sha)
		}
	}
	return insertStructs(ctx, c, breakglassEventsTable, rows)
}
