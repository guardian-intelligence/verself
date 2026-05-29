package nomadclient

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/hashicorp/nomad/api"
)

const failureLogTailBytes int64 = 32 * 1024

var (
	bearerPattern      = regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/=-]+`)
	jwtPattern         = regexp.MustCompile(`\b[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\b`)
	secretLinePattern  = regexp.MustCompile(`(?i)\b(authorization|proxy-authorization|cookie|set-cookie|token|jwt|access_token|id_token|refresh_token|client_secret|password|passwd|secret|api_key|private_key)\b\s*[:=]\s*([^\s]+)`)
	privateKeyPattern  = regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?-----END [A-Z ]*PRIVATE KEY-----`)
	terminalAllocState = map[string]struct{}{
		api.AllocClientStatusFailed: {},
		api.AllocClientStatusLost:   {},
	}
)

type FailurePacket struct {
	JobID             string
	EvalID            string
	DeploymentID      string
	DeploymentStatus  string
	StatusDescription string
	Evaluations       []FailureEvaluation
	Allocations       []FailureAllocation
	InspectErrors     []string
}

type FailureEvaluation struct {
	ID                string
	Status            string
	StatusDescription string
	TriggeredBy       string
	DeploymentID      string
	QueuedAllocs      int
	FailedTaskGroups  []PlanFailure
}

type FailureAllocation struct {
	ID                 string
	Name               string
	EvalID             string
	TaskGroup          string
	NodeID             string
	NodeName           string
	ClientStatus       string
	ClientDescription  string
	DesiredStatus      string
	DesiredDescription string
	Tasks              []FailureTask
	StdoutTail         string
	StderrTail         string
}

type FailureTask struct {
	Name          string
	State         string
	Failed        bool
	Restarts      uint64
	EventType     string
	EventMessage  string
	ExitCode      int
	DriverError   string
	SetupError    string
	DownloadError string
}

func (c *Client) InspectFailure(ctx context.Context, sub *SubmitResult) FailurePacket {
	packet := FailurePacket{}
	if sub == nil {
		packet.InspectErrors = append(packet.InspectErrors, "submit result unavailable")
		return packet
	}
	packet.JobID = sub.JobID
	packet.EvalID = sub.EvalID
	packet.DeploymentID = sub.DeploymentID
	if sub.DeploymentID != "" {
		dep, _, err := c.api.Deployments().Info(sub.DeploymentID, (&api.QueryOptions{}).WithContext(ctx))
		if err != nil {
			packet.InspectErrors = append(packet.InspectErrors, fmt.Sprintf("deployment %s: %v", sub.DeploymentID, err))
		} else if dep != nil {
			packet.DeploymentStatus = dep.Status
			packet.StatusDescription = dep.StatusDescription
		}
		stubs, _, err := c.api.Deployments().Allocations(sub.DeploymentID, (&api.QueryOptions{}).WithContext(ctx))
		if err != nil {
			packet.InspectErrors = append(packet.InspectErrors, fmt.Sprintf("deployment allocations %s: %v", sub.DeploymentID, err))
		} else {
			packet.Allocations = c.failureAllocations(ctx, stubs)
		}
	}
	packet.Evaluations = c.failureEvaluations(ctx, sub.EvalID)
	if len(packet.Allocations) == 0 && sub.EvalID != "" {
		stubs, _, err := c.api.Evaluations().Allocations(sub.EvalID, (&api.QueryOptions{}).WithContext(ctx))
		if err != nil {
			packet.InspectErrors = append(packet.InspectErrors, fmt.Sprintf("evaluation allocations %s: %v", sub.EvalID, err))
		} else {
			packet.Allocations = c.failureAllocations(ctx, stubs)
		}
	}
	return packet
}

func (c *Client) failureEvaluations(ctx context.Context, startID string) []FailureEvaluation {
	var out []FailureEvaluation
	seen := map[string]bool{}
	evalID := startID
	for evalID != "" && !seen[evalID] && len(out) < 8 {
		seen[evalID] = true
		ev, _, err := c.api.Evaluations().Info(evalID, (&api.QueryOptions{}).WithContext(ctx))
		if err != nil || ev == nil {
			break
		}
		out = append(out, FailureEvaluation{
			ID:                ev.ID,
			Status:            ev.Status,
			StatusDescription: ev.StatusDescription,
			TriggeredBy:       ev.TriggeredBy,
			DeploymentID:      ev.DeploymentID,
			QueuedAllocs:      sumIntMap(ev.QueuedAllocations),
			FailedTaskGroups:  planFailures(ev.FailedTGAllocs),
		})
		next := firstNonEmpty(ev.BlockedEval, ev.NextEval)
		if next == "" {
			break
		}
		evalID = next
	}
	return out
}

