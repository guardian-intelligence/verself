package jobs

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
)

var ErrRunLogCursorInvalid = errors.New("sandbox-rental: run log cursor invalid")

type RunLogSearchFilters struct {
	Limit       int
	Cursor      string
	Query       string
	ExecutionID uuid.UUID
	AttemptID   uuid.UUID
	SourceKind  string
	Repository  string
	Workflow    string
	Branch      string
	RunnerClass string
}

type RunLogSearchResult struct {
	ExecutionID        uuid.UUID
	AttemptID          uuid.UUID
	SourceKind         string
	WorkloadKind       string
	RunnerClass        string
	RepositoryFullName string
	WorkflowName       string
	JobName            string
	HeadBranch         string
	ScheduleID         string
	Seq                uint32
	Stream             string
	Chunk              string
	CreatedAt          time.Time
}

type RunLogSearchPage struct {
	Results    []RunLogSearchResult
	NextCursor string
	Limit      int
}

type runLogCursor struct {
	CreatedAt time.Time
	AttemptID uuid.UUID
	Seq       uint32
}

func makeRunLogCursor(createdAt time.Time, attemptID uuid.UUID, seq uint32) string {
	return fmt.Sprintf("%s.%s.%d", createdAt.UTC().Format(time.RFC3339Nano), attemptID.String(), seq)
}

func parseRunLogCursor(value string) (runLogCursor, error) {
	lastDot := strings.LastIndex(value, ".")
	if lastDot <= 0 || lastDot == len(value)-1 {
		return runLogCursor{}, ErrRunLogCursorInvalid
	}
	seq, err := parseUint32(value[lastDot+1:])
	if err != nil {
		return runLogCursor{}, ErrRunLogCursorInvalid
	}
	prefix := value[:lastDot]
	secondDot := strings.LastIndex(prefix, ".")
	if secondDot <= 0 || secondDot == len(prefix)-1 {
		return runLogCursor{}, ErrRunLogCursorInvalid
	}
	createdAt, err := time.Parse(time.RFC3339Nano, prefix[:secondDot])
	if err != nil {
		return runLogCursor{}, ErrRunLogCursorInvalid
	}
	attemptID, err := uuid.Parse(prefix[secondDot+1:])
	if err != nil {
		return runLogCursor{}, ErrRunLogCursorInvalid
	}
	return runLogCursor{CreatedAt: createdAt.UTC(), AttemptID: attemptID, Seq: seq}, nil
}

func parseUint32(value string) (uint32, error) {
	parsed, err := strconv.ParseUint(value, 10, 32)
	if err != nil {
		return 0, err
	}
	return uint32(parsed), nil
}

func (s *Service) SearchRunLogs(ctx context.Context, orgID uint64, filters RunLogSearchFilters) (RunLogSearchPage, error) {
	ctx, span := tracer.Start(ctx, "sandbox-rental.logs.search")
	defer span.End()
	if s.CH == nil {
		return RunLogSearchPage{}, fmt.Errorf("clickhouse is not configured")
	}

	limit := filters.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	cursor := runLogCursor{CreatedAt: time.Unix(0, 0).UTC(), AttemptID: uuid.Nil, Seq: 0}
	cursorEnabled := uint8(0)
	if strings.TrimSpace(filters.Cursor) != "" {
		parsed, err := parseRunLogCursor(filters.Cursor)
		if err != nil {
			return RunLogSearchPage{}, err
		}
		cursor = parsed
		cursorEnabled = 1
	}

	executionFilter := filters.ExecutionID.String()
	attemptFilter := filters.AttemptID.String()
	cursorAttemptID := cursor.AttemptID.String()

	rows, err := s.CH.Query(ctx, `
		SELECT
			execution_id, attempt_id, org_id, source_kind, workload_kind, runner_class, external_provider,
			product_id, correlation_id, repository_full_name, workflow_name, job_name, head_branch,
			schedule_id, seq, stream, chunk, created_at
		FROM forge_metal.job_logs
		WHERE org_id = $1
		  AND ($2 = '' OR source_kind = $2)
		  AND ($3 = '' OR repository_full_name = $3)
		  AND ($4 = '' OR workflow_name = $4)
		  AND ($5 = '' OR head_branch = $5)
		  AND ($6 = '' OR runner_class = $6)
		  AND ($7 = '00000000-0000-0000-0000-000000000000' OR execution_id = toUUID($7))
		  AND ($8 = '00000000-0000-0000-0000-000000000000' OR attempt_id = toUUID($8))
		  AND ($9 = '' OR positionCaseInsensitiveUTF8(chunk, $9) > 0)
		  AND ($10 = 0 OR (created_at, attempt_id, seq) < ($11, toUUID($12), $13))
		ORDER BY created_at DESC, attempt_id DESC, seq DESC
		LIMIT $14
	`, orgID, strings.TrimSpace(filters.SourceKind), strings.TrimSpace(filters.Repository), strings.TrimSpace(filters.Workflow),
		strings.TrimSpace(filters.Branch), strings.TrimSpace(filters.RunnerClass), executionFilter, attemptFilter,
		strings.TrimSpace(filters.Query), cursorEnabled, cursor.CreatedAt, cursorAttemptID, cursor.Seq, limit+1)
	if err != nil {
		return RunLogSearchPage{}, fmt.Errorf("search run logs: %w", err)
	}
	defer rows.Close()

	results := make([]RunLogSearchResult, 0, limit)
	for rows.Next() {
		var row jobLogRow
		if err := rows.ScanStruct(&row); err != nil {
			return RunLogSearchPage{}, fmt.Errorf("scan run log search result: %w", err)
		}
		results = append(results, RunLogSearchResult{
			ExecutionID:        row.ExecutionID,
			AttemptID:          row.AttemptID,
			SourceKind:         row.SourceKind,
			WorkloadKind:       row.WorkloadKind,
			RunnerClass:        row.RunnerClass,
			RepositoryFullName: row.RepositoryFullName,
			WorkflowName:       row.WorkflowName,
			JobName:            row.JobName,
			HeadBranch:         row.HeadBranch,
			ScheduleID:         row.ScheduleID,
			Seq:                row.Seq,
			Stream:             row.Stream,
			Chunk:              row.Chunk,
			CreatedAt:          row.CreatedAt,
		})
	}
	if err := rows.Err(); err != nil {
		return RunLogSearchPage{}, fmt.Errorf("iterate run log search results: %w", err)
	}

	nextCursor := ""
	if len(results) > limit {
		last := results[limit-1]
		nextCursor = makeRunLogCursor(last.CreatedAt, last.AttemptID, last.Seq)
		results = results[:limit]
	}
	span.SetAttributes(traceOrgID(orgID), attribute.Int("sandbox.run_log_result_count", len(results)))
	return RunLogSearchPage{Results: results, NextCursor: nextCursor, Limit: limit}, nil
}
