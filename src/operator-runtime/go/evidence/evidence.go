package evidence

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	opch "github.com/verself/operator-runtime/clickhouse"
	opruntime "github.com/verself/operator-runtime/runtime"
)

const operatorCommandRunsTable = "verself.operator_command_runs"

type Recorder struct {
	ClickHouse *opch.Client
}

type CommandRun struct {
	Timestamp    time.Time `ch:"event_at"`
	RunID        string    `ch:"run_id"`
	Site         string    `ch:"site"`
	Command      string    `ch:"command"`
	ActorDevice  string    `ch:"actor_device"`
	TargetHost   string    `ch:"target_host"`
	TargetUser   string    `ch:"target_user"`
	Status       string    `ch:"status"`
	DurationMS   uint32    `ch:"duration_ms"`
	ErrorKind    string    `ch:"error_kind"`
	ErrorMessage string    `ch:"error_message"`
	TraceID      string    `ch:"trace_id"`
}

func (r Recorder) RecordCommandRun(ctx context.Context, rt *opruntime.Runtime, started time.Time, command string, runErr error) error {
	if r.ClickHouse == nil || r.ClickHouse.Conn == nil {
		return fmt.Errorf("operator evidence: ClickHouse client is required")
	}
	if rt == nil {
		return fmt.Errorf("operator evidence: runtime is required")
	}
	status := "succeeded"
	errorKind := ""
	errorMessage := ""
	if runErr != nil {
		status = "failed"
		errorKind = fmt.Sprintf("%T", runErr)
		errorMessage = runErr.Error()
	}
	if command == "" {
		command = rt.Command
	}
	if command == "" {
		return fmt.Errorf("operator evidence: command is required")
	}
	duration := time.Since(started)
	if duration < 0 {
		duration = 0
	}
	row := CommandRun{
		Timestamp:    time.Now().UTC(),
		RunID:        uuid.NewString(),
		Site:         rt.Site,
		Command:      command,
		ActorDevice:  rt.Device,
		TargetHost:   rt.Target.Host,
		TargetUser:   rt.Target.User,
		Status:       status,
		DurationMS:   uint32(duration.Milliseconds()),
		ErrorKind:    errorKind,
		ErrorMessage: errorMessage,
		TraceID:      rt.TraceID(),
	}
	batch, err := r.ClickHouse.Conn.PrepareBatch(ctx, "INSERT INTO "+operatorCommandRunsTable)
	if err != nil {
		return fmt.Errorf("operator evidence: prepare insert: %w", err)
	}
	if err := batch.AppendStruct(&row); err != nil {
		_ = batch.Abort()
		return fmt.Errorf("operator evidence: append command run: %w", err)
	}
	if err := batch.Send(); err != nil {
		return fmt.Errorf("operator evidence: send command run: %w", err)
	}
	return nil
}
