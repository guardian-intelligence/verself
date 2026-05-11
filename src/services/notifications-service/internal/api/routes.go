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

	"github.com/verself/domain-transfer-objects"
	"github.com/verself/notifications-service/internal/notifications"
	auth "github.com/verself/service-runtime/auth"
	runtimeiam "github.com/verself/service-runtime/iam"
)

const (
	maxIdempotencyKeyLength = 128
	bodyLimitNoBody         = 1024
	bodyLimitSmallJSON      = 16 << 10
)

var apiTracer = otel.Tracer("notifications-service/internal/api")

type emptyInput struct{}

type listInput struct {
	Limit int `query:"limit" minimum:"1" maximum:"100" default:"20"`
}

type summaryOutput struct {
	Body dto.NotificationSummary
}

type listOutput struct {
	Body dto.NotificationList
}

type putPreferencesInput struct {
	IdempotencyKey string `header:"Idempotency-Key" required:"true" minLength:"1" maxLength:"128"`
	Body           dto.NotificationPutPreferencesRequest
}

type markReadInput struct {
	IdempotencyKey string `header:"Idempotency-Key" required:"true" minLength:"1" maxLength:"128"`
	Body           dto.NotificationMarkReadRequest
}

type dismissInput struct {
	NotificationID string `path:"notification_id" format:"uuid"`
	IdempotencyKey string `header:"Idempotency-Key" required:"true" minLength:"1" maxLength:"128"`
}

type markNotificationInput struct {
	NotificationID string `path:"notification_id" format:"uuid"`
	IdempotencyKey string `header:"Idempotency-Key" required:"true" minLength:"1" maxLength:"128"`
}

type clearInput struct {
	IdempotencyKey string `header:"Idempotency-Key" required:"true" minLength:"1" maxLength:"128"`
}

type testInput struct {
	IdempotencyKey string `header:"Idempotency-Key" required:"true" minLength:"1" maxLength:"128"`
	Body           dto.NotificationTestRequest
}

type acceptedOutput struct {
	Body dto.NotificationAccepted
}

