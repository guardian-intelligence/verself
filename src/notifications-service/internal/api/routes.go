package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/forge-metal/apiwire"
	auth "github.com/forge-metal/auth-middleware"
	"github.com/forge-metal/notifications-service/internal/notifications"
)

const (
	maxIdempotencyKeyLength = 128
	bodyLimitSmallJSON      = 16 << 10
)

var apiTracer = otel.Tracer("notifications-service/internal/api")

type emptyInput struct{}

type listInput struct {
	Limit int `query:"limit" minimum:"1" maximum:"100" default:"20"`
}

type summaryOutput struct {
	Body apiwire.NotificationSummary
}

type listOutput struct {
	Body apiwire.NotificationList
}

type putPreferencesInput struct {
	IdempotencyKey string `header:"Idempotency-Key" required:"true" minLength:"1" maxLength:"128"`
	Body           apiwire.NotificationPutPreferencesRequest
}

type markReadInput struct {
	IdempotencyKey string `header:"Idempotency-Key" required:"true" minLength:"1" maxLength:"128"`
	Body           apiwire.NotificationMarkReadRequest
}

type dismissInput struct {
	NotificationID string `path:"notification_id" format:"uuid"`
	IdempotencyKey string `header:"Idempotency-Key" required:"true" minLength:"1" maxLength:"128"`
}

type clearInput struct {
	IdempotencyKey string `header:"Idempotency-Key" required:"true" minLength:"1" maxLength:"128"`
}

type testInput struct {
	IdempotencyKey string `header:"Idempotency-Key" required:"true" minLength:"1" maxLength:"128"`
	Body           apiwire.NotificationTestRequest
}

type acceptedOutput struct {
	Body apiwire.NotificationAccepted
}

func RegisterRoutes(api huma.API, svc *notifications.Service) {
	register(api, huma.Operation{
		OperationID: "list-notifications",
		Method:      http.MethodGet,
		Path:        "/api/v1/notifications",
		Summary:     "List current human notifications",
	}, "notifications:self:read", listNotifications(svc))

	register(api, huma.Operation{
		OperationID: "get-notification-summary",
		Method:      http.MethodGet,
		Path:        "/api/v1/notifications/summary",
		Summary:     "Get current human notification summary",
	}, "notifications:self:read", getSummary(svc))

	register(api, huma.Operation{
		OperationID:   "put-notification-preferences",
		Method:        http.MethodPut,
		Path:          "/api/v1/notifications/preferences",
		Summary:       "Replace current human notification preferences",
		DefaultStatus: http.StatusOK,
		MaxBodyBytes:  bodyLimitSmallJSON,
	}, "notifications:self:preferences:write", putPreferences(svc))

	register(api, huma.Operation{
		OperationID:   "advance-notification-read-cursor",
		Method:        http.MethodPost,
		Path:          "/api/v1/notifications/read-cursor",
		Summary:       "Advance current human notification read cursor",
		DefaultStatus: http.StatusOK,
		MaxBodyBytes:  bodyLimitSmallJSON,
	}, "notifications:self:write", markRead(svc))

	register(api, huma.Operation{
		OperationID:   "dismiss-notification",
		Method:        http.MethodPost,
		Path:          "/api/v1/notifications/{notification_id}/dismiss",
		Summary:       "Dismiss a current human notification",
		DefaultStatus: http.StatusOK,
	}, "notifications:self:write", dismissNotification(svc))

	register(api, huma.Operation{
		OperationID:   "clear-notifications",
		Method:        http.MethodPost,
		Path:          "/api/v1/notifications/clear",
		Summary:       "Dismiss all current human notifications",
		DefaultStatus: http.StatusOK,
	}, "notifications:self:write", clearNotifications(svc))

	register(api, huma.Operation{
		OperationID:   "publish-test-notification",
		Method:        http.MethodPost,
		Path:          "/api/v1/notifications/test",
		Summary:       "Publish a synthetic notification to the current human",
		DefaultStatus: http.StatusAccepted,
		MaxBodyBytes:  bodyLimitSmallJSON,
	}, "notifications:self:test", publishTestNotification(svc))
}

