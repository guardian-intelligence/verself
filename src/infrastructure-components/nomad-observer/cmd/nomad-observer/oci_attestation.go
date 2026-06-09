package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/hashicorp/nomad/api"
	ocidigest "github.com/opencontainers/go-digest"
	"go.opentelemetry.io/otel/trace"
)

const workloadOCIAttestationInsert = "INSERT INTO verself.workload_oci_attestations"

type WorkloadOCIAttestationRow struct {
	Site              string    `ch:"site"`
	NomadNamespace    string    `ch:"nomad_namespace"`
	NomadJobID        string    `ch:"nomad_job_id"`
	NomadGroup        string    `ch:"nomad_group"`
	NomadTask         string    `ch:"nomad_task"`
	AllocID           string    `ch:"alloc_id"`
	NodeID            string    `ch:"node_id"`
	NodeName          string    `ch:"node_name"`
	ImageRef          string    `ch:"image_ref"`
	DeclaredDigest    string    `ch:"declared_digest"`
	MeasuredDigest    string    `ch:"measured_digest"`
	SourceCommit      string    `ch:"source_commit"`
	DeployRunKey      string    `ch:"deploy_run_key"`
	SpecSHA256        string    `ch:"spec_sha256"`
	ArtifactSHA256    string    `ch:"artifact_sha256"`
	AllocClientStatus string    `ch:"alloc_client_status"`
	TaskState         string    `ch:"task_state"`
	Decision          string    `ch:"decision"`
	MeasurementSource string    `ch:"measurement_source"`
	Reason            string    `ch:"reason"`
	AllocModifyIndex  uint64    `ch:"alloc_modify_index"`
	TraceID           string    `ch:"trace_id"`
	SpanID            string    `ch:"span_id"`
	ObservedAt        time.Time `ch:"observed_at"`
}

func (o *observer) recordWorkloadOCIEvidence(ctx context.Context, alloc *api.Allocation, meta deployMeta) {
	if o.ch == nil || alloc == nil {
		return
	}
	recordCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	detailedAlloc, err := o.allocationWithJob(recordCtx, alloc)
	if err != nil {
		o.logger.WarnContext(recordCtx, "nomad.oci_attestation.alloc_lookup_failed",
			slog.String("nomad.namespace", normalizeNamespace(alloc.Namespace, o.cfg.namespace)),
			slog.String("nomad.alloc_id", alloc.ID),
			slog.String("nomad.job_id", alloc.JobID),
			slog.String("error", err.Error()),
		)
		return
	}
	if meta.empty() {
		meta = metadataFromAlloc(detailedAlloc)
	}
	rows := workloadOCIAttestationRows(recordCtx, o.cfg.site, normalizeNamespace(detailedAlloc.Namespace, o.cfg.namespace), detailedAlloc, meta, time.Now().UTC(), trace.SpanContextFromContext(ctx), o.measureWorkloadOCIRuntime)
	if len(rows) == 0 {
		return
	}
	if err := appendWorkloadOCIAttestations(recordCtx, o.ch, rows); err != nil {
		o.logger.WarnContext(recordCtx, "nomad.oci_attestation.insert_failed",
			slog.String("nomad.namespace", normalizeNamespace(detailedAlloc.Namespace, o.cfg.namespace)),
			slog.String("nomad.alloc_id", detailedAlloc.ID),
			slog.String("nomad.job_id", detailedAlloc.JobID),
			slog.Int("rows", len(rows)),
			slog.String("error", err.Error()),
		)
		return
	}
	o.logger.InfoContext(recordCtx, "nomad.oci_attestation.inserted",
		slog.String("nomad.namespace", normalizeNamespace(detailedAlloc.Namespace, o.cfg.namespace)),
		slog.String("nomad.alloc_id", detailedAlloc.ID),
		slog.String("nomad.job_id", detailedAlloc.JobID),
		slog.Int("rows", len(rows)),
	)
}

func (o *observer) allocationWithJob(ctx context.Context, alloc *api.Allocation) (*api.Allocation, error) {
	if alloc.Job != nil {
		return alloc, nil
	}
	namespace := normalizeNamespace(alloc.Namespace, o.cfg.namespace)
	detailedAlloc, _, err := o.client.Allocations().Info(alloc.ID, (&api.QueryOptions{Namespace: namespace}).WithContext(ctx))
	if err != nil {
		return nil, err
	}
	if detailedAlloc == nil {
		return nil, fmt.Errorf("allocation %s not found", alloc.ID)
	}
	if detailedAlloc.Job == nil {
		return nil, fmt.Errorf("allocation %s has no job payload", alloc.ID)
	}
	return detailedAlloc, nil
}

type workloadOCIRuntimeMeasurer func(context.Context, *api.Allocation, allocationOCITask) workloadOCIRuntimeMeasurement

type workloadOCIRuntimeMeasurement struct {
	digest string
	source string
	reason string
}

