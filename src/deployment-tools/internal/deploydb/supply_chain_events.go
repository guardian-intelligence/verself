package deploydb

import (
	"context"
	"time"
)

const supplyChainPolicyEventsTable = "verself.supply_chain_policy_events"

type SupplyChainPolicyEventRow struct {
	EventAt        time.Time `ch:"event_at"`
	DeployRunKey   string    `ch:"deploy_run_key"`
	Site           string    `ch:"site"`
	SourcePath     string    `ch:"source_path"`
	Line           uint32    `ch:"line"`
	SourceKind     string    `ch:"source_kind"`
	Surface        string    `ch:"surface"`
	Artifact       string    `ch:"artifact"`
	UpstreamURL    string    `ch:"upstream_url"`
	Digest         string    `ch:"digest"`
	PolicyResult   string    `ch:"policy_result"`
	PolicyReason   string    `ch:"policy_reason"`
	AdmissionState string    `ch:"admission_state"`
	TUFTargetPath  string    `ch:"tuf_target_path"`
	StorageURI     string    `ch:"storage_uri"`
	TraceID        string    `ch:"trace_id"`
	SpanID         string    `ch:"span_id"`
	Evidence       string    `ch:"evidence"`
}

func (c *Client) InsertSupplyChainPolicyEvents(ctx context.Context, rows []SupplyChainPolicyEventRow) error {
	return insertStructs(ctx, c, supplyChainPolicyEventsTable, rows)
}
