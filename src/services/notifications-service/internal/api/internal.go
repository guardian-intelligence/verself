package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/verself/domain-transfer-objects"
	"github.com/verself/notifications-service/internal/notifications"
	workloadauth "github.com/verself/service-runtime/workload"
)

const (
	bodyLimitWorkflowJSON       = 64 << 10
	bodyLimitGrafanaWebhookJSON = 256 << 10
)

type InternalConfig struct {
	Service            *notifications.Service
	PlatformAlertOrgID string
	PlatformAlertEmail string
}

type triggerWorkflowInput struct {
	WorkflowKey    string `path:"workflow_key" maxLength:"128"`
	IdempotencyKey string `header:"Idempotency-Key" required:"true" minLength:"1" maxLength:"128"`
	Body           dto.NotificationWorkflowTriggerRequest
}

type triggerWorkflowOutput struct {
	Body dto.NotificationWorkflowTriggerResponse
}

type grafanaWebhookInput struct {
	Body grafanaWebhookPayload
}

type grafanaWebhookPayload struct {
	Receiver          string            `json:"receiver,omitempty"`
	Status            string            `json:"status,omitempty"`
	OrgID             string            `json:"orgId,omitempty"`
	GroupKey          string            `json:"groupKey,omitempty"`
	ExternalURL       string            `json:"externalURL,omitempty"`
	Title             string            `json:"title,omitempty"`
	Message           string            `json:"message,omitempty"`
	CommonLabels      map[string]string `json:"commonLabels,omitempty"`
	CommonAnnotations map[string]string `json:"commonAnnotations,omitempty"`
	Alerts            []grafanaAlert    `json:"alerts,omitempty"`
}

type grafanaAlert struct {
	Status       string            `json:"status,omitempty"`
	Labels       map[string]string `json:"labels,omitempty"`
	Annotations  map[string]string `json:"annotations,omitempty"`
	StartsAt     time.Time         `json:"startsAt,omitempty"`
	EndsAt       time.Time         `json:"endsAt,omitempty"`
	GeneratorURL string            `json:"generatorURL,omitempty"`
	DashboardURL string            `json:"dashboardURL,omitempty"`
	PanelURL     string            `json:"panelURL,omitempty"`
	SilenceURL   string            `json:"silenceURL,omitempty"`
	Fingerprint  string            `json:"fingerprint,omitempty"`
}

func RegisterInternalRoutes(api huma.API, cfg InternalConfig) {
	huma.Register(api, huma.Operation{
		OperationID:   "trigger-notification-workflow",
		Method:        http.MethodPost,
		Path:          "/internal/v1/workflows/{workflow_key}/trigger",
		Summary:       "Trigger a notification workflow",
		DefaultStatus: http.StatusAccepted,
		MaxBodyBytes:  bodyLimitWorkflowJSON,
		Security:      []map[string][]string{{"mutualTLS": {}}},
	}, triggerWorkflow(cfg.Service))

	huma.Register(api, huma.Operation{
		OperationID:   "receive-grafana-alert-webhook",
		Method:        http.MethodPost,
		Path:          "/internal/v1/integrations/grafana/alerts",
		Summary:       "Receive a Grafana alert webhook",
		DefaultStatus: http.StatusAccepted,
		MaxBodyBytes:  bodyLimitGrafanaWebhookJSON,
		Security:      []map[string][]string{{"mutualTLS": {}}},
	}, grafanaAlertWebhook(cfg))
}

