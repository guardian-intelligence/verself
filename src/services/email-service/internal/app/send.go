package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/verself/email-service/internal/mailstore"
	"github.com/verself/email-service/internal/provider"
)

type SendEmailRequest struct {
	OrgID          string `json:"org_id"`
	FromAddress    string `json:"from_address"`
	ToAddress      string `json:"to_address"`
	Subject        string `json:"subject"`
	TextBody       string `json:"text_body"`
	HTMLBody       string `json:"html_body"`
	IdempotencyKey string `json:"idempotency_key"`
	WorkflowKey    string `json:"workflow_key"`
	WorkflowRunID  string `json:"workflow_run_id"`
	ActorSubject   string `json:"actor_subject"`
	RequireSendAs  bool   `json:"require_send_as"`
}

type SendEmailResult struct {
	MessageID         string `json:"message_id"`
	Provider          string `json:"provider"`
	ProviderMessageID string `json:"provider_message_id"`
	Status            string `json:"status"`
}

func (s *Service) SendEmail(ctx context.Context, req SendEmailRequest) (SendEmailResult, error) {
	if s.store == nil {
		return SendEmailResult{}, fmt.Errorf("mailstore is not configured")
	}
	if s.sender == nil {
		return SendEmailResult{}, fmt.Errorf("email sender is not configured")
	}
	req = normalizeSendEmailRequest(req)
	if err := validateSendEmailRequest(req); err != nil {
		return SendEmailResult{}, err
	}
	if err := s.store.EnsureOutboundAddress(ctx, req.OrgID, req.FromAddress); err != nil {
		return SendEmailResult{}, err
	}
	if req.RequireSendAs {
		allowed, err := s.store.HasSendAsGrant(ctx, req.FromAddress, req.ActorSubject)
		if err != nil {
			return SendEmailResult{}, err
		}
		if !allowed {
			slog.WarnContext(ctx, "email-service: send_as denied",
				"org_id", req.OrgID,
				"from_address", req.FromAddress,
				"actor_subject", req.ActorSubject,
			)
			return SendEmailResult{}, fmt.Errorf("send_as denied for %s", req.FromAddress)
		}
	}

	messageID := stableID("email_msg", req.OrgID, req.IdempotencyKey)
	payloadFingerprint := stableID(
		"email_payload",
		req.OrgID,
		req.FromAddress,
		req.ToAddress,
		req.Subject,
		req.TextBody,
		req.HTMLBody,
		req.WorkflowKey,
		req.WorkflowRunID,
	)
	outbound, err := s.store.CreateOutboundMessage(ctx, mailstore.OutboundDraft{
		MessageID:          messageID,
		OrgID:              req.OrgID,
		FromAddress:        req.FromAddress,
		ToAddress:          req.ToAddress,
		Subject:            req.Subject,
		TextBody:           req.TextBody,
		HTMLBody:           req.HTMLBody,
		IdempotencyKey:     req.IdempotencyKey,
		PayloadFingerprint: payloadFingerprint,
		WorkflowKey:        req.WorkflowKey,
		WorkflowRunID:      req.WorkflowRunID,
		Provider:           s.sender.Name(),
		CreatedBy:          firstNonEmpty(req.ActorSubject, "system:internal-email-producer"),
	})
	if err != nil {
		return SendEmailResult{}, err
	}
	if outbound.ProviderMessageID != "" && outbound.Status == "sent" {
		return SendEmailResult{
			MessageID:         outbound.MessageID,
			Provider:          outbound.Provider,
			ProviderMessageID: outbound.ProviderMessageID,
			Status:            outbound.Status,
		}, nil
	}
	slog.InfoContext(ctx, "email-service: outbound accepted",
		"org_id", req.OrgID,
		"message_id", outbound.MessageID,
		"from_address", req.FromAddress,
		"to_address", req.ToAddress,
		"provider", s.sender.Name(),
	)

	attemptID := stableID("email_attempt", outbound.MessageID, time.Now().UTC().Format(time.RFC3339Nano))
	if err := s.store.InsertDeliveryAttempt(ctx, attemptID, outbound.MessageID, s.sender.Name(), req.IdempotencyKey, time.Now().UTC()); err != nil {
		return SendEmailResult{}, err
	}
	accepted, err := s.sender.Send(ctx, provider.Message{
		From:           req.FromAddress,
		To:             req.ToAddress,
		Subject:        req.Subject,
		Text:           req.TextBody,
		HTML:           req.HTMLBody,
		IdempotencyKey: req.IdempotencyKey,
		WorkflowKey:    req.WorkflowKey,
		WorkflowRunID:  req.WorkflowRunID,
	})
	if err != nil {
		_ = s.store.CompleteDelivery(ctx, attemptID, outbound.MessageID, "failed", "", err.Error(), time.Now().UTC())
		slog.ErrorContext(ctx, "email-service: provider send failed",
			"org_id", req.OrgID,
			"message_id", outbound.MessageID,
			"attempt_id", attemptID,
			"provider", s.sender.Name(),
			"error", err,
		)
		return SendEmailResult{}, err
	}
	if err := s.store.CompleteDelivery(ctx, attemptID, outbound.MessageID, "sent", accepted.ProviderMessageID, "", time.Now().UTC()); err != nil {
		return SendEmailResult{}, err
	}
	slog.InfoContext(ctx, "email-service: provider accepted",
		"org_id", req.OrgID,
		"message_id", outbound.MessageID,
		"attempt_id", attemptID,
		"provider", s.sender.Name(),
		"provider_message_id", accepted.ProviderMessageID,
	)
	return SendEmailResult{
		MessageID:         outbound.MessageID,
		Provider:          s.sender.Name(),
		ProviderMessageID: accepted.ProviderMessageID,
		Status:            "sent",
	}, nil
}

func normalizeSendEmailRequest(req SendEmailRequest) SendEmailRequest {
	req.OrgID = strings.TrimSpace(req.OrgID)
	req.FromAddress = strings.ToLower(strings.TrimSpace(req.FromAddress))
	req.ToAddress = strings.ToLower(strings.TrimSpace(req.ToAddress))
	req.Subject = strings.TrimSpace(req.Subject)
	req.IdempotencyKey = strings.TrimSpace(req.IdempotencyKey)
	req.WorkflowKey = strings.TrimSpace(req.WorkflowKey)
	req.WorkflowRunID = strings.TrimSpace(req.WorkflowRunID)
	req.ActorSubject = strings.TrimSpace(req.ActorSubject)
	return req
}

func validateSendEmailRequest(req SendEmailRequest) error {
	switch {
	case req.OrgID == "":
		return fmt.Errorf("org_id is required")
	case req.FromAddress == "":
		return fmt.Errorf("from_address is required")
	case req.ToAddress == "":
		return fmt.Errorf("to_address is required")
	case req.Subject == "":
		return fmt.Errorf("subject is required")
	case req.TextBody == "" && req.HTMLBody == "":
		return fmt.Errorf("text_body or html_body is required")
	case req.IdempotencyKey == "":
		return fmt.Errorf("idempotency_key is required")
	}
	return nil
}

func stableID(prefix string, values ...string) string {
	hash := sha256.New()
	for _, value := range values {
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(strings.TrimSpace(value)))
	}
	return prefix + "_" + hex.EncodeToString(hash.Sum(nil))[:32]
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