func RegisterRoutes(api huma.API, svc *notifications.Service, authorizer runtimeiam.OperationAuthorizer) {
	register(api, huma.Operation{
		OperationID: "list-notifications",
		Method:      http.MethodGet,
		Path:        "/api/v1/notifications",
		Summary:     "List current human notifications",
	}, authorizer, runtimeiam.OperationPolicy{
		Permission:     "notifications:self:read",
		Resource:       "notification_subject",
		Action:         runtimeiam.ActionList,
		OrgScope:       runtimeiam.OrgScopeTokenSubject,
		RateLimitClass: "read",
		AuditEvent:     "notifications.list",
	}, listNotifications(svc))

	register(api, huma.Operation{
		OperationID: "get-notification-summary",
		Method:      http.MethodGet,
		Path:        "/api/v1/notifications/summary",
		Summary:     "Get current human notification summary",
	}, authorizer, runtimeiam.OperationPolicy{
		Permission:     "notifications:self:read",
		Resource:       "notification_subject",
		Action:         runtimeiam.ActionRead,
		OrgScope:       runtimeiam.OrgScopeTokenSubject,
		RateLimitClass: "read",
		AuditEvent:     "notifications.summary.read",
	}, getSummary(svc))

	register(api, huma.Operation{
		OperationID:   "put-notification-preferences",
		Method:        http.MethodPut,
		Path:          "/api/v1/notifications/preferences",
		Summary:       "Replace current human notification preferences",
		DefaultStatus: http.StatusOK,
		MaxBodyBytes:  bodyLimitSmallJSON,
	}, authorizer, runtimeiam.OperationPolicy{
		Permission:     "notifications:self:preferences:write",
		Resource:       "notification_preferences",
		Action:         runtimeiam.ActionWrite,
		OrgScope:       runtimeiam.OrgScopeTokenSubject,
		RateLimitClass: "notification_mutation",
		Idempotency:    runtimeiam.IdempotencyHeaderKey,
		AuditEvent:     "notifications.preferences.write",
		BodyLimitBytes: bodyLimitSmallJSON,
	}, putPreferences(svc))

	register(api, huma.Operation{
		OperationID:   "advance-notification-read-cursor",
		Method:        http.MethodPost,
		Path:          "/api/v1/notifications/read-cursor",
		Summary:       "Advance current human notification read cursor",
		DefaultStatus: http.StatusOK,
		MaxBodyBytes:  bodyLimitSmallJSON,
	}, authorizer, runtimeiam.OperationPolicy{
		Permission:     "notifications:self:write",
		Resource:       "notification_subject",
		Action:         runtimeiam.ActionWrite,
		OrgScope:       runtimeiam.OrgScopeTokenSubject,
		RateLimitClass: "notification_mutation",
		Idempotency:    runtimeiam.IdempotencyHeaderKey,
		AuditEvent:     "notifications.read_cursor.advance",
		BodyLimitBytes: bodyLimitSmallJSON,
	}, markRead(svc))

	register(api, huma.Operation{
		OperationID:   "dismiss-notification",
		Method:        http.MethodPost,
		Path:          "/api/v1/notifications/{notification_id}/dismiss",
		Summary:       "Dismiss a current human notification",
		DefaultStatus: http.StatusOK,
	}, authorizer, runtimeiam.OperationPolicy{
		Permission:     "notifications:self:write",
		Resource:       "notification_subject",
		Action:         runtimeiam.ActionWrite,
		OrgScope:       runtimeiam.OrgScopeTokenSubject,
		RateLimitClass: "notification_mutation",
		Idempotency:    runtimeiam.IdempotencyHeaderKey,
		AuditEvent:     "notifications.dismiss",
		BodyLimitBytes: bodyLimitNoBody,
	}, dismissNotification(svc))

	register(api, huma.Operation{
		OperationID:   "mark-notification-read",
		Method:        http.MethodPost,
		Path:          "/api/v1/notifications/{notification_id}/read",
		Summary:       "Mark one current human notification read",
		DefaultStatus: http.StatusOK,
	}, authorizer, runtimeiam.OperationPolicy{
		Permission:     "notifications:self:write",
		Resource:       "notification_subject",
		Action:         runtimeiam.ActionWrite,
		OrgScope:       runtimeiam.OrgScopeTokenSubject,
		RateLimitClass: "notification_mutation",
		Idempotency:    runtimeiam.IdempotencyHeaderKey,
		AuditEvent:     "notifications.mark_read",
		BodyLimitBytes: bodyLimitNoBody,
	}, markNotificationRead(svc))

	register(api, huma.Operation{
		OperationID:   "clear-notifications",
		Method:        http.MethodPost,
		Path:          "/api/v1/notifications/clear",
		Summary:       "Dismiss all current human notifications",
		DefaultStatus: http.StatusOK,
	}, authorizer, runtimeiam.OperationPolicy{
		Permission:     "notifications:self:write",
		Resource:       "notification_subject",
		Action:         runtimeiam.ActionWrite,
		OrgScope:       runtimeiam.OrgScopeTokenSubject,
		RateLimitClass: "notification_mutation",
		Idempotency:    runtimeiam.IdempotencyHeaderKey,
		AuditEvent:     "notifications.clear",
		BodyLimitBytes: bodyLimitNoBody,
	}, clearNotifications(svc))

	register(api, huma.Operation{
		OperationID:   "publish-test-notification",
		Method:        http.MethodPost,
		Path:          "/api/v1/notifications/test",
		Summary:       "Publish a synthetic notification to the current human",
		DefaultStatus: http.StatusAccepted,
		MaxBodyBytes:  bodyLimitSmallJSON,
	}, authorizer, runtimeiam.OperationPolicy{
		Permission:     "notifications:self:test",
		Resource:       "notification_subject",
		Action:         runtimeiam.ActionTest,
		OrgScope:       runtimeiam.OrgScopeTokenSubject,
		RateLimitClass: "notification_mutation",
		Idempotency:    runtimeiam.IdempotencyHeaderKey,
		AuditEvent:     "notifications.test.publish",
		BodyLimitBytes: bodyLimitSmallJSON,
	}, publishTestNotification(svc))
}