func triggerWorkflow(svc *notifications.Service) func(context.Context, *triggerWorkflowInput) (*triggerWorkflowOutput, error) {
	return func(ctx context.Context, input *triggerWorkflowInput) (*triggerWorkflowOutput, error) {
		if err := validateIdempotencyKey(ctx, input.IdempotencyKey); err != nil {
			return nil, err
		}
		peerID, ok := workloadauth.PeerIDFromContext(ctx)
		if !ok {
			return nil, unauthorized(ctx)
		}
		recipients := make([]notifications.WorkflowRecipient, 0, len(input.Body.Recipients))
		for _, recipient := range input.Body.Recipients {
			recipients = append(recipients, notifications.WorkflowRecipient{
				SubjectID: recipient.SubjectID,
				Email:     recipient.Email,
			})
		}
		result, err := svc.TriggerWorkflow(ctx, notifications.WorkflowTriggerRequest{
			WorkflowKey:    input.WorkflowKey,
			OrgID:          input.Body.OrgID,
			TriggeredBy:    peerID.String(),
			IdempotencyKey: input.IdempotencyKey,
			Recipients:     recipients,
			Title:          input.Body.Title,
			Body:           input.Body.Body,
			ActionURL:      input.Body.ActionURL,
			Priority:       input.Body.Priority,
			ResourceKind:   notificationResourceKind(input.Body.TargetResourceName),
			ResourceID:     input.Body.TargetResourceName.String(),
			Data:           input.Body.Data,
			Traceparent:    input.Body.Traceparent,
		})
		if err != nil {
			return nil, notificationError(ctx, err)
		}
		return &triggerWorkflowOutput{Body: dto.NotificationWorkflowTriggerResponse{
			WorkflowRunID:            result.WorkflowRunID.String(),
			WorkflowKey:              result.WorkflowKey,
			OrgID:                    result.OrgID,
			AcceptedAt:               result.AcceptedAt,
			RecipientCount:           result.RecipientCount,
			WebNotificationsAccepted: result.WebNotificationsAccepted,
			EmailDeliveriesQueued:    result.EmailDeliveriesQueued,
			SuppressedCount:          result.SuppressedCount,
			Traceparent:              result.Traceparent,
		}}, nil
	}
}

func notificationResourceKind(name dto.ResourceName) string {
	if strings.TrimSpace(name.String()) == "" {
		return ""
	}
	return "resource_name"
}

func grafanaAlertWebhook(cfg InternalConfig) func(context.Context, *grafanaWebhookInput) (*triggerWorkflowOutput, error) {
	return func(ctx context.Context, input *grafanaWebhookInput) (*triggerWorkflowOutput, error) {
		peerID, ok := workloadauth.PeerIDFromContext(ctx)
		if !ok {
			return nil, unauthorized(ctx)
		}
		orgID := strings.TrimSpace(cfg.PlatformAlertOrgID)
		alertEmail := strings.TrimSpace(cfg.PlatformAlertEmail)
		if orgID == "" || alertEmail == "" {
			return nil, problem(ctx, http.StatusInternalServerError, "grafana-alert-recipient-not-configured", "Grafana alert recipient is not configured", nil)
		}
		payload := input.Body
		status := strings.ToLower(strings.TrimSpace(payload.Status))
		switch status {
		case "resolved":
		case "firing", "":
			status = "firing"
		default:
			status = "firing"
		}
		body, err := json.Marshal(payload)
		if err != nil {
			return nil, notificationError(ctx, err)
		}
		result, err := cfg.Service.TriggerWorkflow(ctx, notifications.WorkflowTriggerRequest{
			WorkflowKey:    "platform.grafana.alert." + status,
			OrgID:          orgID,
			TriggeredBy:    peerID.String(),
			IdempotencyKey: grafanaIdempotencyKey(payload),
			Recipients: []notifications.WorkflowRecipient{
				{Email: alertEmail},
			},
			Title:        truncateText(grafanaTitle(payload, status), 120),
			Body:         truncateText(grafanaBody(payload, status), 500),
			ActionURL:    truncateText(grafanaActionURL(payload), 500),
			Priority:     grafanaPriority(payload),
			ResourceKind: "grafana_alert_group",
			ResourceID:   grafanaResourceID(payload),
			Data:         body,
			Traceparent:  "",
		})
		if err != nil {
			return nil, notificationError(ctx, err)
		}
		return &triggerWorkflowOutput{Body: dto.NotificationWorkflowTriggerResponse{
			WorkflowRunID:            result.WorkflowRunID.String(),
			WorkflowKey:              result.WorkflowKey,
			OrgID:                    result.OrgID,
			AcceptedAt:               result.AcceptedAt,
			RecipientCount:           result.RecipientCount,
			WebNotificationsAccepted: result.WebNotificationsAccepted,
			EmailDeliveriesQueued:    result.EmailDeliveriesQueued,
			SuppressedCount:          result.SuppressedCount,
			Traceparent:              result.Traceparent,
		}}, nil
	}
}

