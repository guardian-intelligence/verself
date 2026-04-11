package jobs

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	WebhookDeliveryQueued     = "queued"
	WebhookDeliveryProcessing = "processing"
	WebhookDeliveryProcessed  = "processed"
	WebhookDeliveryIgnored    = "ignored"
	WebhookDeliveryFailed     = "failed"
)

type RecordWebhookDeliveryRequest struct {
	EndpointID         uuid.UUID
	IntegrationID      uuid.UUID
	OrgID              uint64
	Provider           string
	ProviderHost       string
	ProviderDeliveryID string
	EventType          string
	Payload            json.RawMessage
	TraceID            string
}

type WebhookDeliveryRecord struct {
	DeliveryID         uuid.UUID       `json:"delivery_id"`
	EndpointID         uuid.UUID       `json:"endpoint_id"`
	IntegrationID      uuid.UUID       `json:"integration_id"`
	OrgID              uint64          `json:"org_id"`
	Provider           string          `json:"provider"`
	ProviderHost       string          `json:"provider_host"`
	ProviderDeliveryID string          `json:"provider_delivery_id"`
	EventType          string          `json:"event_type"`
	State              string          `json:"state"`
	Payload            json.RawMessage `json:"payload"`
	PayloadSHA256      string          `json:"payload_sha256"`
	AttemptCount       int             `json:"attempt_count"`
	LastError          string          `json:"last_error"`
	TraceID            string          `json:"trace_id"`
	ReceivedAt         time.Time       `json:"received_at"`
	ClaimedAt          *time.Time      `json:"claimed_at,omitempty"`
	ProcessedAt        *time.Time      `json:"processed_at,omitempty"`
	NextAttemptAt      time.Time       `json:"next_attempt_at"`
	UpdatedAt          time.Time       `json:"updated_at"`
}

type WebhookDeliveryEventRow struct {
	DeliveryID         string    `ch:"delivery_id"`
	EndpointID         string    `ch:"endpoint_id"`
	IntegrationID      string    `ch:"integration_id"`
	OrgID              uint64    `ch:"org_id"`
	Provider           string    `ch:"provider"`
	ProviderHost       string    `ch:"provider_host"`
	ProviderDeliveryID string    `ch:"provider_delivery_id"`
	EventType          string    `ch:"event_type"`
	State              string    `ch:"state"`
	AttemptCount       uint16    `ch:"attempt_count"`
	PayloadSHA256      string    `ch:"payload_sha256"`
	Error              string    `ch:"error"`
	TraceID            string    `ch:"trace_id"`
	CreatedAt          time.Time `ch:"created_at"`
}

func (s *Service) RecordWebhookDelivery(ctx context.Context, req RecordWebhookDeliveryRequest) (*WebhookDeliveryRecord, bool, error) {
	req, err := normalizeRecordWebhookDeliveryRequest(req)
	if err != nil {
		return nil, false, err
	}
	now := time.Now().UTC()
	deliveryID := uuid.New()
	payloadSHA := webhookPayloadSHA256(req.Payload)
	var insertedID uuid.UUID
	err = s.PG.QueryRowContext(ctx, `
		INSERT INTO webhook_deliveries (
			delivery_id, endpoint_id, integration_id, org_id, provider, provider_host,
			provider_delivery_id, event_type, state, payload, payload_sha256,
			trace_id, received_at, next_attempt_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6,
			$7, $8, $9, $10::jsonb, $11,
			$12, $13, $13, $13
		)
		ON CONFLICT (endpoint_id, provider_delivery_id)
			WHERE provider_delivery_id <> ''
			DO NOTHING
		RETURNING delivery_id
	`, deliveryID, req.EndpointID, req.IntegrationID, int64(req.OrgID), req.Provider, req.ProviderHost,
		req.ProviderDeliveryID, req.EventType, WebhookDeliveryQueued, string(req.Payload), payloadSHA,
		req.TraceID, now).Scan(&insertedID)
	if err == nil {
		if _, err := s.PG.ExecContext(ctx, `
			UPDATE webhook_endpoints
			SET delivery_count = delivery_count + 1,
			    last_delivery_at = $2,
			    updated_at = $2
			WHERE endpoint_id = $1
		`, req.EndpointID, now); err != nil {
			return nil, false, fmt.Errorf("update webhook endpoint delivery counters: %w", err)
		}
		record, err := s.GetWebhookDelivery(ctx, insertedID)
		if err != nil {
			return nil, false, err
		}
		s.writeWebhookDeliveryEvent(ctx, *record, WebhookDeliveryQueued, "")
		return record, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, false, fmt.Errorf("insert webhook delivery: %w", err)
	}
	record, err := s.GetWebhookDeliveryByProviderDelivery(ctx, req.EndpointID, req.ProviderDeliveryID)
	if err != nil {
		return nil, false, err
	}
	return record, false, nil
}

func (s *Service) GetWebhookDelivery(ctx context.Context, deliveryID uuid.UUID) (*WebhookDeliveryRecord, error) {
	record, err := scanWebhookDeliveryRow(s.PG.QueryRowContext(ctx, webhookDeliverySelectSQL()+`
		WHERE delivery_id = $1
	`, deliveryID))
	if err != nil {
		return nil, err
	}
	return record, nil
}

