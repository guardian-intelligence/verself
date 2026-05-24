package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/verself/sandbox-rental-service/internal/scheduler"
	"github.com/verself/sandbox-rental-service/internal/store"
	vmorchestrator "github.com/verself/vm-orchestrator"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const (
	goldenVMStateRequested    = "requested"
	goldenVMStateCreateQueued = "create_queued"
	goldenVMStateCreating     = "creating"
	goldenVMStateCreated      = "created"
	goldenVMStatePublishing   = "publishing"
	goldenVMStatePublished    = "published"
	goldenVMStatePromoting    = "promoting"
	goldenVMStateCommitted    = "committed"
	goldenVMStateSkipped      = "skipped"
	goldenVMStateFailed       = "failed"
)

func shouldQueueGoldenVMCreate(plan durableCachePlan, sealDecision durableSealDecision) bool {
	return plan.Enabled && plan.GoldenVM.Enabled && goldenVMCheckpointSkipReason(plan, sealDecision) == ""
}

func (s *Service) enqueueGoldenVMCreate(ctx context.Context, item executionWorkItem, leaseID, execID string, plan durableCachePlan) (bool, error) {
	if !shouldQueueGoldenVMCreate(plan, durableSealDecision{Commit: true}) {
		return false, nil
	}
	if s.Scheduler == nil {
		return false, fmt.Errorf("golden VM create scheduler is not configured")
	}
	tx, err := s.PGX.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin golden VM create enqueue: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	req := scheduler.GoldenVMCreateRequest{
		OperationID:   plan.GoldenVM.OperationID.String(),
		ExecutionID:   item.ExecutionID.String(),
		AttemptID:     item.AttemptID.String(),
		LeaseID:       leaseID,
		ExecID:        execID,
		CorrelationID: item.CorrelationID,
		TraceParent:   traceParent(ctx),
	}
	result, err := s.Scheduler.EnqueueGoldenVMCreateTx(ctx, tx, req)
	if err != nil {
		return false, err
	}
	now := time.Now().UTC()
	rows, err := store.New(tx).MarkGoldenVMOperationCreateQueued(ctx, store.MarkGoldenVMOperationCreateQueuedParams{
		LeaseID:     leaseID,
		ExecID:      execID,
		CreateJobID: result.JobID,
		RecordedAt:  pgTime(now),
		OperationID: plan.GoldenVM.OperationID,
	})
	if err != nil {
		return false, fmt.Errorf("mark golden VM create queued: %w", err)
	}
	if rows == 0 {
		return false, fmt.Errorf("golden VM operation %s was not requestable for create", plan.GoldenVM.OperationID)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit golden VM create enqueue: %w", err)
	}
	event := plan.GoldenVM.event(item.ExecutionID, item.AttemptID, plan.Identity, goldenVMEventCreateEnqueue, "succeeded", "")
	event.FromState = goldenVMStateRequested
	event.ToState = goldenVMStateCreateQueued
	event.LeaseID = leaseID
	event.ExecID = execID
	event.RiverJobID = result.JobID
	_ = s.appendGoldenVMEvent(ctx, event)
	return true, nil
}

func (s *Service) failGoldenVMCreateEnqueue(ctx context.Context, item executionWorkItem, plan durableCachePlan, cause error) {
	if !plan.Enabled || !plan.GoldenVM.Enabled {
		return
	}
	reason := durableFailureReason("golden_vm_create_enqueue_failed", cause)
	now := time.Now().UTC()
	if err := s.storeQueries().MarkGoldenVMOperationFailed(ctx, store.MarkGoldenVMOperationFailedParams{
		RecordedAt:    pgTime(now),
		FailureReason: reason,
		OperationID:   plan.GoldenVM.OperationID,
	}); err != nil && s.Logger != nil {
		s.Logger.WarnContext(ctx, "mark golden VM enqueue failed", "operation_id", plan.GoldenVM.OperationID, "error", err)
	}
	event := plan.GoldenVM.event(item.ExecutionID, item.AttemptID, plan.Identity, goldenVMEventCreateEnqueue, "failed", reason)
	event.FromState = goldenVMStateRequested
	event.ToState = goldenVMStateFailed
	_ = s.appendGoldenVMEvent(ctx, event)
}