func register[I, O any](api huma.API, op huma.Operation, permission string, handler func(context.Context, *I) (*O, error)) {
	if op.Extensions == nil {
		op.Extensions = map[string]any{}
	}
	op.Security = []map[string][]string{{"bearerAuth": {}}}
	op.Extensions["x-forge-metal-iam"] = map[string]any{
		"permission":          permission,
		"resource":            "notification_subject",
		"action":              actionFromMethod(op.Method),
		"org_scope":           "token_subject",
		"rate_limit_class":    rateLimitClass(op.Method),
		"audit_event":         "notification." + strings.ReplaceAll(op.OperationID, "-", "."),
		"source_product_area": "Notifications",
		"operation_display":   op.Summary,
		"operation_type":      actionFromMethod(op.Method),
		"event_category":      "notifications",
		"risk_level":          riskLevel(op.Method),
		"data_classification": "controller_personal_data",
	}
	huma.Register(api, op, func(ctx context.Context, input *I) (*O, error) {
		ctx, span := apiTracer.Start(ctx, "notifications.api."+op.OperationID)
		defer span.End()
		identity := auth.FromContext(ctx)
		if identity != nil {
			span.SetAttributes(
				attribute.String("forge_metal.org_id", identity.OrgID),
				attribute.String("forge_metal.subject_id", identity.Subject),
			)
		}
		out, err := handler(ctx, input)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return nil, err
		}
		return out, nil
	})
}

func listNotifications(svc *notifications.Service) func(context.Context, *listInput) (*listOutput, error) {
	return func(ctx context.Context, input *listInput) (*listOutput, error) {
		principal, err := principalFromContext(ctx)
		if err != nil {
			return nil, err
		}
		result, err := svc.List(ctx, principal, notifications.ListRequest{Limit: input.Limit})
		if err != nil {
			return nil, notificationError(ctx, err)
		}
		return &listOutput{Body: listDTO(result)}, nil
	}
}

func getSummary(svc *notifications.Service) func(context.Context, *emptyInput) (*summaryOutput, error) {
	return func(ctx context.Context, _ *emptyInput) (*summaryOutput, error) {
		principal, err := principalFromContext(ctx)
		if err != nil {
			return nil, err
		}
		summary, err := svc.Summary(ctx, principal)
		if err != nil {
			return nil, notificationError(ctx, err)
		}
		return &summaryOutput{Body: summaryDTO(summary)}, nil
	}
}

func putPreferences(svc *notifications.Service) func(context.Context, *putPreferencesInput) (*summaryOutput, error) {
	return func(ctx context.Context, input *putPreferencesInput) (*summaryOutput, error) {
		if err := validateIdempotencyKey(ctx, input.IdempotencyKey); err != nil {
			return nil, err
		}
		principal, err := principalFromContext(ctx)
		if err != nil {
			return nil, err
		}
		summary, err := svc.PutPreferences(ctx, principal, notifications.PutPreferencesRequest{
			Version: input.Body.Version,
			Enabled: input.Body.Enabled,
		})
		if err != nil {
			return nil, notificationError(ctx, err)
		}
		return &summaryOutput{Body: summaryDTO(summary)}, nil
	}
}

func markRead(svc *notifications.Service) func(context.Context, *markReadInput) (*summaryOutput, error) {
	return func(ctx context.Context, input *markReadInput) (*summaryOutput, error) {
		if err := validateIdempotencyKey(ctx, input.IdempotencyKey); err != nil {
			return nil, err
		}
		principal, err := principalFromContext(ctx)
		if err != nil {
			return nil, err
		}
		readUpTo, err := apiwire.ParseInt64(input.Body.ReadUpToSequence)
		if err != nil {
			return nil, badRequest(ctx, "invalid-read-cursor", "read cursor must be a decimal int64", err)
		}
		summary, err := svc.MarkRead(ctx, principal, notifications.MarkReadRequest{ReadUpToSequence: readUpTo})
		if err != nil {
			return nil, notificationError(ctx, err)
		}
		return &summaryOutput{Body: summaryDTO(summary)}, nil
	}
}

func dismissNotification(svc *notifications.Service) func(context.Context, *dismissInput) (*summaryOutput, error) {
	return func(ctx context.Context, input *dismissInput) (*summaryOutput, error) {
		if err := validateIdempotencyKey(ctx, input.IdempotencyKey); err != nil {
			return nil, err
		}
		principal, err := principalFromContext(ctx)
		if err != nil {
			return nil, err
		}
		notificationID, err := uuid.Parse(strings.TrimSpace(input.NotificationID))
		if err != nil {
			return nil, badRequest(ctx, "invalid-notification-id", "notification_id must be a UUID", err)
		}
		summary, err := svc.Dismiss(ctx, principal, notifications.DismissRequest{NotificationID: notificationID})
		if err != nil {
			return nil, notificationError(ctx, err)
		}
		return &summaryOutput{Body: summaryDTO(summary)}, nil
	}
}

func clearNotifications(svc *notifications.Service) func(context.Context, *clearInput) (*summaryOutput, error) {
	return func(ctx context.Context, input *clearInput) (*summaryOutput, error) {
		if err := validateIdempotencyKey(ctx, input.IdempotencyKey); err != nil {
			return nil, err
		}
		principal, err := principalFromContext(ctx)
		if err != nil {
			return nil, err
		}
		summary, err := svc.DismissAll(ctx, principal)
		if err != nil {
			return nil, notificationError(ctx, err)
		}
		return &summaryOutput{Body: summaryDTO(summary)}, nil
	}
}

