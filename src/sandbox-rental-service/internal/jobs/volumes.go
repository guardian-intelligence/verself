package jobs

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/forge-metal/apiwire"
	billingclient "github.com/forge-metal/billing-service/client"
	"github.com/forge-metal/sandbox-rental-service/internal/scheduler"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const (
	VolumeStateActive        = "active"
	VolumeStateReadOnly      = "read_only"
	VolumeStateWriteBlocked  = "write_blocked"
	VolumeStateRetentionOnly = "retention_only"
	VolumeStateDeleted       = "deleted"

	VolumeMeterStateQueued           = "queued"
	VolumeMeterStateBillingReserving = "billing_reserving"
	VolumeMeterStateBillingSettled   = "billing_settled"
	VolumeMeterStateBillingFailed    = "billing_failed"

	defaultVolumeWindowMillis uint32 = 60_000
	defaultStorageNodeID             = "single-node"
	defaultStoragePoolID             = "default"
)

var ErrVolumeMissing = errors.New("sandbox-rental: volume missing")

type VolumeCreateRequest struct {
	ProductID      string
	IdempotencyKey string
	DisplayName    string
}

type VolumeMeterTickRequest struct {
	IdempotencyKey       string
	WindowMillis         uint32
	UsedBytes            uint64
	UsedBySnapshotsBytes uint64
	WrittenBytes         uint64
	ProvisionedBytes     uint64
	ObservedAt           time.Time
}