func (s *Service) GetWebhookDeliveryByProviderDelivery(ctx context.Context, endpointID uuid.UUID, providerDeliveryID string) (*WebhookDeliveryRecord, error) {
	record, err := scanWebhookDeliveryRow(s.PG.QueryRowContext(ctx, webhookDeliverySelectSQL()+`
		WHERE endpoint_id = $1 AND provider_delivery_id = $2
	`, endpointID, strings.TrimSpace(providerDeliveryID)))
	if err != nil {
		return nil, err
	}
	return record, nil
}

func (s *Service) ClaimNextWebhookDelivery(ctx context.Context) (*WebhookDeliveryRecord, bool, error) {
	tx, err := s.PG.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, fmt.Errorf("begin webhook delivery claim: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var deliveryID uuid.UUID
	err = tx.QueryRowContext(ctx, `
		SELECT delivery_id
		FROM webhook_deliveries
		WHERE state IN ($1, $2)
		  AND next_attempt_at <= now()
		ORDER BY received_at
		LIMIT 1
		FOR UPDATE SKIP LOCKED
	`, WebhookDeliveryQueued, WebhookDeliveryFailed).Scan(&deliveryID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			if err := tx.Commit(); err != nil {
				return nil, false, fmt.Errorf("commit empty webhook delivery claim: %w", err)
			}
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("claim webhook delivery: %w", err)
	}
	now := time.Now().UTC()
	if _, err := tx.ExecContext(ctx, `
		UPDATE webhook_deliveries
		SET state = $2,
		    attempt_count = attempt_count + 1,
		    claimed_at = $3,
		    updated_at = $3
		WHERE delivery_id = $1
	`, deliveryID, WebhookDeliveryProcessing, now); err != nil {
		return nil, false, fmt.Errorf("mark webhook delivery processing: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, false, fmt.Errorf("commit webhook delivery claim: %w", err)
	}
	record, err := s.GetWebhookDelivery(ctx, deliveryID)
	if err != nil {
		return nil, false, err
	}
	s.writeWebhookDeliveryEvent(ctx, *record, WebhookDeliveryProcessing, "")
	return record, true, nil
}

func (s *Service) MarkWebhookDeliveryProcessed(ctx context.Context, deliveryID uuid.UUID) error {
	return s.markWebhookDeliveryTerminal(ctx, deliveryID, WebhookDeliveryProcessed, "")
}

func (s *Service) MarkWebhookDeliveryIgnored(ctx context.Context, deliveryID uuid.UUID, reason string) error {
	return s.markWebhookDeliveryTerminal(ctx, deliveryID, WebhookDeliveryIgnored, reason)
}

func (s *Service) MarkWebhookDeliveryFailed(ctx context.Context, deliveryID uuid.UUID, cause error) error {
	errText := strings.TrimSpace(fmt.Sprint(cause))
	now := time.Now().UTC()
	if _, err := s.PG.ExecContext(ctx, `
		UPDATE webhook_deliveries
		SET state = $2,
		    last_error = $3,
		    next_attempt_at = $4,
		    updated_at = $5
		WHERE delivery_id = $1
	`, deliveryID, WebhookDeliveryFailed, errText, now.Add(webhookDeliveryRetryDelay(ctx, s, deliveryID)), now); err != nil {
		return fmt.Errorf("mark webhook delivery failed: %w", err)
	}
	record, err := s.GetWebhookDelivery(ctx, deliveryID)
	if err == nil {
		s.writeWebhookDeliveryEvent(ctx, *record, WebhookDeliveryFailed, errText)
	}
	return nil
}

func (s *Service) markWebhookDeliveryTerminal(ctx context.Context, deliveryID uuid.UUID, state, detail string) error {
	now := time.Now().UTC()
	if _, err := s.PG.ExecContext(ctx, `
		UPDATE webhook_deliveries
		SET state = $2,
		    last_error = $3,
		    processed_at = $4,
		    updated_at = $4
		WHERE delivery_id = $1
	`, deliveryID, state, strings.TrimSpace(detail), now); err != nil {
		return fmt.Errorf("mark webhook delivery %s: %w", state, err)
	}
	record, err := s.GetWebhookDelivery(ctx, deliveryID)
	if err == nil {
		s.writeWebhookDeliveryEvent(ctx, *record, state, detail)
	}
	return nil
}

func normalizeRecordWebhookDeliveryRequest(req RecordWebhookDeliveryRequest) (RecordWebhookDeliveryRequest, error) {
	req.Provider = strings.TrimSpace(strings.ToLower(req.Provider))
	req.ProviderHost = normalizeProviderHost(req.ProviderHost)
	req.ProviderDeliveryID = strings.TrimSpace(req.ProviderDeliveryID)
	req.EventType = strings.TrimSpace(req.EventType)
	req.Payload = json.RawMessage(strings.TrimSpace(string(req.Payload)))
	req.TraceID = strings.TrimSpace(req.TraceID)
	if req.EndpointID == uuid.Nil {
		return RecordWebhookDeliveryRequest{}, fmt.Errorf("endpoint_id is required")
	}
	if req.IntegrationID == uuid.Nil {
		return RecordWebhookDeliveryRequest{}, fmt.Errorf("integration_id is required")
	}
	if req.OrgID == 0 {
		return RecordWebhookDeliveryRequest{}, fmt.Errorf("org_id is required")
	}
	if req.Provider == "" {
		return RecordWebhookDeliveryRequest{}, fmt.Errorf("provider is required")
	}
	if req.ProviderHost == "" {
		return RecordWebhookDeliveryRequest{}, fmt.Errorf("provider_host is required")
	}
	if req.ProviderDeliveryID == "" {
		return RecordWebhookDeliveryRequest{}, fmt.Errorf("provider_delivery_id is required")
	}
	if req.EventType == "" {
		return RecordWebhookDeliveryRequest{}, fmt.Errorf("event_type is required")
	}
	if len(req.Payload) == 0 || !json.Valid(req.Payload) {
		return RecordWebhookDeliveryRequest{}, fmt.Errorf("payload must be valid JSON")
	}
	return req, nil
}

func webhookPayloadSHA256(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func webhookDeliveryRetryDelay(ctx context.Context, s *Service, deliveryID uuid.UUID) time.Duration {
	var attempts int
	if err := s.PG.QueryRowContext(ctx, `
		SELECT attempt_count
		FROM webhook_deliveries
		WHERE delivery_id = $1
	`, deliveryID).Scan(&attempts); err != nil {
		return time.Minute
	}
	if attempts <= 1 {
		return time.Second
	}
	if attempts >= 5 {
		return time.Hour
	}
	return time.Duration(attempts*attempts) * time.Second
}

func webhookDeliverySelectSQL() string {
	return `
		SELECT
			delivery_id,
			endpoint_id,
			integration_id,
			org_id,
			provider,
			provider_host,
			provider_delivery_id,
			event_type,
			state,
			payload,
			payload_sha256,
			attempt_count,
			last_error,
			trace_id,
			received_at,
			claimed_at,
			processed_at,
			next_attempt_at,
			updated_at
		FROM webhook_deliveries
	`
}

func scanWebhookDeliveryRow(scanner rowScanner) (*WebhookDeliveryRecord, error) {
	var (
		record      WebhookDeliveryRecord
		claimedAt   sql.NullTime
		processedAt sql.NullTime
		payload     []byte
	)
	if err := scanner.Scan(
		&record.DeliveryID,
		&record.EndpointID,
		&record.IntegrationID,
		&record.OrgID,
		&record.Provider,
		&record.ProviderHost,
		&record.ProviderDeliveryID,
		&record.EventType,
		&record.State,
		&payload,
		&record.PayloadSHA256,
		&record.AttemptCount,
		&record.LastError,
		&record.TraceID,
		&record.ReceivedAt,
		&claimedAt,
		&processedAt,
		&record.NextAttemptAt,
		&record.UpdatedAt,
	); err != nil {
		return nil, err
	}
	record.Payload = append(json.RawMessage(nil), payload...)
	if claimedAt.Valid {
		record.ClaimedAt = &claimedAt.Time
	}
	if processedAt.Valid {
		record.ProcessedAt = &processedAt.Time
	}
	return &record, nil
}

func (s *Service) writeWebhookDeliveryEvent(ctx context.Context, delivery WebhookDeliveryRecord, state, errText string) {
	if s.CH == nil {
		return
	}
	logger := webhookDeliveryLogger(s)
	batch, err := s.CH.PrepareBatch(ctx, "INSERT INTO "+s.CHDatabase+".webhook_delivery_events")
	if err != nil {
		logger.ErrorContext(ctx, "prepare webhook delivery event batch", "error", err)
		return
	}
	row := WebhookDeliveryEventRow{
		DeliveryID:         delivery.DeliveryID.String(),
		EndpointID:         delivery.EndpointID.String(),
		IntegrationID:      delivery.IntegrationID.String(),
		OrgID:              delivery.OrgID,
		Provider:           delivery.Provider,
		ProviderHost:       delivery.ProviderHost,
		ProviderDeliveryID: delivery.ProviderDeliveryID,
		EventType:          delivery.EventType,
		State:              state,
		AttemptCount:       uint16(delivery.AttemptCount),
		PayloadSHA256:      delivery.PayloadSHA256,
		Error:              strings.TrimSpace(errText),
		TraceID:            delivery.TraceID,
		CreatedAt:          time.Now().UTC(),
	}
	if err := batch.AppendStruct(&row); err != nil {
		logger.ErrorContext(ctx, "append webhook delivery event", "error", err)
		return
	}
	if err := batch.Send(); err != nil {
		logger.ErrorContext(ctx, "send webhook delivery event batch", "error", err)
	}
}

func webhookDeliveryLogger(s *Service) *slog.Logger {
	if s != nil && s.Logger != nil {
		return s.Logger
	}
	return slog.Default()
}
