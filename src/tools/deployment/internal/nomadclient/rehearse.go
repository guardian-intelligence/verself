package nomadclient

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/hashicorp/nomad/api"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type ValidationResult struct {
	DriverConfigValidated bool
	Warnings              string
	Errors                []string
}

func (r ValidationResult) Failed() bool {
	return len(r.Errors) > 0
}

type TaskGroupUpdate struct {
	Group   string
	Kind    string
	Desired uint64
}

type PlanFailure struct {
	Group              string
	NodePool           string
	NodesEvaluated     int
	NodesFiltered      int
	NodesExhausted     int
	CoalescedFailures  int
	ClassFiltered      []string
	ClassExhausted     []string
	ConstraintFiltered []string
	DimensionExhausted []string
	QuotaExhausted     []string
	ResourcesExhausted []string
}

type PlanResult struct {
	JobModifyIndex uint64
	DiffType       string
	Warnings       string
	Updates        []TaskGroupUpdate
	Failures       []PlanFailure
}

func (r PlanResult) Failed() bool {
	return len(r.Failures) > 0
}

type PlannedJob struct {
	Spec     *Spec
	Decision Decision
	Plan     PlanResult
}

func (c *Client) Validate(ctx context.Context, spec *Spec) (ValidationResult, error) {
	ctx, span := c.tracer.Start(ctx, "verself_deploy.nomad.validate",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(attribute.String("nomad.job_id", spec.JobID())),
	)
	defer span.End()

	resp, _, err := c.api.Jobs().Validate(spec.Job, (&api.WriteOptions{}).WithContext(ctx))
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return ValidationResult{}, fmt.Errorf("validate %s: %w", spec.JobID(), err)
	}
	result := ValidationResult{
		DriverConfigValidated: resp.DriverConfigValidated,
		Warnings:              strings.TrimSpace(resp.Warnings),
		Errors:                append([]string(nil), resp.ValidationErrors...),
	}
	if strings.TrimSpace(resp.Error) != "" && len(result.Errors) == 0 {
		result.Errors = append(result.Errors, strings.TrimSpace(resp.Error))
	}
	span.SetAttributes(
		attribute.Bool("nomad.driver_config_validated", result.DriverConfigValidated),
		attribute.Int("nomad.validation_errors", len(result.Errors)),
		attribute.Bool("nomad.validation_warnings", result.Warnings != ""),
	)
	if result.Failed() {
		err := fmt.Errorf("validate %s: %s", spec.JobID(), strings.Join(result.Errors, "; "))
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return result, nil
	}
	span.SetStatus(codes.Ok, "")
	return result, nil
}

func (c *Client) Plan(ctx context.Context, spec *Spec) (PlanResult, error) {
	ctx, span := c.tracer.Start(ctx, "verself_deploy.nomad.plan",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(attribute.String("nomad.job_id", spec.JobID())),
	)
	defer span.End()

	resp, _, err := c.api.Jobs().Plan(spec.Job, true, (&api.WriteOptions{}).WithContext(ctx))
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return PlanResult{}, fmt.Errorf("plan %s: %w", spec.JobID(), err)
	}
	result := planResult(resp)
	span.SetAttributes(
		attribute.Int64("nomad.job_modify_index", int64FromUint64(result.JobModifyIndex, "job modify index")),
		attribute.Int("nomad.failed_tg_allocs", len(result.Failures)),
		attribute.Bool("nomad.plan_warnings", result.Warnings != ""),
	)
	if result.DiffType != "" {
		span.SetAttributes(attribute.String("nomad.diff_type", result.DiffType))
	}
	if result.Failed() {
		err := fmt.Errorf("plan %s: %s", spec.JobID(), result.Failures[0].Summary())
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return result, nil
	}
	span.SetStatus(codes.Ok, "")
	return result, nil
}