type VolumeRecord struct {
	VolumeID              uuid.UUID
	OrgID                 uint64
	ActorID               string
	ProductID             string
	DisplayName           string
	State                 string
	StorageNodeID         string
	PoolID                string
	DatasetRef            string
	CurrentGenerationID   uuid.UUID
	UsedBytes             uint64
	UsedBySnapshotsBytes  uint64
	BillableLiveBytes     uint64
	BillableRetainedBytes uint64
	WrittenBytes          uint64
	ProvisionedBytes      uint64
	LastMeteredAt         *time.Time
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

type VolumeMeterTickRecord struct {
	MeterTickID           uuid.UUID
	VolumeID              uuid.UUID
	OrgID                 uint64
	ActorID               string
	ProductID             string
	SourceType            string
	SourceRef             string
	WindowSeq             uint32
	WindowMillis          uint32
	State                 string
	ObservedAt            time.Time
	WindowStart           time.Time
	WindowEnd             time.Time
	UsedBytes             uint64
	UsedBySnapshotsBytes  uint64
	BillableLiveBytes     uint64
	BillableRetainedBytes uint64
	WrittenBytes          uint64
	ProvisionedBytes      uint64
	Allocation            map[string]float64
	BillingWindowID       string
	BilledChargeUnits     uint64
	BillingFailureReason  string
	ClickHouseProjectedAt *time.Time
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

type volumeMeterTickRow struct {
	MeterTickID           uuid.UUID          `ch:"meter_tick_id"`
	VolumeID              uuid.UUID          `ch:"volume_id"`
	VolumeGenerationID    uuid.UUID          `ch:"volume_generation_id"`
	OrgID                 uint64             `ch:"org_id"`
	ActorID               string             `ch:"actor_id"`
	ProductID             string             `ch:"product_id"`
	SourceType            string             `ch:"source_type"`
	SourceRef             string             `ch:"source_ref"`
	WindowSeq             uint32             `ch:"window_seq"`
	WindowMillis          uint32             `ch:"window_millis"`
	State                 string             `ch:"state"`
	StorageNodeID         string             `ch:"storage_node_id"`
	PoolID                string             `ch:"pool_id"`
	DatasetRef            string             `ch:"dataset_ref"`
	ObservedAt            time.Time          `ch:"observed_at"`
	WindowStart           time.Time          `ch:"window_start"`
	WindowEnd             time.Time          `ch:"window_end"`
	UsedBytes             uint64             `ch:"used_bytes"`
	UsedBySnapshotsBytes  uint64             `ch:"usedbysnapshots_bytes"`
	BillableLiveBytes     uint64             `ch:"billable_live_bytes"`
	BillableRetainedBytes uint64             `ch:"billable_retained_bytes"`
	WrittenBytes          uint64             `ch:"written_bytes"`
	ProvisionedBytes      uint64             `ch:"provisioned_bytes"`
	LiveGiB               float64            `ch:"live_gib"`
	RetainedGiB           float64            `ch:"retained_gib"`
	Dimensions            map[string]float64 `ch:"dimensions"`
	ComponentQuantities   map[string]float64 `ch:"component_quantities"`
	BillingWindowID       string             `ch:"billing_window_id"`
	BilledChargeUnits     uint64             `ch:"billed_charge_units"`
	BillingFailureReason  string             `ch:"billing_failure_reason"`
	RecordedAt            time.Time          `ch:"recorded_at"`
	TraceID               string             `ch:"trace_id"`
}

func (s *Service) CreateVolume(ctx context.Context, orgID uint64, actorID string, req VolumeCreateRequest) (VolumeRecord, error) {
	ctx, span := tracer.Start(ctx, "sandbox-rental.volume.create")
	defer span.End()
	if s.PGX == nil {
		return VolumeRecord{}, ErrRunnerUnavailable
	}
	req.ProductID = firstNonEmpty(strings.TrimSpace(req.ProductID), defaultProductID)
	req.IdempotencyKey = strings.TrimSpace(req.IdempotencyKey)
	req.DisplayName = strings.TrimSpace(req.DisplayName)
	if req.IdempotencyKey == "" {
		return VolumeRecord{}, fmt.Errorf("idempotency_key is required")
	}
	volumeID := uuid.New()
	generationID := uuid.New()
	datasetRef := "volume:" + strings.ReplaceAll(volumeID.String(), "-", "")
	now := time.Now().UTC()

	tx, err := s.PGX.Begin(ctx)
	if err != nil {
		return VolumeRecord{}, fmt.Errorf("begin create volume: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var inserted uuid.UUID
	err = tx.QueryRow(ctx, `
		INSERT INTO volumes (
			volume_id, org_id, actor_id, product_id, idempotency_key, display_name, state,
			storage_node_id, pool_id, dataset_ref, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$11)
		ON CONFLICT (org_id, idempotency_key) DO NOTHING
		RETURNING volume_id
	`, volumeID, orgID, actorID, req.ProductID, req.IdempotencyKey, req.DisplayName, VolumeStateActive, defaultStorageNodeID, defaultStoragePoolID, datasetRef, now).Scan(&inserted)
	if errors.Is(err, pgx.ErrNoRows) {
		if err := tx.Commit(ctx); err != nil {
			return VolumeRecord{}, fmt.Errorf("commit existing volume lookup: %w", err)
		}
		return s.GetVolumeByIdempotency(ctx, orgID, req.IdempotencyKey)
	}
	if err != nil {
		return VolumeRecord{}, fmt.Errorf("insert volume: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO volume_generations (volume_generation_id, volume_id, org_id, generation_seq, source_ref, state, created_at)
		VALUES ($1,$2,$3,1,$4,'current',$5)
	`, generationID, volumeID, orgID, datasetRef+":gen:1", now); err != nil {
		return VolumeRecord{}, fmt.Errorf("insert volume generation: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE volumes SET current_generation_id = $2 WHERE volume_id = $1`, volumeID, generationID); err != nil {
		return VolumeRecord{}, fmt.Errorf("set current volume generation: %w", err)
	}
	if err := insertVolumeEvent(ctx, tx, volumeID, orgID, "volume.created", map[string]any{"volume_id": volumeID.String(), "dataset_ref": datasetRef, "storage_node_id": defaultStorageNodeID, "pool_id": defaultStoragePoolID}); err != nil {
		return VolumeRecord{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return VolumeRecord{}, fmt.Errorf("commit create volume: %w", err)
	}
	span.SetAttributes(attribute.String("volume.id", volumeID.String()), attribute.String("org.id", fmt.Sprintf("%d", orgID)))
	return s.GetVolume(ctx, orgID, volumeID)
}

func (s *Service) GetVolumeByIdempotency(ctx context.Context, orgID uint64, idempotencyKey string) (VolumeRecord, error) {
	var volumeID uuid.UUID
	if err := s.PGX.QueryRow(ctx, `SELECT volume_id FROM volumes WHERE org_id = $1 AND idempotency_key = $2`, orgID, strings.TrimSpace(idempotencyKey)).Scan(&volumeID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return VolumeRecord{}, ErrVolumeMissing
		}
		return VolumeRecord{}, err
	}
	return s.GetVolume(ctx, orgID, volumeID)
}

func (s *Service) GetVolume(ctx context.Context, orgID uint64, volumeID uuid.UUID) (VolumeRecord, error) {
	row := s.PGX.QueryRow(ctx, volumeRecordSelectSQL()+` WHERE volume_id = $1 AND org_id = $2`, volumeID, orgID)
	return scanVolumeRecord(row)
}

func (s *Service) ListVolumes(ctx context.Context, orgID uint64) ([]VolumeRecord, error) {
	rows, err := s.PGX.Query(ctx, volumeRecordSelectSQL()+` WHERE org_id = $1 ORDER BY updated_at DESC, volume_id`, orgID)
	if err != nil {
		return nil, fmt.Errorf("query volumes: %w", err)
	}
	defer rows.Close()
	out := []VolumeRecord{}
	for rows.Next() {
		record, err := scanVolumeRecord(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	return out, rows.Err()
}

func (s *Service) EnqueueVolumeMeterTick(ctx context.Context, orgID uint64, actorID string, volumeID uuid.UUID, req VolumeMeterTickRequest) (VolumeMeterTickRecord, scheduler.ProbeResult, error) {
	ctx, span := tracer.Start(ctx, "sandbox-rental.volume.meter_tick.enqueue")
	defer span.End()
	if s.PGX == nil || s.Scheduler == nil {
		return VolumeMeterTickRecord{}, scheduler.ProbeResult{}, ErrRunnerUnavailable
	}
	req.IdempotencyKey = strings.TrimSpace(req.IdempotencyKey)
	if req.IdempotencyKey == "" {
		return VolumeMeterTickRecord{}, scheduler.ProbeResult{}, fmt.Errorf("idempotency_key is required")
	}
	if req.WindowMillis == 0 {
		req.WindowMillis = defaultVolumeWindowMillis
	}
	if req.WindowMillis < 30_000 {
		return VolumeMeterTickRecord{}, scheduler.ProbeResult{}, fmt.Errorf("window_millis must be at least 30000")
	}
	observedAt := req.ObservedAt.UTC()
	if observedAt.IsZero() {
		observedAt = time.Now().UTC()
	}
	windowEnd := observedAt
	windowStart := windowEnd.Add(-time.Duration(req.WindowMillis) * time.Millisecond)
	liveBytes, retainedBytes := billableVolumeBytes(req.UsedBytes, req.UsedBySnapshotsBytes)
	allocation := volumeBillingAllocation(liveBytes, retainedBytes)
	allocationJSON, err := json.Marshal(allocation)
	if err != nil {
		return VolumeMeterTickRecord{}, scheduler.ProbeResult{}, fmt.Errorf("marshal volume allocation: %w", err)
	}
	usedBytes, err := int64FromUint64("used_bytes", req.UsedBytes)
	if err != nil {
		return VolumeMeterTickRecord{}, scheduler.ProbeResult{}, err
	}
	snapshotBytes, err := int64FromUint64("usedbysnapshots_bytes", req.UsedBySnapshotsBytes)
	if err != nil {
		return VolumeMeterTickRecord{}, scheduler.ProbeResult{}, err
	}
	billableLiveBytes, err := int64FromUint64("billable_live_bytes", liveBytes)
	if err != nil {
		return VolumeMeterTickRecord{}, scheduler.ProbeResult{}, err
	}
	billableRetainedBytes, err := int64FromUint64("billable_retained_bytes", retainedBytes)
	if err != nil {
		return VolumeMeterTickRecord{}, scheduler.ProbeResult{}, err
	}
	writtenBytes, err := int64FromUint64("written_bytes", req.WrittenBytes)
	if err != nil {
		return VolumeMeterTickRecord{}, scheduler.ProbeResult{}, err
	}
	provisionedBytes, err := int64FromUint64("provisioned_bytes", req.ProvisionedBytes)
	if err != nil {
		return VolumeMeterTickRecord{}, scheduler.ProbeResult{}, err
	}
	tickID := uuid.New()
	sourceRef := "volume:" + volumeID.String() + ":tick:" + req.IdempotencyKey
	now := time.Now().UTC()

	tx, err := s.PGX.Begin(ctx)
	if err != nil {
		return VolumeMeterTickRecord{}, scheduler.ProbeResult{}, fmt.Errorf("begin volume meter tick: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var productID string
	if err := tx.QueryRow(ctx, `SELECT product_id FROM volumes WHERE volume_id = $1 AND org_id = $2 AND state <> 'deleted' FOR UPDATE`, volumeID, orgID).Scan(&productID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return VolumeMeterTickRecord{}, scheduler.ProbeResult{}, ErrVolumeMissing
		}
		return VolumeMeterTickRecord{}, scheduler.ProbeResult{}, err
	}
	var inserted uuid.UUID
	err = tx.QueryRow(ctx, `
		INSERT INTO volume_meter_ticks (
			meter_tick_id, volume_id, org_id, actor_id, product_id, idempotency_key, source_ref,
			window_seq, window_millis, state, observed_at, window_start, window_end,
			used_bytes, usedbysnapshots_bytes, billable_live_bytes, billable_retained_bytes,
			written_bytes, provisioned_bytes, allocation_jsonb, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,1,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$20)
		ON CONFLICT (org_id, idempotency_key) DO NOTHING
		RETURNING meter_tick_id
	`, tickID, volumeID, orgID, actorID, productID, req.IdempotencyKey, sourceRef, int64(req.WindowMillis), VolumeMeterStateQueued, observedAt, windowStart, windowEnd, usedBytes, snapshotBytes, billableLiveBytes, billableRetainedBytes, writtenBytes, provisionedBytes, allocationJSON, now).Scan(&inserted)
	if errors.Is(err, pgx.ErrNoRows) {
		if err := tx.Commit(ctx); err != nil {
			return VolumeMeterTickRecord{}, scheduler.ProbeResult{}, fmt.Errorf("commit existing volume meter tick lookup: %w", err)
		}
		tick, err := s.GetVolumeMeterTickByIdempotency(ctx, orgID, req.IdempotencyKey)
		return tick, scheduler.ProbeResult{}, err
	}
	if err != nil {
		return VolumeMeterTickRecord{}, scheduler.ProbeResult{}, fmt.Errorf("insert volume meter tick: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE volumes
		SET used_bytes = $2, usedbysnapshots_bytes = $3, billable_live_bytes = $4, billable_retained_bytes = $5,
		    written_bytes = $6, provisioned_bytes = $7, last_metered_at = $8, updated_at = $9
		WHERE volume_id = $1
	`, volumeID, usedBytes, snapshotBytes, billableLiveBytes, billableRetainedBytes, writtenBytes, provisionedBytes, observedAt, now); err != nil {
		return VolumeMeterTickRecord{}, scheduler.ProbeResult{}, fmt.Errorf("update volume current usage: %w", err)
	}
	if err := insertVolumeEvent(ctx, tx, volumeID, orgID, "volume.meter_tick_queued", map[string]any{"meter_tick_id": tickID.String(), "source_ref": sourceRef, "window_millis": req.WindowMillis}); err != nil {
		return VolumeMeterTickRecord{}, scheduler.ProbeResult{}, err
	}
	job, err := s.Scheduler.EnqueueVolumeMeterTickTx(ctx, tx, scheduler.VolumeMeterTickRequest{MeterTickID: tickID.String(), CorrelationID: CorrelationIDFromContext(ctx), TraceParent: traceParent(ctx)})
	if err != nil {
		return VolumeMeterTickRecord{}, scheduler.ProbeResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return VolumeMeterTickRecord{}, scheduler.ProbeResult{}, fmt.Errorf("commit volume meter tick: %w", err)
	}
	span.SetAttributes(attribute.String("volume.id", volumeID.String()), attribute.String("volume.meter_tick_id", tickID.String()), attribute.String("org.id", fmt.Sprintf("%d", orgID)))
	tick, err := s.GetVolumeMeterTick(ctx, orgID, tickID)
	return tick, job, err
}

func (s *Service) GetVolumeMeterTickByIdempotency(ctx context.Context, orgID uint64, idempotencyKey string) (VolumeMeterTickRecord, error) {
	var tickID uuid.UUID
	if err := s.PGX.QueryRow(ctx, `SELECT meter_tick_id FROM volume_meter_ticks WHERE org_id = $1 AND idempotency_key = $2`, orgID, strings.TrimSpace(idempotencyKey)).Scan(&tickID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return VolumeMeterTickRecord{}, ErrVolumeMissing
		}
		return VolumeMeterTickRecord{}, err
	}
	return s.GetVolumeMeterTick(ctx, orgID, tickID)
}

func (s *Service) GetVolumeMeterTick(ctx context.Context, orgID uint64, tickID uuid.UUID) (VolumeMeterTickRecord, error) {
	row := s.PGX.QueryRow(ctx, volumeMeterTickSelectSQL()+` WHERE meter_tick_id = $1 AND org_id = $2`, tickID, orgID)
	return scanVolumeMeterTick(row)
}

func (s *Service) RunVolumeMeterTick(ctx context.Context, tickID uuid.UUID) (err error) {
	ctx, span := tracer.Start(ctx, "sandbox-rental.volume.meter_tick.run")
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()
	if s.PGX == nil || s.Billing == nil {
		return ErrRunnerUnavailable
	}
	tick, volume, err := s.loadVolumeMeterTickForWork(ctx, tickID)
	if err != nil {
		return err
	}
	span.SetAttributes(attribute.String("volume.id", tick.VolumeID.String()), attribute.String("volume.meter_tick_id", tickID.String()), attribute.String("org.id", fmt.Sprintf("%d", tick.OrgID)), attribute.String("volume.meter_tick_state", tick.State))
	if tick.ClickHouseProjectedAt != nil {
		return nil
	}
	switch tick.State {
	case VolumeMeterStateBillingSettled, VolumeMeterStateBillingFailed:
		return s.projectVolumeMeterTick(ctx, tick, volume)
	case VolumeMeterStateQueued, VolumeMeterStateBillingReserving:
	default:
		return nil
	}
	if _, err := s.PGX.Exec(ctx, `UPDATE volume_meter_ticks SET state = $2, updated_at = $3 WHERE meter_tick_id = $1 AND state IN ('queued','billing_reserving')`, tickID, VolumeMeterStateBillingReserving, time.Now().UTC()); err != nil {
		return fmt.Errorf("mark volume meter tick reserving: %w", err)
	}

	reservation, err := s.reserveBillingWindow(ctx, apiwire.BillingReserveWindowRequest{
		OrgID:            apiwire.Uint64(tick.OrgID),
		ProductID:        tick.ProductID,
		ActorID:          tick.ActorID,
		ConcurrentCount:  1,
		SourceType:       tick.SourceType,
		SourceRef:        tick.SourceRef,
		WindowSeq:        tick.WindowSeq,
		ReservationShape: string(billingclient.Time),
		ReservedQuantity: tick.WindowMillis,
		BillingJobID:     billingJobIDForAttempt(tick.MeterTickID),
		Allocation:       tick.Allocation,
	})
	if err != nil {
		if errors.Is(err, ErrBillingPaymentRequired) || errors.Is(err, ErrBillingForbidden) {
			return s.failVolumeMeterTick(ctx, tick, volume, err)
		}
		return err
	}
	result, err := s.settleBillingWindow(ctx, reservation, tick.WindowMillis, volumeUsageSummary(tick))
	if err != nil {
		if errors.Is(err, ErrBillingPaymentRequired) || errors.Is(err, ErrBillingForbidden) {
			return s.failVolumeMeterTick(ctx, tick, volume, err)
		}
		return err
	}
	tick.State = VolumeMeterStateBillingSettled
	tick.BillingWindowID = reservation.WindowID
	tick.BilledChargeUnits = result.BilledChargeUnits.Uint64()
	tick.BillingFailureReason = ""
	billedChargeUnits, err := int64FromUint64("billed_charge_units", tick.BilledChargeUnits)
	if err != nil {
		return err
	}
	if _, err := s.PGX.Exec(ctx, `
		UPDATE volume_meter_ticks
		SET state = $2, billing_window_id = $3, billed_charge_units = $4, billing_failure_reason = '', updated_at = $5
		WHERE meter_tick_id = $1
	`, tickID, tick.State, tick.BillingWindowID, billedChargeUnits, time.Now().UTC()); err != nil {
		return fmt.Errorf("mark volume meter tick settled: %w", err)
	}
	if err := s.projectVolumeMeterTick(ctx, tick, volume); err != nil {
		return err
	}
	_, err = s.PGX.Exec(ctx, `UPDATE volumes SET state = CASE WHEN state = 'write_blocked' THEN 'active' ELSE state END, updated_at = $2 WHERE volume_id = $1`, tick.VolumeID, time.Now().UTC())
	return err
}

func (s *Service) failVolumeMeterTick(ctx context.Context, tick VolumeMeterTickRecord, volume VolumeRecord, cause error) error {
	tick.State = VolumeMeterStateBillingFailed
	tick.BillingFailureReason = strings.TrimSpace(cause.Error())
	if _, err := s.PGX.Exec(ctx, `
		UPDATE volume_meter_ticks
		SET state = $2, billing_failure_reason = $3, updated_at = $4
		WHERE meter_tick_id = $1
	`, tick.MeterTickID, tick.State, tick.BillingFailureReason, time.Now().UTC()); err != nil {
		return fmt.Errorf("mark volume meter tick failed: %w", err)
	}
	if _, err := s.PGX.Exec(ctx, `
		UPDATE volumes
		SET state = 'write_blocked', updated_at = $2
		WHERE volume_id = $1 AND state IN ('active','read_only')
	`, tick.VolumeID, time.Now().UTC()); err != nil {
		return fmt.Errorf("mark volume write blocked: %w", err)
	}
	if err := s.projectVolumeMeterTick(ctx, tick, volume); err != nil {
		return err
	}
	return nil
}

func (s *Service) projectVolumeMeterTick(ctx context.Context, tick VolumeMeterTickRecord, volume VolumeRecord) error {
	if s.CH == nil {
		_, err := s.PGX.Exec(ctx, `UPDATE volume_meter_ticks SET clickhouse_projected_at = $2, updated_at = $2 WHERE meter_tick_id = $1`, tick.MeterTickID, time.Now().UTC())
		return err
	}
	recordedAt := time.Now().UTC()
	row := volumeMeterTickRow{
		MeterTickID:           tick.MeterTickID,
		VolumeID:              tick.VolumeID,
		VolumeGenerationID:    volume.CurrentGenerationID,
		OrgID:                 tick.OrgID,
		ActorID:               tick.ActorID,
		ProductID:             tick.ProductID,
		SourceType:            tick.SourceType,
		SourceRef:             tick.SourceRef,
		WindowSeq:             tick.WindowSeq,
		WindowMillis:          tick.WindowMillis,
		State:                 tick.State,
		StorageNodeID:         volume.StorageNodeID,
		PoolID:                volume.PoolID,
		DatasetRef:            volume.DatasetRef,
		ObservedAt:            tick.ObservedAt,
		WindowStart:           tick.WindowStart,
		WindowEnd:             tick.WindowEnd,
		UsedBytes:             tick.UsedBytes,
		UsedBySnapshotsBytes:  tick.UsedBySnapshotsBytes,
		BillableLiveBytes:     tick.BillableLiveBytes,
		BillableRetainedBytes: tick.BillableRetainedBytes,
		WrittenBytes:          tick.WrittenBytes,
		ProvisionedBytes:      tick.ProvisionedBytes,
		LiveGiB:               bytesToGiB(tick.BillableLiveBytes),
		RetainedGiB:           bytesToGiB(tick.BillableRetainedBytes),
		Dimensions:            tick.Allocation,
		ComponentQuantities:   volumeComponentQuantities(tick),
		BillingWindowID:       tick.BillingWindowID,
		BilledChargeUnits:     tick.BilledChargeUnits,
		BillingFailureReason:  tick.BillingFailureReason,
		RecordedAt:            recordedAt,
		TraceID:               traceIDFromContext(ctx),
	}
	batch, err := s.CH.PrepareBatch(ctx, "INSERT INTO "+s.CHDatabase+".volume_meter_ticks")
	if err != nil {
		return fmt.Errorf("prepare volume meter tick batch: %w", err)
	}
	if err := batch.AppendStruct(&row); err != nil {
		return fmt.Errorf("append volume meter tick row: %w", err)
	}
	if err := batch.Send(); err != nil {
		return fmt.Errorf("send volume meter tick batch: %w", err)
	}
	_, err = s.PGX.Exec(ctx, `UPDATE volume_meter_ticks SET clickhouse_projected_at = $2, updated_at = $2 WHERE meter_tick_id = $1`, tick.MeterTickID, recordedAt)
	return err
}

func (s *Service) loadVolumeMeterTickForWork(ctx context.Context, tickID uuid.UUID) (VolumeMeterTickRecord, VolumeRecord, error) {
	tick, err := scanVolumeMeterTick(s.PGX.QueryRow(ctx, volumeMeterTickSelectSQL()+` WHERE meter_tick_id = $1`, tickID))
	if err != nil {
		return VolumeMeterTickRecord{}, VolumeRecord{}, err
	}
	volume, err := s.GetVolume(ctx, tick.OrgID, tick.VolumeID)
	if err != nil {
		return VolumeMeterTickRecord{}, VolumeRecord{}, err
	}
	return tick, volume, nil
}

func volumeRecordSelectSQL() string {
	return `SELECT volume_id, org_id, actor_id, product_id, display_name, state, storage_node_id, pool_id, dataset_ref,
		COALESCE(current_generation_id, '00000000-0000-0000-0000-000000000000'::uuid),
		used_bytes, usedbysnapshots_bytes, billable_live_bytes, billable_retained_bytes, written_bytes, provisioned_bytes,
		last_metered_at, created_at, updated_at FROM volumes`
}

type volumeScanner interface {
	Scan(dest ...any) error
}

func scanVolumeRecord(row volumeScanner) (VolumeRecord, error) {
	var record VolumeRecord
	var orgID, usedBytes, snapshotsBytes, liveBytes, retainedBytes, writtenBytes, provisionedBytes int64
	var lastMetered sql.NullTime
	if err := row.Scan(&record.VolumeID, &orgID, &record.ActorID, &record.ProductID, &record.DisplayName, &record.State, &record.StorageNodeID, &record.PoolID, &record.DatasetRef, &record.CurrentGenerationID, &usedBytes, &snapshotsBytes, &liveBytes, &retainedBytes, &writtenBytes, &provisionedBytes, &lastMetered, &record.CreatedAt, &record.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return VolumeRecord{}, ErrVolumeMissing
		}
		return VolumeRecord{}, err
	}
	record.OrgID = uint64(orgID)
	record.UsedBytes = uint64(usedBytes)
	record.UsedBySnapshotsBytes = uint64(snapshotsBytes)
	record.BillableLiveBytes = uint64(liveBytes)
	record.BillableRetainedBytes = uint64(retainedBytes)
	record.WrittenBytes = uint64(writtenBytes)
	record.ProvisionedBytes = uint64(provisionedBytes)
	if lastMetered.Valid {
		value := lastMetered.Time.UTC()
		record.LastMeteredAt = &value
	}
	return record, nil
}

func volumeMeterTickSelectSQL() string {
	return `SELECT meter_tick_id, volume_id, org_id, actor_id, product_id, source_type, source_ref, window_seq, window_millis, state,
		observed_at, window_start, window_end, used_bytes, usedbysnapshots_bytes, billable_live_bytes, billable_retained_bytes,
		written_bytes, provisioned_bytes, allocation_jsonb, billing_window_id, billed_charge_units, billing_failure_reason,
		clickhouse_projected_at, created_at, updated_at FROM volume_meter_ticks`
}

func scanVolumeMeterTick(row volumeScanner) (VolumeMeterTickRecord, error) {
	var tick VolumeMeterTickRecord
	var orgID, windowSeq, windowMillis, usedBytes, snapshotsBytes, liveBytes, retainedBytes, writtenBytes, provisionedBytes, billedCharge int64
	var allocationBytes []byte
	var projected sql.NullTime
	if err := row.Scan(&tick.MeterTickID, &tick.VolumeID, &orgID, &tick.ActorID, &tick.ProductID, &tick.SourceType, &tick.SourceRef, &windowSeq, &windowMillis, &tick.State, &tick.ObservedAt, &tick.WindowStart, &tick.WindowEnd, &usedBytes, &snapshotsBytes, &liveBytes, &retainedBytes, &writtenBytes, &provisionedBytes, &allocationBytes, &tick.BillingWindowID, &billedCharge, &tick.BillingFailureReason, &projected, &tick.CreatedAt, &tick.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return VolumeMeterTickRecord{}, ErrVolumeMissing
		}
		return VolumeMeterTickRecord{}, err
	}
	tick.OrgID = uint64(orgID)
	tick.WindowSeq = uint32(windowSeq)
	tick.WindowMillis = uint32(windowMillis)
	tick.UsedBytes = uint64(usedBytes)
	tick.UsedBySnapshotsBytes = uint64(snapshotsBytes)
	tick.BillableLiveBytes = uint64(liveBytes)
	tick.BillableRetainedBytes = uint64(retainedBytes)
	tick.WrittenBytes = uint64(writtenBytes)
	tick.ProvisionedBytes = uint64(provisionedBytes)
	tick.BilledChargeUnits = uint64(billedCharge)
	if err := json.Unmarshal(allocationBytes, &tick.Allocation); err != nil {
		return VolumeMeterTickRecord{}, fmt.Errorf("decode volume allocation: %w", err)
	}
	if projected.Valid {
		value := projected.Time.UTC()
		tick.ClickHouseProjectedAt = &value
	}
	return tick, nil
}

func insertVolumeEvent(ctx context.Context, tx pgx.Tx, volumeID uuid.UUID, orgID uint64, eventType string, payload map[string]any) error {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal volume event payload: %w", err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO volume_events (volume_id, org_id, event_type, trace_id, payload, created_at) VALUES ($1,$2,$3,$4,$5,$6)`, volumeID, orgID, eventType, traceIDFromContext(ctx), payloadBytes, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("insert volume event: %w", err)
	}
	return nil
}

func billableVolumeBytes(usedBytes, usedBySnapshotsBytes uint64) (uint64, uint64) {
	if usedBySnapshotsBytes >= usedBytes {
		return 0, usedBySnapshotsBytes
	}
	return usedBytes - usedBySnapshotsBytes, usedBySnapshotsBytes
}

func volumeBillingAllocation(liveBytes, retainedBytes uint64) map[string]float64 {
	return map[string]float64{
		billingSKUDurableVolumeLiveGiBMs:     bytesToGiB(liveBytes),
		billingSKUDurableVolumeRetainedGiBMs: bytesToGiB(retainedBytes),
	}
}

func volumeComponentQuantities(tick VolumeMeterTickRecord) map[string]float64 {
	return map[string]float64{
		billingSKUDurableVolumeLiveGiBMs:     bytesToGiB(tick.BillableLiveBytes) * float64(tick.WindowMillis),
		billingSKUDurableVolumeRetainedGiBMs: bytesToGiB(tick.BillableRetainedBytes) * float64(tick.WindowMillis),
	}
}

func volumeUsageSummary(tick VolumeMeterTickRecord) map[string]any {
	return map[string]any{
		"volume_id":               tick.VolumeID.String(),
		"meter_tick_id":           tick.MeterTickID.String(),
		"used_bytes":              tick.UsedBytes,
		"usedbysnapshots_bytes":   tick.UsedBySnapshotsBytes,
		"billable_live_bytes":     tick.BillableLiveBytes,
		"billable_retained_bytes": tick.BillableRetainedBytes,
		"written_bytes":           tick.WrittenBytes,
		"provisioned_bytes":       tick.ProvisionedBytes,
		"observed_window_millis":  tick.WindowMillis,
	}
}

func bytesToGiB(value uint64) float64 {
	return float64(value) / float64(billingBytesPerGiB)
}

func int64FromUint64(field string, value uint64) (int64, error) {
	if value > math.MaxInt64 {
		return 0, fmt.Errorf("%s exceeds int64 range", field)
	}
	return int64(value), nil
}

func traceIDFromContext(ctx context.Context) string {
	spanContext := trace.SpanContextFromContext(ctx)
	if !spanContext.HasTraceID() {
		return ""
	}
	return spanContext.TraceID().String()
}
