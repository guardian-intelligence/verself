package jobs

import (
	"context"
	"fmt"
	"strings"

	"github.com/forge-metal/sandbox-rental-service/internal/scheduler"
)

type SchedulerProbeRequest struct {
	Message string
	OrgID   uint64
	ActorID string
}

type SchedulerProbeResult struct {
	JobID  int64
	Kind   string
	Queue  string
	Status string
}

func (s *Service) EnqueueSchedulerProbe(ctx context.Context, req SchedulerProbeRequest) (SchedulerProbeResult, error) {
	if s.Scheduler == nil {
		return SchedulerProbeResult{}, fmt.Errorf("scheduler runtime unavailable")
	}
	result, err := s.Scheduler.EnqueueProbe(ctx, scheduler.ProbeRequest{
		Message:       strings.TrimSpace(req.Message),
		OrgID:         req.OrgID,
		ActorID:       strings.TrimSpace(req.ActorID),
		CorrelationID: strings.TrimSpace(CorrelationIDFromContext(ctx)),
	})
	if err != nil {
		return SchedulerProbeResult{}, err
	}
	return SchedulerProbeResult{
		JobID:  result.JobID,
		Kind:   result.Kind,
		Queue:  result.Queue,
		Status: result.Status,
	}, nil
}