func publishTestNotification(svc *notifications.Service) func(context.Context, *testInput) (*acceptedOutput, error) {
	return func(ctx context.Context, input *testInput) (*acceptedOutput, error) {
		if err := validateIdempotencyKey(ctx, input.IdempotencyKey); err != nil {
			return nil, err
		}
		principal, err := principalFromContext(ctx)
		if err != nil {
			return nil, err
		}
		accepted, err := svc.PublishSyntheticTest(ctx, principal, notifications.TestRequest{
			Title:     input.Body.Title,
			Body:      input.Body.Body,
			ActionURL: input.Body.ActionURL,
		})
		if err != nil {
			return nil, notificationError(ctx, err)
		}
		return &acceptedOutput{Body: apiwire.NotificationAccepted{
			EventID:     accepted.EventID.String(),
			Traceparent: accepted.Traceparent,
		}}, nil
	}
}

func principalFromContext(ctx context.Context) (notifications.Principal, error) {
	identity := auth.FromContext(ctx)
	if identity == nil {
		return notifications.Principal{}, unauthorized(ctx)
	}
	principal := notifications.Principal{Subject: identity.Subject, OrgID: identity.OrgID, Email: identity.Email, Raw: identity.Raw}
	if err := notifications.ValidatePrincipal(principal); err != nil {
		return notifications.Principal{}, forbidden(ctx, "human-notification-inbox-required", "notification routes require a human subject token")
	}
	return principal, nil
}

func validateIdempotencyKey(ctx context.Context, value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return badRequest(ctx, "idempotency-key-required", "Idempotency-Key is required for this operation", nil)
	}
	if len(value) > maxIdempotencyKeyLength {
		return badRequest(ctx, "idempotency-key-too-long", "Idempotency-Key is too long", nil)
	}
	if strings.ContainsAny(value, "\x00\r\n\t") {
		return badRequest(ctx, "idempotency-key-invalid", "Idempotency-Key contains unsupported characters", nil)
	}
	return nil
}

func listDTO(result notifications.ListResult) apiwire.NotificationList {
	out := apiwire.NotificationList{
		Summary:       summaryDTO(result.Summary),
		Notifications: make([]apiwire.Notification, 0, len(result.Notifications)),
	}
	for _, notification := range result.Notifications {
		out.Notifications = append(out.Notifications, notificationDTO(notification))
	}
	return out
}

func summaryDTO(summary notifications.Summary) apiwire.NotificationSummary {
	return apiwire.NotificationSummary{
		OrgID:              summary.OrgID,
		SubjectID:          summary.SubjectID,
		UnreadCount:        summary.UnreadCount,
		LatestSequence:     strconv.FormatInt(summary.LatestSequence, 10),
		ReadUpToSequence:   strconv.FormatInt(summary.ReadUpToSequence, 10),
		Preferences:        preferencesDTO(summary.Preferences),
		LatestNotification: notificationPtrDTO(summary.LatestNotification),
	}
}

func preferencesDTO(preferences notifications.Preferences) apiwire.NotificationPreferences {
	return apiwire.NotificationPreferences{
		Enabled:   preferences.Enabled,
		Version:   preferences.Version,
		UpdatedAt: preferences.UpdatedAt,
		UpdatedBy: preferences.UpdatedBy,
	}
}

func notificationPtrDTO(notification *notifications.Notification) *apiwire.Notification {
	if notification == nil {
		return nil
	}
	out := notificationDTO(*notification)
	return &out
}

func notificationDTO(notification notifications.Notification) apiwire.Notification {
	return apiwire.Notification{
		NotificationID:     notification.NotificationID.String(),
		OrgID:              notification.OrgID,
		RecipientSubjectID: notification.RecipientSubjectID,
		RecipientSequence:  strconv.FormatInt(notification.RecipientSequence, 10),
		Kind:               notification.Kind,
		Priority:           notification.Priority,
		Title:              notification.Title,
		Body:               notification.Body,
		ActionURL:          notification.ActionURL,
		ResourceKind:       notification.ResourceKind,
		ResourceID:         notification.ResourceID,
		CreatedAt:          notification.CreatedAt,
		ExpiresAt:          notification.ExpiresAt,
		DismissedAt:        notification.DismissedAt,
	}
}

func actionFromMethod(method string) string {
	switch method {
	case http.MethodGet:
		return "read"
	default:
		return "write"
	}
}

func rateLimitClass(method string) string {
	if method == http.MethodGet {
		return "read"
	}
	return "notification_mutation"
}

func riskLevel(method string) string {
	if method == http.MethodGet {
		return "low"
	}
	return "medium"
}