func grafanaTitle(payload grafanaWebhookPayload, status string) string {
	if title := strings.TrimSpace(payload.Title); title != "" {
		return title
	}
	alertname := firstMapValue(payload.CommonLabels, "alertname", "rule", "name")
	if alertname == "" && len(payload.Alerts) > 0 {
		alertname = firstMapValue(payload.Alerts[0].Labels, "alertname", "rule", "name")
	}
	if alertname == "" {
		alertname = "Grafana alert"
	}
	return fmt.Sprintf("%s: %s", strings.ToUpper(status), alertname)
}

func grafanaBody(payload grafanaWebhookPayload, status string) string {
	if message := strings.TrimSpace(payload.Message); message != "" {
		return message
	}
	summary := firstMapValue(payload.CommonAnnotations, "summary", "description")
	if summary != "" {
		return summary
	}
	if len(payload.Alerts) > 0 {
		alert := payload.Alerts[0]
		summary = firstMapValue(alert.Annotations, "summary", "description")
		if summary != "" {
			return summary
		}
		return fmt.Sprintf("%s alert from Grafana: %s", status, formatLabels(alert.Labels))
	}
	return fmt.Sprintf("%s alert group from Grafana", status)
}

func grafanaActionURL(payload grafanaWebhookPayload) string {
	for _, alert := range payload.Alerts {
		if alert.DashboardURL != "" {
			return alert.DashboardURL
		}
		if alert.PanelURL != "" {
			return alert.PanelURL
		}
		if alert.GeneratorURL != "" {
			return alert.GeneratorURL
		}
	}
	return strings.TrimSpace(payload.ExternalURL)
}

func grafanaPriority(payload grafanaWebhookPayload) string {
	severity := strings.ToLower(firstMapValue(payload.CommonLabels, "severity", "priority"))
	if severity == "" && len(payload.Alerts) > 0 {
		severity = strings.ToLower(firstMapValue(payload.Alerts[0].Labels, "severity", "priority"))
	}
	switch severity {
	case "critical", "page", "high":
		return notifications.PriorityHigh
	case "info", "low":
		return notifications.PriorityLow
	default:
		return notifications.PriorityNormal
	}
}

func grafanaIdempotencyKey(payload grafanaWebhookPayload) string {
	seed := strings.Join([]string{
		payload.Status,
		payload.GroupKey,
		payload.ExternalURL,
		grafanaResourceID(payload),
	}, "|")
	sum := sha256.Sum256([]byte(seed))
	return "grafana:" + hex.EncodeToString(sum[:])
}

func grafanaResourceID(payload grafanaWebhookPayload) string {
	if groupKey := strings.TrimSpace(payload.GroupKey); groupKey != "" {
		sum := sha256.Sum256([]byte(groupKey))
		return hex.EncodeToString(sum[:])
	}
	if len(payload.Alerts) > 0 && strings.TrimSpace(payload.Alerts[0].Fingerprint) != "" {
		return strings.TrimSpace(payload.Alerts[0].Fingerprint)
	}
	sum := sha256.Sum256([]byte(strings.TrimSpace(payload.Title) + "|" + strings.TrimSpace(payload.Message)))
	return hex.EncodeToString(sum[:])
}

func firstMapValue(values map[string]string, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(values[key]); value != "" {
			return value
		}
	}
	return ""
}

func formatLabels(labels map[string]string) string {
	if len(labels) == 0 {
		return "no labels"
	}
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+labels[key])
	}
	return strings.Join(parts, ", ")
}

func truncateText(value string, max int) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return string(runes[:max])
}