func (f PlanFailure) Summary() string {
	parts := []string{fmt.Sprintf("group=%s", f.Group)}
	if len(f.DimensionExhausted) > 0 {
		parts = append(parts, "dimensions="+strings.Join(f.DimensionExhausted, ","))
	}
	if len(f.ResourcesExhausted) > 0 {
		parts = append(parts, "resources="+strings.Join(f.ResourcesExhausted, ","))
	}
	if len(f.ClassFiltered) > 0 {
		parts = append(parts, "class_filtered="+strings.Join(f.ClassFiltered, ","))
	}
	if len(f.ConstraintFiltered) > 0 {
		parts = append(parts, "constraints="+strings.Join(f.ConstraintFiltered, ","))
	}
	parts = append(parts, fmt.Sprintf("nodes_evaluated=%d", f.NodesEvaluated))
	parts = append(parts, fmt.Sprintf("nodes_exhausted=%d", f.NodesExhausted))
	return strings.Join(parts, " ")
}

func planResult(resp *api.JobPlanResponse) PlanResult {
	if resp == nil {
		return PlanResult{}
	}
	result := PlanResult{
		JobModifyIndex: resp.JobModifyIndex,
		Warnings:       strings.TrimSpace(resp.Warnings),
	}
	if resp.Diff != nil {
		result.DiffType = resp.Diff.Type
		for _, group := range resp.Diff.TaskGroups {
			if group == nil {
				continue
			}
			kinds := make([]string, 0, len(group.Updates))
			for kind := range group.Updates {
				kinds = append(kinds, kind)
			}
			sort.Strings(kinds)
			for _, kind := range kinds {
				result.Updates = append(result.Updates, TaskGroupUpdate{
					Group:   group.Name,
					Kind:    kind,
					Desired: group.Updates[kind],
				})
			}
		}
	}
	groups := make([]string, 0, len(resp.FailedTGAllocs))
	for group := range resp.FailedTGAllocs {
		groups = append(groups, group)
	}
	sort.Strings(groups)
	for _, group := range groups {
		metric := resp.FailedTGAllocs[group]
		if metric == nil {
			continue
		}
		result.Failures = append(result.Failures, PlanFailure{
			Group:              group,
			NodePool:           metric.NodePool,
			NodesEvaluated:     metric.NodesEvaluated,
			NodesFiltered:      metric.NodesFiltered,
			NodesExhausted:     metric.NodesExhausted,
			CoalescedFailures:  metric.CoalescedFailures,
			ClassFiltered:      sortedMapKeys(metric.ClassFiltered),
			ClassExhausted:     sortedMapKeys(metric.ClassExhausted),
			ConstraintFiltered: sortedMapKeys(metric.ConstraintFiltered),
			DimensionExhausted: sortedMapKeys(metric.DimensionExhausted),
			QuotaExhausted:     append([]string(nil), metric.QuotaExhausted...),
			ResourcesExhausted: sortedResourceKeys(metric.ResourcesExhausted),
		})
		sort.Strings(result.Failures[len(result.Failures)-1].QuotaExhausted)
	}
	return result
}

func sortedMapKeys(values map[string]int) []string {
	out := make([]string, 0, len(values))
	for key, count := range values {
		out = append(out, fmt.Sprintf("%s=%d", key, count))
	}
	sort.Strings(out)
	return out
}

func sortedResourceKeys(values map[string]*api.Resources) []string {
	out := make([]string, 0, len(values))
	for key, resources := range values {
		if resources == nil {
			out = append(out, key)
			continue
		}
		parts := []string{key}
		if resources.CPU != nil {
			parts = append(parts, fmt.Sprintf("cpu=%d", *resources.CPU))
		}
		if resources.MemoryMB != nil {
			parts = append(parts, fmt.Sprintf("memory=%d", *resources.MemoryMB))
		}
		if resources.DiskMB != nil {
			parts = append(parts, fmt.Sprintf("disk=%d", *resources.DiskMB))
		}
		out = append(out, strings.Join(parts, ":"))
	}
	sort.Strings(out)
	return out
}
