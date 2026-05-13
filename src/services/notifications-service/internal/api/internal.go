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

	"github.com/danielgtaylor/huma/v2"

	"github.com/verself/notifications-service/internal/internalcontractapi"
	"github.com/verself/notifications-service/internal/notifications"
	workloadauth "github.com/verself/service-runtime/workload"
)

type InternalConfig struct {
	Service            *notifications.Service
	PlatformAlertOrgID string
	PlatformAlertEmail string
}

func RegisterInternalRoutes(api huma.API, cfg InternalConfig) {
	op := internalNotificationContract(internalcontractapi.TriggerNotificationWorkflow.Descriptor, "Trigger a notification workflow")
	huma.Register(api, op, triggerWorkflow(cfg.Service))
	op = internalNotificationContract(internalcontractapi.ReceiveGrafanaAlertWebhook.Descriptor, "Receive a Grafana alert webhook")
	huma.Register(api, op, grafanaAlertWebhook(cfg))
}

func internalNotificationContract(desc internalcontractapi.OperationDescriptor, summary string) huma.Operation {
	return huma.Operation{
		OperationID:   desc.OperationID,
		Method:        desc.Method,
		Path:          desc.Path,
		Summary:       summary,
		DefaultStatus: desc.DefaultStatus,
		MaxBodyBytes:  desc.RequestBodyMaxBytes,
		Errors:        internalContractProblemStatuses(desc.Problems),
		Security:      []map[string][]string{{"mutualTLS": {}}},
		Extensions:    map[string]any{"x-verself-contract": internalContractExtension(desc), "x-verself-iam": operationPolicyFromInternalContract(desc).OpenAPIExtension()},
	}
}

func triggerWorkflow(svc *notifications.Service) func(context.Context, *internalcontractapi.TriggerNotificationWorkflowInput) (*internalcontractapi.NotificationWorkflowTriggerOutput, error) {
	return func(ctx context.Context, input *internalcontractapi.TriggerNotificationWorkflowInput) (*internalcontractapi.NotificationWorkflowTriggerOutput, error) {
		if err := validateIdempotencyKey(ctx, string(input.IdempotencyKey)); err != nil {
			return nil, err
		}
		peerID, ok := workloadauth.PeerIDFromContext(ctx)
		if !ok {
			return nil, unauthorized(ctx)
		}
		recipients := make([]notifications.WorkflowRecipient, 0, len(input.Body.Recipients))
		for _, recipient := range input.Body.Recipients {
			recipients = append(recipients, notifications.WorkflowRecipient{
				SubjectID: contractString(recipient.SubjectID),
				Email:     contractString(recipient.Email),
			})
		}
		data, err := contractDocument(input.Body.Data)
		if err != nil {
			return nil, badRequest(ctx, "workflow-data-invalid", "workflow data is invalid", err)
		}
		targetResourceName := contractString(input.Body.TargetResourceName)
		result, err := svc.TriggerWorkflow(ctx, notifications.WorkflowTriggerRequest{
			WorkflowKey:    string(input.WorkflowKey),
			OrgID:          string(input.Body.OrgID),
			TriggeredBy:    peerID.String(),
			IdempotencyKey: string(input.IdempotencyKey),
			Recipients:     recipients,
			Title:          contractString(input.Body.Title),
			Body:           string(input.Body.Body),
			ActionURL:      contractString(input.Body.ActionURL),
			Priority:       contractString(input.Body.Priority),
			ResourceKind:   notificationResourceKind(targetResourceName),
			ResourceID:     targetResourceName,
			Data:           data,
			Traceparent:    contractString(input.Body.Traceparent),
		})
		if err != nil {
			return nil, notificationError(ctx, err)
		}
		return workflowTriggerOutput(result), nil
	}
}

func contractDocument(value *map[string]any) (json.RawMessage, error) {
	if value == nil {
		return nil, nil
	}
	return json.Marshal(*value)
}

func notificationResourceKind(name string) string {
	if strings.TrimSpace(name) == "" {
		return ""
	}
	return "resource_name"
}

func grafanaAlertWebhook(cfg InternalConfig) func(context.Context, *internalcontractapi.ReceiveGrafanaAlertWebhookInput) (*internalcontractapi.NotificationWorkflowTriggerOutput, error) {
	return func(ctx context.Context, input *internalcontractapi.ReceiveGrafanaAlertWebhookInput) (*internalcontractapi.NotificationWorkflowTriggerOutput, error) {
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
		status := strings.ToLower(strings.TrimSpace(contractString(payload.Status)))
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
			Title:        truncateText(grafanaTitle(payload, status), 240),
			Body:         truncateText(grafanaBody(payload, status), 4096),
			ActionURL:    truncateText(grafanaActionURL(payload), 8192),
			Priority:     grafanaPriority(payload),
			ResourceKind: "grafana_alert_group",
			ResourceID:   grafanaResourceID(payload),
			Data:         body,
			Traceparent:  "",
		})
		if err != nil {
			return nil, notificationError(ctx, err)
		}
		return workflowTriggerOutput(result), nil
	}
}