func (c *Client) failureAllocations(ctx context.Context, stubs []*api.AllocationListStub) []FailureAllocation {
	sort.Slice(stubs, func(i, j int) bool {
		return stubs[i].ModifyIndex > stubs[j].ModifyIndex
	})
	var out []FailureAllocation
	for _, stub := range stubs {
		if stub == nil {
			continue
		}
		if _, terminal := terminalAllocState[stub.ClientStatus]; !terminal && stub.ClientStatus != api.AllocClientStatusRunning {
			continue
		}
		alloc, _, err := c.api.Allocations().Info(stub.ID, (&api.QueryOptions{Namespace: stub.Namespace}).WithContext(ctx))
		if err != nil || alloc == nil {
			continue
		}
		row := FailureAllocation{
			ID:                 alloc.ID,
			Name:               alloc.Name,
			EvalID:             alloc.EvalID,
			TaskGroup:          alloc.TaskGroup,
			NodeID:             alloc.NodeID,
			NodeName:           alloc.NodeName,
			ClientStatus:       alloc.ClientStatus,
			ClientDescription:  alloc.ClientDescription,
			DesiredStatus:      alloc.DesiredStatus,
			DesiredDescription: alloc.DesiredDescription,
			Tasks:              failureTasks(alloc.TaskStates),
		}
		for _, task := range row.Tasks {
			if task.Name == "" {
				continue
			}
			if row.StderrTail == "" {
				row.StderrTail = c.readLogTail(ctx, alloc, task.Name, "stderr")
			}
			if row.StdoutTail == "" {
				row.StdoutTail = c.readLogTail(ctx, alloc, task.Name, "stdout")
			}
			break
		}
		out = append(out, row)
		if len(out) >= 6 {
			break
		}
	}
	return out
}

func failureTasks(states map[string]*api.TaskState) []FailureTask {
	names := make([]string, 0, len(states))
	for name := range states {
		names = append(names, name)
	}
	sort.Strings(names)
	var out []FailureTask
	for _, name := range names {
		state := states[name]
		if state == nil {
			continue
		}
		event := latestTaskEvent(state.Events)
		row := FailureTask{
			Name:     name,
			State:    state.State,
			Failed:   state.Failed,
			Restarts: state.Restarts,
		}
		if event != nil {
			row.EventType = event.Type
			row.EventMessage = firstNonEmpty(event.DisplayMessage, event.Message, event.DriverError, event.SetupError, event.DownloadError)
			row.ExitCode = event.ExitCode
			row.DriverError = event.DriverError
			row.SetupError = event.SetupError
			row.DownloadError = event.DownloadError
		}
		if row.Failed || row.State == "dead" || row.EventMessage != "" || row.Restarts > 0 {
			out = append(out, row)
		}
	}
	return out
}

func latestTaskEvent(events []*api.TaskEvent) *api.TaskEvent {
	for i := len(events) - 1; i >= 0; i-- {
		if events[i] != nil {
			return events[i]
		}
	}
	return nil
}

func (c *Client) readLogTail(ctx context.Context, alloc *api.Allocation, task, stream string) string {
	body, err := c.allocLogTail(ctx, alloc, task, stream)
	if err != nil || len(bytes.TrimSpace(body)) == 0 {
		return ""
	}
	redacted := redactLogTail(string(body))
	if int64(len(redacted)) > failureLogTailBytes {
		redacted = redacted[len(redacted)-int(failureLogTailBytes):]
	}
	return redacted
}

func (c *Client) allocLogTail(ctx context.Context, alloc *api.Allocation, task, stream string) ([]byte, error) {
	if alloc == nil {
		return nil, errors.New("allocation not found")
	}
	cancel := make(chan struct{})
	defer close(cancel)
	frames, errCh := c.api.AllocFS().Logs(
		alloc,
		false,
		task,
		stream,
		api.OriginEnd,
		failureLogTailBytes,
		cancel,
		(&api.QueryOptions{Namespace: alloc.Namespace}).WithContext(ctx),
	)
	if frames == nil {
		select {
		case err := <-errCh:
			if err == nil {
				err = errors.New("log stream unavailable")
			}
			return nil, err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	var buf bytes.Buffer
	for frame := range frames {
		if frame != nil && len(frame.Data) > 0 {
			_, _ = buf.Write(frame.Data)
			if int64(buf.Len()) > failureLogTailBytes*2 {
				trimBuffer(&buf, int(failureLogTailBytes))
			}
		}
	}
	select {
	case err := <-errCh:
		if err != nil {
			return nil, err
		}
	default:
	}
	trimBuffer(&buf, int(failureLogTailBytes))
	return buf.Bytes(), nil
}

func trimBuffer(buf *bytes.Buffer, limit int) {
	if limit <= 0 || buf.Len() <= limit {
		return
	}
	data := buf.Bytes()
	buf.Reset()
	_, _ = buf.Write(data[len(data)-limit:])
}

func redactLogTail(value string) string {
	value = privateKeyPattern.ReplaceAllString(value, "[REDACTED_PRIVATE_KEY]")
	value = bearerPattern.ReplaceAllString(value, "Bearer [REDACTED]")
	value = jwtPattern.ReplaceAllString(value, "[REDACTED_JWT]")
	value = secretLinePattern.ReplaceAllString(value, "$1=[REDACTED]")
	return value
}

func planFailures(values map[string]*api.AllocationMetric) []PlanFailure {
	resp := &api.JobPlanResponse{FailedTGAllocs: values}
	return planResult(resp).Failures
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func sumIntMap(values map[string]int) int {
	var total int
	for _, value := range values {
		total += value
	}
	return total
}
