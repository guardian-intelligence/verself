package recurring

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/verself/sandbox-rental-service/internal/store"
)

func (s *Service) storeQueries() *store.Queries {
	return store.New(s.pgx)
}

func dbOrgID(orgID uint64) int64 {
	return int64(orgID)
}

func orgIDFromDB(orgID int64) uint64 {
	if orgID <= 0 {
		return 0
	}
	return uint64(orgID)
}

func pgTime(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value.UTC(), Valid: true}
}

func timeFromPG(value pgtype.Timestamptz) time.Time {
	if !value.Valid {
		return time.Time{}
	}
	return value.Time.UTC()
}

func timePtrFromPG(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	t := value.Time.UTC()
	return &t
}

func scheduleRecordFromStore(row store.ExecutionSchedule) (ScheduleRecord, error) {
	record := ScheduleRecord{
		ScheduleID:         row.ScheduleID,
		OrgID:              orgIDFromDB(row.OrgID),
		ActorID:            row.ActorID,
		DisplayName:        row.DisplayName,
		IdempotencyKey:     row.IdempotencyKey,
		TemporalScheduleID: row.TemporalScheduleID,
		TemporalNamespace:  row.TemporalNamespace,
		TaskQueue:          row.TaskQueue,
		State:              row.State,
		IntervalSeconds:    uint32(row.IntervalSeconds),
		ProjectID:          row.ProjectID,
		SourceRepositoryID: row.SourceRepositoryID,
		WorkflowPath:       row.WorkflowPath,
		Ref:                row.Ref,
		CreatedAt:          timeFromPG(row.CreatedAt),
		UpdatedAt:          timeFromPG(row.UpdatedAt),
	}
	if len(row.InputsJson) > 0 {
		if err := json.Unmarshal(row.InputsJson, &record.Inputs); err != nil {
			return ScheduleRecord{}, fmt.Errorf("decode workflow schedule inputs: %w", err)
		}
	}
	if record.Inputs == nil {
		record.Inputs = map[string]string{}
	}
	return record, nil
}

func dispatchRecordFromStore(row store.ListExecutionScheduleDispatchesRow) DispatchRecord {
	return DispatchRecord{
		DispatchID:          row.DispatchID,
		ScheduleID:          row.ScheduleID,
		TemporalWorkflowID:  row.TemporalWorkflowID,
		TemporalRunID:       row.TemporalRunID,
		SourceWorkflowRunID: row.SourceWorkflowRunID,
		ProjectID:           row.ProjectID,
		WorkflowState:       row.WorkflowState,
		State:               row.State,
		FailureReason:       row.FailureReason,
		ScheduledAt:         timeFromPG(row.ScheduledAt),
		SubmittedAt:         timePtrFromPG(row.SubmittedAt),
		CreatedAt:           timeFromPG(row.CreatedAt),
		UpdatedAt:           timeFromPG(row.UpdatedAt),
	}
}

func dispatchRecordFromUpsert(row store.UpsertExecutionScheduleDispatchStartRow) DispatchRecord {
	return DispatchRecord{
		DispatchID:          row.DispatchID,
		ScheduleID:          row.ScheduleID,
		TemporalWorkflowID:  row.TemporalWorkflowID,
		TemporalRunID:       row.TemporalRunID,
		SourceWorkflowRunID: row.SourceWorkflowRunID,
		ProjectID:           row.ProjectID,
		WorkflowState:       row.WorkflowState,
		State:               row.State,
		FailureReason:       row.FailureReason,
		ScheduledAt:         timeFromPG(row.ScheduledAt),
		SubmittedAt:         timePtrFromPG(row.SubmittedAt),
		CreatedAt:           timeFromPG(row.CreatedAt),
		UpdatedAt:           timeFromPG(row.UpdatedAt),
	}
}