func workflowTriggerOutput(result notifications.WorkflowTriggerResult) *internalcontractapi.NotificationWorkflowTriggerOutput {
	return &internalcontractapi.NotificationWorkflowTriggerOutput{Body: internalcontractapi.NotificationWorkflowTriggerResult{
		WorkflowRunID:            internalcontractapi.WorkflowRunID(result.WorkflowRunID.String()),
		WorkflowKey:              internalcontractapi.WorkflowKey(result.WorkflowKey),
		OrgID:                    internalcontractapi.OrgID(result.OrgID),
		AcceptedAt:               formatContractTime(result.AcceptedAt),
		RecipientCount:           internalcontractapi.NotificationSmallCount(result.RecipientCount),
		WebNotificationsAccepted: internalcontractapi.NotificationSmallCount(result.WebNotificationsAccepted),
		EmailDeliveriesQueued:    internalcontractapi.NotificationSmallCount(result.EmailDeliveriesQueued),
		SuppressedCount:          internalcontractapi.NotificationSmallCount(result.SuppressedCount),
		Traceparent:              optionalContractString[internalcontractapi.TraceParent](result.Traceparent),
	}}
}

func grafanaTitle(payload internalcontractapi.GrafanaWebhookPayload, status string) string {
	if title := strings.TrimSpace(contractString(payload.Title)); title != "" {
		return title
	}
	alertname := firstMapValue(grafanaLabelMap(payload.CommonLabels), "alertname", "rule", "name")
	alerts := grafanaAlerts(payload)
	if alertname == "" && len(alerts) > 0 {
		alertname = firstMapValue(grafanaLabelMap(alerts[0].Labels), "alertname", "rule", "name")
	}
	if alertname == "" {
		alertname = "Grafana alert"
	}
	return fmt.Sprintf("%s: %s", strings.ToUpper(status), alertname)
}

func grafanaBody(payload internalcontractapi.GrafanaWebhookPayload, status string) string {
	if message := strings.TrimSpace(contractString(payload.Message)); message != "" {
		return message
	}
	summary := firstMapValue(grafanaLabelMap(payload.CommonAnnotations), "summary", "description")
	if summary != "" {
		return summary
	}
	alerts := grafanaAlerts(payload)
	if len(alerts) > 0 {
		alert := alerts[0]
		summary = firstMapValue(grafanaLabelMap(alert.Annotations), "summary", "description")
		if summary != "" {
			return summary
		}
		return fmt.Sprintf("%s alert from Grafana: %s", status, formatLabels(grafanaLabelMap(alert.Labels)))
	}
	return fmt.Sprintf("%s alert group from Grafana", status)
}

func grafanaActionURL(payload internalcontractapi.GrafanaWebhookPayload) string {
	for _, alert := range grafanaAlerts(payload) {
		if value := strings.TrimSpace(contractString(alert.DashboardURL)); value != "" {
			return value
		}
		if value := strings.TrimSpace(contractString(alert.PanelURL)); value != "" {
			return value
		}
		if value := strings.TrimSpace(contractString(alert.GeneratorURL)); value != "" {
			return value
		}
	}
	return strings.TrimSpace(contractString(payload.ExternalURL))
}

func grafanaPriority(payload internalcontractapi.GrafanaWebhookPayload) string {
	severity := strings.ToLower(firstMapValue(grafanaLabelMap(payload.CommonLabels), "severity", "priority"))
	alerts := grafanaAlerts(payload)
	if severity == "" && len(alerts) > 0 {
		severity = strings.ToLower(firstMapValue(grafanaLabelMap(alerts[0].Labels), "severity", "priority"))
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

func grafanaIdempotencyKey(payload internalcontractapi.GrafanaWebhookPayload) string {
	seed := strings.Join([]string{
		contractString(payload.Status),
		contractString(payload.GroupKey),
		contractString(payload.ExternalURL),
		grafanaResourceID(payload),
	}, "|")
	sum := sha256.Sum256([]byte(seed))
	return "grafana:" + hex.EncodeToString(sum[:])
}

func grafanaResourceID(payload internalcontractapi.GrafanaWebhookPayload) string {
	if groupKey := strings.TrimSpace(contractString(payload.GroupKey)); groupKey != "" {
		sum := sha256.Sum256([]byte(groupKey))
		return hex.EncodeToString(sum[:])
	}
	alerts := grafanaAlerts(payload)
	if len(alerts) > 0 && strings.TrimSpace(contractString(alerts[0].Fingerprint)) != "" {
		return strings.TrimSpace(contractString(alerts[0].Fingerprint))
	}
	sum := sha256.Sum256([]byte(strings.TrimSpace(contractString(payload.Title)) + "|" + strings.TrimSpace(contractString(payload.Message))))
	return hex.EncodeToString(sum[:])
}

func grafanaAlerts(payload internalcontractapi.GrafanaWebhookPayload) internalcontractapi.GrafanaAlerts {
	if payload.Alerts == nil {
		return nil
	}
	return *payload.Alerts
}

func grafanaLabelMap(values *internalcontractapi.GrafanaLabelMap) map[string]string {
	if values == nil {
		return nil
	}
	out := make(map[string]string, len(*values))
	for key, value := range *values {
		out[key] = string(value)
	}
	return out
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