func (s *Service) CreateGoldenVM(ctx context.Context, operationID uuid.UUID, riverJobID int64) error {
	ctx, span := tracer.Start(ctx, "golden_vm.create", trace.WithAttributes(
		attribute.String("golden_vm.operation_id", operationID.String()),
		attribute.Int64("river.job_id", riverJobID),
	))
	defer span.End()
	op, err := s.storeQueries().GetGoldenVMOperationForCreate(ctx, store.GetGoldenVMOperationForCreateParams{OperationID: operationID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	}
	span.SetAttributes(
		attribute.String("execution.id", op.ExecutionID.String()),
		attribute.String("attempt.id", op.AttemptID.String()),
		attribute.String("lease.id", op.LeaseID),
		attribute.String("golden_vm.state", op.State),
	)
	if goldenVMOperationTerminal(op.State) {
		return nil
	}
	if strings.TrimSpace(op.LeaseID) == "" {
		return fmt.Errorf("golden VM operation %s has no lease", op.OperationID)
	}

	renewCtx, stopRenew := context.WithCancel(detachedContext(ctx))
	defer stopRenew()
	go s.renewLeaseLoop(renewCtx, op.LeaseID, op.OperationID.String()+":golden-vm")

	item, err := s.loadWorkItem(ctx, op.ExecutionID, op.AttemptID)
	if err != nil {
		return err
	}
	plan, checkpoint, err := s.loadGoldenVMCreatePlan(ctx, op)
	if err != nil {
		return err
	}
	terminal := false
	defer func() {
		if terminal {
			s.releaseGoldenVMCreateLease(detachedContext(ctx), item, plan, op.LeaseID, riverJobID)
		}
	}()

	switch op.State {
	case goldenVMStateCreateQueued, goldenVMStateCreating, goldenVMStateRequested:
		_, err = s.createGoldenVMCheckpoint(ctx, item, plan, op, riverJobID)
		if err != nil {
			if ctx.Err() != nil {
				return err
			}
			if fallbackErr := s.finalizeDurableCachesWithoutGoldenVM(ctx, item, op.LeaseID, plan); fallbackErr != nil {
				span.RecordError(fallbackErr)
				if s.Logger != nil {
					s.Logger.WarnContext(ctx, "durable cache fallback after golden VM checkpoint failure failed", "execution_id", item.ExecutionID, "attempt_id", item.AttemptID, "operation_id", op.OperationID, "error", fallbackErr)
				}
			}
			terminal = true
			s.failGoldenVMCreate(ctx, item, plan, op, "checkpoint_failed", err)
			return nil
		}
		op, err = s.storeQueries().GetGoldenVMOperationForCreate(ctx, store.GetGoldenVMOperationForCreateParams{OperationID: operationID})
		if err != nil {
			return err
		}
		plan, _, err = s.loadGoldenVMCreatePlan(ctx, op)
		if err != nil {
			return err
		}
	}

	switch op.State {
	case goldenVMStateCreated, goldenVMStatePublishing:
		if checkpoint == nil {
			record := goldenVMCheckpointRecordFromOperation(op)
			checkpoint = &record
		}
		if err := s.publishGoldenVMCreate(ctx, item, plan, *checkpoint); err != nil {
			if ctx.Err() != nil {
				return err
			}
			terminal = true
			s.failGoldenVMCreate(ctx, item, plan, op, "publish_failed", err)
			return nil
		}
		op, err = s.storeQueries().GetGoldenVMOperationForCreate(ctx, store.GetGoldenVMOperationForCreateParams{OperationID: operationID})
		if err != nil {
			return err
		}
		plan, _, err = s.loadGoldenVMCreatePlan(ctx, op)
		if err != nil {
			return err
		}
	}

	if op.State == goldenVMStatePublished || op.State == goldenVMStatePromoting {
		if _, err := s.storeQueries().MarkGoldenVMOperationPromoting(ctx, store.MarkGoldenVMOperationPromotingParams{RecordedAt: pgTime(time.Now().UTC()), OperationID: op.OperationID}); err != nil {
			return err
		}
		if _, err := s.promoteDurableWorkflowRun(ctx, goldenRunRefFromRunnerIdentity(plan.Identity)); err != nil {
			return err
		}
		if _, err := s.promoteGoldenVMWorkflowRun(ctx, goldenRunRefFromRunnerIdentity(plan.Identity)); err != nil {
			return err
		}
		if err := s.storeQueries().MarkGoldenVMOperationCommitted(ctx, store.MarkGoldenVMOperationCommittedParams{RecordedAt: pgTime(time.Now().UTC()), OperationID: op.OperationID}); err != nil {
			return err
		}
	}
	terminal = true
	return nil
}

func (s *Service) finalizeDurableCachesWithoutGoldenVM(ctx context.Context, item executionWorkItem, leaseID string, plan durableCachePlan) error {
	commitPlan := plan
	commitPlan.GoldenVM = goldenVMPlan{}
	return s.finalizeDurableCaches(ctx, item, leaseID, commitPlan, durableSealDecision{Commit: true})
}

func (s *Service) createGoldenVMCheckpoint(ctx context.Context, item executionWorkItem, plan durableCachePlan, op store.GoldenVmOperation, riverJobID int64) (*vmorchestrator.GoldenVMCheckpointRecord, error) {
	now := time.Now().UTC()
	if _, err := s.storeQueries().MarkGoldenVMOperationCreating(ctx, store.MarkGoldenVMOperationCreatingParams{RecordedAt: pgTime(now), OperationID: op.OperationID}); err != nil {
		return nil, fmt.Errorf("mark golden VM creating: %w", err)
	}
	event := plan.GoldenVM.event(item.ExecutionID, item.AttemptID, plan.Identity, goldenVMEventCreateStart, "succeeded", "")
	event.FromState = firstNonEmpty(op.State, goldenVMStateCreateQueued)
	event.ToState = goldenVMStateCreating
	event.LeaseID = op.LeaseID
	event.ExecID = op.ExecID
	event.RiverJobID = riverJobID
	_ = s.appendGoldenVMEvent(ctx, event)

	checkpoint, err := s.Orchestrator.CheckpointGoldenVM(ctx, op.LeaseID, op.OperationID.String()+":checkpoint", op.OperationID.String(), op.CandidateGoldenVmSnapshotID.String())
	if err != nil {
		return nil, fmt.Errorf("checkpoint golden VM: %w", err)
	}
	mountSnapshotsJSON, err := goldenVMMountSnapshotsJSON(checkpoint.MountSnapshots)
	if err != nil {
		return nil, err
	}
	stateBytes, err := int64FromUint64("golden VM state bytes", checkpoint.StateBytes)
	if err != nil {
		return nil, err
	}
	memoryBytes, err := int64FromUint64("golden VM memory bytes", checkpoint.MemoryBytes)
	if err != nil {
		return nil, err
	}
	if _, err := s.storeQueries().MarkGoldenVMOperationCreated(ctx, store.MarkGoldenVMOperationCreatedParams{
		SnapshotKey:        checkpoint.SnapshotKey,
		RootSnapshotRef:    checkpoint.RootSnapshotRef,
		RootSnapshotGuid:   checkpoint.RootSnapshotGUID,
		VmstateArtifactRef: checkpoint.VMStateArtifactRef,
		MemoryArtifactRef:  checkpoint.MemoryArtifactRef,
		MountSnapshotsJson: mountSnapshotsJSON,
		StateBytes:         stateBytes,
		MemoryBytes:        memoryBytes,
		RecordedAt:         pgTime(checkpoint.CheckpointedAt),
		OperationID:        op.OperationID,
	}); err != nil {
		return nil, fmt.Errorf("mark golden VM created: %w", err)
	}
	beforeEvent := plan.GoldenVM.event(item.ExecutionID, item.AttemptID, plan.Identity, goldenVMEventBeforeSnapshot, "succeeded", "")
	beforeEvent.LeaseID = op.LeaseID
	beforeEvent.ExecID = op.ExecID
	beforeEvent.RiverJobID = riverJobID
	beforeEvent.SnapshotKey = checkpoint.SnapshotKey
	beforeEvent.VMStateArtifactRef = checkpoint.VMStateArtifactRef
	beforeEvent.MemoryArtifactRef = checkpoint.MemoryArtifactRef
	beforeEvent.RootSnapshotRef = checkpoint.RootSnapshotRef
	beforeEvent.RootSnapshotGUID = checkpoint.RootSnapshotGUID
	_ = s.appendGoldenVMEvent(ctx, beforeEvent)
	checkpointEvent := beforeEvent
	checkpointEvent.Name = goldenVMEventCheckpoint
	checkpointEvent.FromState = goldenVMStateCreating
	checkpointEvent.ToState = goldenVMStateCreated
	checkpointEvent.StateBytes = checkpoint.StateBytes
	checkpointEvent.MemoryBytes = checkpoint.MemoryBytes
	_ = s.appendGoldenVMEvent(ctx, checkpointEvent)
	return &checkpoint, nil
}

func (s *Service) publishGoldenVMCreate(ctx context.Context, item executionWorkItem, plan durableCachePlan, checkpoint vmorchestrator.GoldenVMCheckpointRecord) error {
	if _, err := s.storeQueries().MarkGoldenVMOperationPublishing(ctx, store.MarkGoldenVMOperationPublishingParams{RecordedAt: pgTime(time.Now().UTC()), OperationID: plan.GoldenVM.OperationID}); err != nil {
		return fmt.Errorf("mark golden VM publishing: %w", err)
	}
	generations, anyCandidate, err := s.commitGoldenVMDurableCaches(ctx, item, plan, checkpoint)
	if err != nil {
		return err
	}
	if err := s.publishGoldenVMSnapshot(ctx, item, plan, checkpoint, generations); err != nil {
		return err
	}
	if anyCandidate {
		if _, err := s.promoteDurableWorkflowRun(ctx, goldenRunRefFromRunnerIdentity(plan.Identity)); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) commitGoldenVMDurableCaches(ctx context.Context, item executionWorkItem, plan durableCachePlan, checkpoint vmorchestrator.GoldenVMCheckpointRecord) ([]goldenVMSnapshotGeneration, bool, error) {
	phaseStarted := time.Now().UTC()
	var retErr error
	defer func() {
		result, reason := sandboxPhaseResult(retErr)
		s.recordRunnerIdentityPhase(ctx, plan.Identity, item, "sandbox-rental", "sandbox.durable.seal", result, reason, phaseStarted, time.Now().UTC(), sandboxPhaseAttrs{
			"commit_allowed":     "true",
			"commit_skip_reason": "",
			"async_owner":        "golden.vm.create",
		})
	}()
	goldenGenerations := make([]goldenVMSnapshotGeneration, 0, len(plan.Operations))
	anyCandidate := false
	leaseID := strings.TrimSpace(checkpoint.LeaseID)
	if leaseID == "" {
		err := fmt.Errorf("golden VM checkpoint has no lease id")
		retErr = err
		return nil, false, err
	}
	for idx, op := range plan.Operations {
		if !op.Mounted {
			err := fmt.Errorf("durable mount %s is not mounted: %s", op.CacheName, firstNonEmpty(op.MountSkipReason, "mount_not_available"))
			retErr = err
			return nil, false, err
		}
		if err := s.storeQueries().MarkDurableOperationSealStarted(ctx, store.MarkDurableOperationSealStartedParams{SealStartedAt: pgTime(time.Now().UTC()), OperationID: op.OperationID}); err != nil {
			retErr = err
			return nil, false, err
		}
		commit, err := s.Orchestrator.CommitFilesystemMount(ctx, leaseID, op.OperationID.String()+":commit", op.OperationID.String(), op.MountName, op.DurableScopeID.String(), op.SourceSnapshotRef, op.CandidateGenerationID.String())
		if err != nil {
			_ = s.storeQueries().MarkDurableOperationFailed(ctx, store.MarkDurableOperationFailedParams{FailureReason: err.Error(), RecordedAt: pgTime(time.Now().UTC()), OperationID: op.OperationID})
			for _, event := range durableCommitFailureEvents(op, item.ExecutionID, item.AttemptID, plan.Identity, err) {
				_ = s.appendDurableEvent(ctx, event)
			}
			retErr = fmt.Errorf("commit durable cache %s: %w", op.CacheName, err)
			return nil, false, retErr
		}
		sealEvent := op.event(item.ExecutionID, item.AttemptID, plan.Identity, durableEventSeal, "succeeded", "", commit.Snapshot)
		sealEvent.UsedBytes = commit.UsedBytes
		sealEvent.WrittenBytes = commit.WrittenBytes
		_ = s.appendDurableEvent(ctx, sealEvent)
		usedBytes, err := int64FromUint64("durable used bytes", commit.UsedBytes)
		if err != nil {
			retErr = err
			return nil, false, err
		}
		writtenBytes, err := int64FromUint64("durable written bytes", commit.WrittenBytes)
		if err != nil {
			retErr = err
			return nil, false, err
		}
		now := time.Now().UTC()
		generationID, err := s.storeQueries().InsertDurableGeneration(ctx, store.InsertDurableGenerationParams{
			DurableGenerationID: op.CandidateGenerationID,
			DurableScopeID:      op.DurableScopeID,
			OperationID:         op.OperationID,
			SourceGenerationID:  op.SourceGenerationID,
			HeadSha:             firstNonEmpty(plan.Identity.HeadSHA, plan.Identity.RunHeadSHA),
			TreeHash:            "",
			ProviderRunID:       plan.Identity.ProviderRunID,
			ProviderRunAttempt:  plan.Identity.ProviderRunAttempt,
			ProviderJobID:       plan.Identity.ProviderJobID,
			Result:              "success",
			PromotionEligible:   op.PromotionEligible,
			State:               durableGenerationInitialState(op),
			ZfsSnapshotRef:      commit.Snapshot,
			ZfsSnapshotGuid:     commit.SnapshotGUID,
			UsedBytes:           usedBytes,
			WrittenBytes:        writtenBytes,
			SealedAt:            pgTime(commit.CommittedAt),
			CommittedAt:         pgTime(now),
			LastUsedAt:          pgTime(now),
			ExpiresAt:           pgOptionalTime(durableGenerationExpiresAt(op, now)),
		})
		if err != nil {
			retErr = fmt.Errorf("insert durable generation %s: %w", op.CacheName, err)
			return nil, false, retErr
		}
		if err := s.storeQueries().MarkDurableOperationResultRecorded(ctx, store.MarkDurableOperationResultRecordedParams{FinalState: "committed", SealedAt: pgTime(commit.CommittedAt), RecordedAt: pgTime(now), FailureReason: "", OperationID: op.OperationID}); err != nil {
			retErr = err
			return nil, false, err
		}
		commitEvent := op.event(item.ExecutionID, item.AttemptID, plan.Identity, durableEventCommit, "succeeded", "", commit.Snapshot)
		commitEvent.GenerationID = &generationID
		commitEvent.UsedBytes = commit.UsedBytes
		commitEvent.WrittenBytes = commit.WrittenBytes
		_ = s.appendDurableEvent(ctx, commitEvent)
		if !op.PromotionEligible {
			retainEvent := op.event(item.ExecutionID, item.AttemptID, plan.Identity, durableEventRetain, "succeeded", "non_promotable_scope", commit.Snapshot)
			retainEvent.GenerationID = &generationID
			retainEvent.UsedBytes = commit.UsedBytes
			retainEvent.WrittenBytes = commit.WrittenBytes
			_ = s.appendDurableEvent(ctx, retainEvent)
		}
		anyCandidate = anyCandidate || op.PromotionEligible
		if op.PromotionEligible {
			goldenGenerations = append(goldenGenerations, goldenVMSnapshotGeneration{
				DurableScopeID:      op.DurableScopeID,
				DurableGenerationID: generationID,
				CacheName:           op.CacheName,
				ZFSSnapshotRef:      commit.Snapshot,
				ZFSSnapshotGUID:     commit.SnapshotGUID,
				DriveID:             op.MountName,
				MountPath:           op.MountPath,
				BindPaths:           append([]string(nil), op.BindPaths...),
				FSType:              "ext4",
				ReadOnly:            false,
				Required:            op.Required,
				SortOrder:           idx,
			})
		}
	}
	return goldenGenerations, anyCandidate, nil
}

func (s *Service) releaseGoldenVMCreateLease(ctx context.Context, item executionWorkItem, plan durableCachePlan, leaseID string, riverJobID int64) {
	started := time.Now().UTC()
	releaseErr := s.Orchestrator.ReleaseLease(ctx, leaseID, plan.GoldenVM.OperationID.String()+":release")
	completed := time.Now().UTC()
	result, reason := sandboxPhaseResult(releaseErr)
	event := plan.GoldenVM.event(item.ExecutionID, item.AttemptID, plan.Identity, goldenVMEventCreateReleaseLease, result, reason)
	event.LeaseID = leaseID
	event.RiverJobID = riverJobID
	_ = s.appendGoldenVMEvent(ctx, event)
	s.recordExecutionPhase(ctx, item, "sandbox-rental", "vm.lease.release", result, reason, started, completed, sandboxPhaseAttrs{
		"async_owner": "golden.vm.create",
	})
	if releaseErr != nil && s.Logger != nil {
		s.Logger.WarnContext(ctx, "release lease after golden VM create failed", "lease_id", leaseID, "operation_id", plan.GoldenVM.OperationID, "error", releaseErr)
	}
}

func (s *Service) failGoldenVMCreate(ctx context.Context, item executionWorkItem, plan durableCachePlan, op store.GoldenVmOperation, reason string, cause error) {
	failureReason := durableFailureReason(reason, cause)
	_ = s.storeQueries().MarkGoldenVMOperationFailed(ctx, store.MarkGoldenVMOperationFailedParams{
		RecordedAt:    pgTime(time.Now().UTC()),
		FailureReason: failureReason,
		OperationID:   op.OperationID,
	})
	event := plan.GoldenVM.event(item.ExecutionID, item.AttemptID, plan.Identity, goldenVMEventCheckpoint, "failed", failureReason)
	event.FromState = op.State
	event.ToState = goldenVMStateFailed
	event.LeaseID = op.LeaseID
	event.ExecID = op.ExecID
	event.RiverJobID = op.CreateJobID
	_ = s.appendGoldenVMEvent(ctx, event)
}

func (s *Service) loadGoldenVMCreatePlan(ctx context.Context, op store.GoldenVmOperation) (durableCachePlan, *vmorchestrator.GoldenVMCheckpointRecord, error) {
	identity, err := s.runnerExecutionIdentity(ctx, op.ExecutionID, op.AttemptID)
	if err != nil {
		identity = goldenVMIdentityFromOperation(op)
	}
	rows, err := s.storeQueries().ListGoldenVMDurableOperations(ctx, store.ListGoldenVMDurableOperationsParams{AttemptID: op.AttemptID})
	if err != nil {
		return durableCachePlan{}, nil, err
	}
	plan := durableCachePlan{Enabled: true, Identity: identity, GoldenVM: goldenVMPlanFromOperation(op)}
	for _, row := range rows {
		var bindPaths []string
		if len(row.BindPathsJson) > 0 {
			if err := json.Unmarshal(row.BindPathsJson, &bindPaths); err != nil {
				return durableCachePlan{}, nil, fmt.Errorf("parse durable bind paths %s: %w", row.OperationID, err)
			}
		}
		plan.Operations = append(plan.Operations, durableCacheOperation{
			OperationID:           row.OperationID,
			DurableScopeID:        row.DurableScopeID,
			JobShapeID:            op.JobShapeID,
			SourceGenerationID:    row.SourceGenerationID,
			SourceSnapshotRef:     row.SourceSnapshotRef,
			CandidateGenerationID: row.CandidateGenerationID,
			MountName:             row.MountName,
			MountPath:             row.InternalMountPath,
			BindPaths:             bindPaths,
			TrustClass:            row.TrustClass,
			CacheName:             row.CacheName,
			PromotionEligible:     row.PromotionEligible,
			Required:              row.Required,
			SelectMissReason:      row.SourceSkipReason,
			Mounted:               row.FinalState == "mounted" || row.FinalState == "committed",
			MountSkipped:          row.FinalState == "skipped",
			MountSkipReason:       row.SourceSkipReason,
		})
	}
	if strings.TrimSpace(op.SnapshotKey) == "" {
		return plan, nil, nil
	}
	checkpoint := goldenVMCheckpointRecordFromOperation(op)
	return plan, &checkpoint, nil
}

func (s *Service) loadRunningDurablePlan(ctx context.Context, item executionWorkItem) (durableCachePlan, error) {
	if item.WorkloadKind != WorkloadKindRunner || item.ExternalProvider != RunnerProviderGitHub {
		return durableCachePlan{}, nil
	}
	op, err := s.storeQueries().GetGoldenVMOperationForAttempt(ctx, store.GetGoldenVMOperationForAttemptParams{AttemptID: item.AttemptID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return durableCachePlan{}, nil
		}
		return durableCachePlan{}, fmt.Errorf("load running golden VM operation: %w", err)
	}
	plan, _, err := s.loadGoldenVMCreatePlan(ctx, op)
	if err != nil {
		return durableCachePlan{}, fmt.Errorf("load running durable plan: %w", err)
	}
	return plan, nil
}

func goldenVMPlanFromOperation(op store.GoldenVmOperation) goldenVMPlan {
	return goldenVMPlan{
		Enabled:                 true,
		OperationID:             op.OperationID,
		CandidateSnapshotID:     op.CandidateGoldenVmSnapshotID,
		JobShapeID:              op.JobShapeID,
		ScopeRef:                op.ScopeRef,
		TrustClass:              op.TrustClass,
		SourceGenerationSetHash: op.SourceGenerationSetHash,
		PromotionEligible:       op.PromotionEligible,
	}
}

func goldenVMIdentityFromOperation(op store.GoldenVmOperation) RunnerExecutionIdentity {
	var allocationID uuid.UUID
	if op.AllocationID != nil {
		allocationID = *op.AllocationID
	}
	return RunnerExecutionIdentity{
		ExecutionID:          op.ExecutionID,
		AttemptID:            op.AttemptID,
		AllocationID:         allocationID,
		OrgID:                op.OrgID,
		Provider:             op.Provider,
		ProviderRepositoryID: op.ProviderRepositoryID,
		ProviderRunID:        op.ProviderRunID,
		ProviderRunAttempt:   op.ProviderRunAttempt,
		ProviderJobID:        op.ProviderJobID,
		HeadSHA:              op.HeadSha,
		RunHeadSHA:           op.HeadSha,
	}
}

func goldenVMCheckpointRecordFromOperation(op store.GoldenVmOperation) vmorchestrator.GoldenVMCheckpointRecord {
	return vmorchestrator.GoldenVMCheckpointRecord{
		LeaseID:            op.LeaseID,
		OperationID:        op.OperationID.String(),
		CheckpointID:       op.CandidateGoldenVmSnapshotID.String(),
		SnapshotKey:        op.SnapshotKey,
		RootSnapshotRef:    op.RootSnapshotRef,
		RootSnapshotGUID:   op.RootSnapshotGuid,
		VMStateArtifactRef: op.VmstateArtifactRef,
		MemoryArtifactRef:  op.MemoryArtifactRef,
		StateBytes:         uint64FromInt64(op.StateBytes, "golden VM state bytes"),
		MemoryBytes:        uint64FromInt64(op.MemoryBytes, "golden VM memory bytes"),
		CheckpointedAt:     timeFromPG(op.CreatedAt),
		MountSnapshots:     goldenVMMountSnapshotsFromJSON(op.MountSnapshotsJson),
	}
}

func goldenVMMountSnapshotsJSON(mounts []vmorchestrator.GoldenVMMountCheckpoint) ([]byte, error) {
	if mounts == nil {
		mounts = []vmorchestrator.GoldenVMMountCheckpoint{}
	}
	return json.Marshal(mounts)
}

func goldenVMMountSnapshotsFromJSON(raw []byte) []vmorchestrator.GoldenVMMountCheckpoint {
	if len(raw) == 0 {
		return nil
	}
	var mounts []vmorchestrator.GoldenVMMountCheckpoint
	if err := json.Unmarshal(raw, &mounts); err != nil {
		return nil
	}
	return mounts
}

func goldenVMOperationTerminal(state string) bool {
	switch strings.TrimSpace(state) {
	case goldenVMStateCommitted, goldenVMStateSkipped, goldenVMStateFailed:
		return true
	default:
		return false
	}
}
