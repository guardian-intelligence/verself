package deploydb

import (
	"context"
	"time"
)

const ansibleTaskEventsTable = "verself.ansible_task_events"

// AnsibleTaskEventRow is the ClickHouse row shape for parsed
// ansible-playbook task output. The ch tags are load-bearing:
// clickhouse-go maps AppendStruct by these names rather than by field
// order.
type AnsibleTaskEventRow struct {
	EventAt      time.Time `ch:"event_at"`
	DeployRunKey string    `ch:"deploy_run_key"`
	Site         string    `ch:"site"`
	Layer        string    `ch:"layer"`
	Playbook     string    `ch:"playbook"`
	Play         string    `ch:"play"`
	Task         string    `ch:"task"`
	Host         string    `ch:"host"`
	Status       string    `ch:"status"`
	Item         string    `ch:"item"`
	DurationMS   uint32    `ch:"duration_ms"`
	Message      string    `ch:"message"`
}

func (c *Client) InsertAnsibleTaskEvents(ctx context.Context, rows []AnsibleTaskEventRow) error {
	return insertStructs(ctx, c, ansibleTaskEventsTable, rows)
}