func register[I, O any](api huma.API, op huma.Operation, authorizer runtimeiam.OperationAuthorizer, policy runtimeiam.OperationPolicy, handler func(context.Context, *I) (*O, error)) {
	if err := policy.ValidateHTTPOperation(op.Method, op.OperationID); err != nil {
		panic(err)
	}
	if policy.BodyLimitBytes > 0 {
		op.MaxBodyBytes = policy.BodyLimitBytes
	}
	if op.Extensions == nil {
		op.Extensions = map[string]any{}
	}
	op.Security = []map[string][]string{{"bearerAuth": {}}}
	op.Extensions["x-verself-iam"] = policy.OpenAPIExtension()
	huma.Register(api, op, func(ctx context.Context, input *I) (*O, error) {
		ctx, span := apiTracer.Start(ctx, "notifications.api."+op.OperationID)
		defer span.End()
		span.SetAttributes(
			attribute.String("notifications.permission", string(policy.Permission)),
			attribute.String("notifications.resource", string(policy.Resource)),
			attribute.String("notifications.action", string(policy.Action)),
			attribute.String("notifications.audit_event", string(policy.AuditEvent)),
		)
		identity, err := enforceOperationPolicy(ctx, authorizer, policy)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return nil, err
		}
		if identity != nil {
			span.SetAttributes(
				attribute.String("verself.org_id", identity.OrgID),
				attribute.String("verself.subject_id", identity.Subject),
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

func enforceOperationPolicy(ctx context.Context, authorizer runtimeiam.OperationAuthorizer, policy runtimeiam.OperationPolicy) (*auth.Identity, error) {
	identity := auth.FromContext(ctx)
	if identity == nil {
		return nil, unauthorized(ctx)
	}
	principal := notifications.Principal{Subject: identity.Subject, OrgID: identity.OrgID, Email: identity.Email, Raw: identity.Raw}
	if err := notifications.ValidatePrincipal(principal); err != nil {
		return identity, forbidden(ctx, "human-notification-inbox-required", "notification routes require a human subject token")
	}
	if authorizer == nil {
		return identity, problem(ctx, http.StatusServiceUnavailable, "iam-authorizer-unavailable", "IAM authorizer unavailable", runtimeiam.ErrAuthorizerUnavailable)
	}
	decision, err := authorizer.AuthorizeOperation(ctx, identity, policy)
	if err != nil {
		return identity, problem(ctx, http.StatusServiceUnavailable, "iam-authorizer-unavailable", "IAM authorization check failed", err)
	}
	if !decision.Allowed {
		return identity, forbidden(ctx, "permission-denied", "missing required notification permission")
	}
	return identity, nil
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
			Version:      input.Body.Version,
			Enabled:      input.Body.Enabled,
			WebEnabled:   input.Body.WebEnabled,
			EmailEnabled: input.Body.EmailEnabled,
			PushEnabled:  input.Body.PushEnabled,
			SMSEnabled:   input.Body.SMSEnabled,
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
		readUpTo, err := dto.ParseInt64(input.Body.ReadUpToSequence)
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

func markNotificationRead(svc *notifications.Service) func(context.Context, *markNotificationInput) (*summaryOutput, error) {
	return func(ctx context.Context, input *markNotificationInput) (*summaryOutput, error) {
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
		summary, err := svc.MarkNotificationRead(ctx, principal, notifications.ReadNotificationRequest{NotificationID: notificationID})
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
		return &acceptedOutput{Body: dto.NotificationAccepted{
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

func listDTO(result notifications.ListResult) dto.NotificationList {
	out := dto.NotificationList{
		Summary:       summaryDTO(result.Summary),
		Notifications: make([]dto.Notification, 0, len(result.Notifications)),
	}
	for _, notification := range result.Notifications {
		out.Notifications = append(out.Notifications, notificationDTO(notification))
	}
	return out
}

func summaryDTO(summary notifications.Summary) dto.NotificationSummary {
	return dto.NotificationSummary{
		OrgID:              summary.OrgID,
		SubjectID:          summary.SubjectID,
		UnreadCount:        summary.UnreadCount,
		LatestSequence:     strconv.FormatInt(summary.LatestSequence, 10),
		ReadUpToSequence:   strconv.FormatInt(summary.ReadUpToSequence, 10),
		Preferences:        preferencesDTO(summary.Preferences),
		LatestNotification: notificationPtrDTO(summary.LatestNotification),
	}
}

func preferencesDTO(preferences notifications.Preferences) dto.NotificationPreferences {
	return dto.NotificationPreferences{
		Enabled:      preferences.Enabled,
		WebEnabled:   preferences.WebEnabled,
		EmailEnabled: preferences.EmailEnabled,
		PushEnabled:  preferences.PushEnabled,
		SMSEnabled:   preferences.SMSEnabled,
		Version:      preferences.Version,
		UpdatedAt:    preferences.UpdatedAt,
		UpdatedBy:    preferences.UpdatedBy,
	}
}

func notificationPtrDTO(notification *notifications.Notification) *dto.Notification {
	if notification == nil {
		return nil
	}
	out := notificationDTO(*notification)
	return &out
}

func notificationDTO(notification notifications.Notification) dto.Notification {
	return dto.Notification{
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
		ReadAt:             notification.ReadAt,
		DismissedAt:        notification.DismissedAt,
	}
}