func workloadOCIAttestationRows(ctx context.Context, site, namespace string, alloc *api.Allocation, meta deployMeta, observedAt time.Time, spanContext trace.SpanContext, measure workloadOCIRuntimeMeasurer) []WorkloadOCIAttestationRow {
	if alloc == nil || alloc.Job == nil {
		return nil
	}
	tasks := allocationOCITasks(alloc)
	if len(tasks) == 0 {
		return nil
	}
	rows := make([]WorkloadOCIAttestationRow, 0, len(tasks))
	for _, task := range tasks {
		declaredDigest, digestErr := imageDigest(task.imageRef)
		measurement := workloadOCIRuntimeMeasurement{
			source: "podman_measurement_unconfigured",
			reason: "runtime container digest measurement is not configured",
		}
		if measure != nil {
			measurement = measure(ctx, alloc, task)
		}
		if strings.TrimSpace(measurement.source) == "" {
			measurement.source = "podman_measurement_unavailable"
		}
		decision, reason := workloadOCIDecision(declaredDigest, measurement.digest)
		if digestErr != nil {
			reason = digestErr.Error()
		} else if measurement.reason != "" && decision == "unmeasured" {
			reason = measurement.reason
		}
		taskState := ""
		if state := alloc.TaskStates[task.name]; state != nil {
			taskState = state.State
		}
		row := WorkloadOCIAttestationRow{
			Site:              site,
			NomadNamespace:    namespace,
			NomadJobID:        alloc.JobID,
			NomadGroup:        alloc.TaskGroup,
			NomadTask:         task.name,
			AllocID:           alloc.ID,
			NodeID:            alloc.NodeID,
			NodeName:          alloc.NodeName,
			ImageRef:          task.imageRef,
			DeclaredDigest:    declaredDigest,
			MeasuredDigest:    measurement.digest,
			SourceCommit:      meta.DeploySHA,
			DeployRunKey:      meta.DeployRunKey,
			SpecSHA256:        meta.SpecSHA256,
			ArtifactSHA256:    meta.ArtifactSHA256,
			AllocClientStatus: alloc.ClientStatus,
			TaskState:         taskState,
			Decision:          decision,
			MeasurementSource: measurement.source,
			Reason:            reason,
			AllocModifyIndex:  alloc.ModifyIndex,
			ObservedAt:        observedAt,
		}
		if spanContext.IsValid() {
			row.TraceID = spanContext.TraceID().String()
			row.SpanID = spanContext.SpanID().String()
		}
		rows = append(rows, row)
	}
	return rows
}

type allocationOCITask struct {
	name     string
	imageRef string
}

func allocationOCITasks(alloc *api.Allocation) []allocationOCITask {
	taskGroup := taskGroupForAllocation(alloc)
	if taskGroup == nil {
		return nil
	}
	tasks := make([]allocationOCITask, 0, len(taskGroup.Tasks))
	for _, task := range taskGroup.Tasks {
		if task == nil || task.Driver != "podman" {
			continue
		}
		imageRef, ok := task.Config["image"].(string)
		if !ok || strings.TrimSpace(imageRef) == "" {
			continue
		}
		tasks = append(tasks, allocationOCITask{name: task.Name, imageRef: strings.TrimSpace(imageRef)})
	}
	return tasks
}

func taskGroupForAllocation(alloc *api.Allocation) *api.TaskGroup {
	if alloc == nil || alloc.Job == nil {
		return nil
	}
	for _, group := range alloc.Job.TaskGroups {
		if group == nil {
			continue
		}
		if ptrValue(group.Name) == alloc.TaskGroup {
			return group
		}
	}
	return nil
}

func imageDigest(imageRef string) (string, error) {
	before, after, ok := strings.Cut(strings.TrimSpace(imageRef), "@")
	if !ok || before == "" || after == "" {
		return "", errors.New("image reference is not pinned by digest")
	}
	parsed, err := ocidigest.Parse(after)
	if err != nil {
		return "", fmt.Errorf("image reference digest is invalid: %w", err)
	}
	return parsed.String(), nil
}

func workloadOCIDecision(declaredDigest, measuredDigest string) (string, string) {
	if declaredDigest == "" {
		return "declared_digest_missing", "image reference is not pinned by digest"
	}
	if measuredDigest == "" {
		return "unmeasured", "runtime container digest measurement is not available"
	}
	if declaredDigest == measuredDigest {
		return "matched", ""
	}
	return "mismatched", "measured runtime digest differs from declared digest"
}

func appendWorkloadOCIAttestations(ctx context.Context, ch clickhouse.Conn, rows []WorkloadOCIAttestationRow) error {
	batch, err := ch.PrepareBatch(ctx, workloadOCIAttestationInsert)
	if err != nil {
		return err
	}
	for i := range rows {
		if err := batch.AppendStruct(&rows[i]); err != nil {
			_ = batch.Abort()
			return err
		}
	}
	return batch.Send()
}
