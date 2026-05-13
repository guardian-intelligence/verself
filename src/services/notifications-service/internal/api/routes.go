package api

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/verself/notifications-service/internal/contractapi"
	"github.com/verself/notifications-service/internal/notifications"
	auth "github.com/verself/service-runtime/auth"
	runtimeiam "github.com/verself/service-runtime/iam"
)

const (
	maxIdempotencyKeyLength = 128
)

var apiTracer = otel.Tracer("notifications-service/internal/api")

func RegisterRoutes(api huma.API, svc *notifications.Service, authorizer runtimeiam.OperationAuthorizer, installationID string) {
	op, policy := notificationOperation(contractapi.ListNotifications.Descriptor, "List current human notifications")
	register(api, authorizer, op, policy, listNotifications(svc, installationID))
	op, policy = notificationOperation(contractapi.GetNotificationSummary.Descriptor, "Get current human notification summary")
	register(api, authorizer, op, policy, getSummary(svc, installationID))
	op, policy = notificationOperation(contractapi.PutNotificationPreferences.Descriptor, "Replace current human notification preferences")
	register(api, authorizer, op, policy, putPreferences(svc, installationID))
	op, policy = notificationOperation(contractapi.AdvanceNotificationReadCursor.Descriptor, "Advance current human notification read cursor")
	register(api, authorizer, op, policy, markRead(svc, installationID))
	op, policy = notificationOperation(contractapi.DismissNotification.Descriptor, "Dismiss a current human notification")
	register(api, authorizer, op, policy, dismissNotification(svc, installationID))
	op, policy = notificationOperation(contractapi.MarkNotificationRead.Descriptor, "Mark one current human notification read")
	register(api, authorizer, op, policy, markNotificationRead(svc, installationID))
	op, policy = notificationOperation(contractapi.ClearNotifications.Descriptor, "Dismiss all current human notifications")
	register(api, authorizer, op, policy, clearNotifications(svc, installationID))
	op, policy = notificationOperation(contractapi.PublishTestNotification.Descriptor, "Publish a synthetic notification to the current human")
	register(api, authorizer, op, policy, publishTestNotification(svc))
}

func notificationOperation(desc contractapi.OperationDescriptor, summary string) (huma.Operation, runtimeiam.OperationPolicy) {
	op := huma.Operation{
		OperationID:   desc.OperationID,
		Method:        desc.Method,
		Path:          desc.Path,
		Summary:       summary,
		DefaultStatus: desc.DefaultStatus,
		Errors:        contractProblemStatuses(desc.Problems),
		Extensions:    map[string]any{"x-verself-contract": contractExtension(desc)},
	}
	return op, operationPolicyFromContract(desc)
}

func register[I, O any](api huma.API, authorizer runtimeiam.OperationAuthorizer, op huma.Operation, policy runtimeiam.OperationPolicy, handler func(context.Context, *I) (*O, error)) {
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
	if decision := apiOperationRateLimiter.Allow(policy.RateLimitClass, operationRateLimitKey(identity), time.Now()); !decision.Allowed {
		return identity, rateLimitExceeded(ctx, decision.RetryAfter)
	}
	return identity, nil
}

func listNotifications(svc *notifications.Service, installationID string) func(context.Context, *contractapi.ListNotificationsInput) (*contractapi.ListNotificationsOutput, error) {
	return func(ctx context.Context, input *contractapi.ListNotificationsInput) (*contractapi.ListNotificationsOutput, error) {
		principal, err := principalFromContext(ctx)
		if err != nil {
			return nil, err
		}
		result, err := svc.List(ctx, principal, notifications.ListRequest{
			Limit:  int(input.Limit),
			Cursor: string(input.Cursor),
		})
		if err != nil {
			return nil, notificationError(ctx, err)
		}
		return &contractapi.ListNotificationsOutput{Body: listContract(result, installationID)}, nil
	}
}

func getSummary(svc *notifications.Service, installationID string) func(context.Context, *contractapi.EmptyInput) (*contractapi.NotificationSummaryOutput, error) {
	return func(ctx context.Context, _ *contractapi.EmptyInput) (*contractapi.NotificationSummaryOutput, error) {
		principal, err := principalFromContext(ctx)
		if err != nil {
			return nil, err
		}
		summary, err := svc.Summary(ctx, principal)
		if err != nil {
			return nil, notificationError(ctx, err)
		}
		return summaryOutput(summary, installationID), nil
	}
}

func putPreferences(svc *notifications.Service, installationID string) func(context.Context, *contractapi.PutNotificationPreferencesInput) (*contractapi.NotificationSummaryOutput, error) {
	return func(ctx context.Context, input *contractapi.PutNotificationPreferencesInput) (*contractapi.NotificationSummaryOutput, error) {
		if err := validateIdempotencyKey(ctx, string(input.IdempotencyKey)); err != nil {
			return nil, err
		}
		principal, err := principalFromContext(ctx)
		if err != nil {
			return nil, err
		}
		version, err := int32FromContractInt(int64(input.Body.Version), "preferences version")
		if err != nil {
			return nil, badRequest(ctx, "preferences-version-out-of-range", "preferences version is outside the supported range", err)
		}
		summary, err := svc.PutPreferences(ctx, principal, notifications.PutPreferencesRequest{
			Version:      version,
			Enabled:      input.Body.Enabled,
			WebEnabled:   input.Body.WebEnabled,
			EmailEnabled: input.Body.EmailEnabled,
			PushEnabled:  input.Body.PushEnabled,
			SMSEnabled:   input.Body.SmsEnabled,
		})
		if err != nil {
			return nil, notificationError(ctx, err)
		}
		return summaryOutput(summary, installationID), nil
	}
}

func markRead(svc *notifications.Service, installationID string) func(context.Context, *contractapi.AdvanceNotificationReadCursorInput) (*contractapi.NotificationSummaryOutput, error) {
	return func(ctx context.Context, input *contractapi.AdvanceNotificationReadCursorInput) (*contractapi.NotificationSummaryOutput, error) {
		if err := validateIdempotencyKey(ctx, string(input.IdempotencyKey)); err != nil {
			return nil, err
		}
		principal, err := principalFromContext(ctx)
		if err != nil {
			return nil, err
		}
		readUpTo, err := strconv.ParseInt(strings.TrimSpace(string(input.Body.ReadUpToSequence)), 10, 64)
		if err != nil {
			return nil, badRequest(ctx, "invalid-read-cursor", "read cursor must be a decimal int64", err)
		}
		summary, err := svc.MarkRead(ctx, principal, notifications.MarkReadRequest{ReadUpToSequence: readUpTo})
		if err != nil {
			return nil, notificationError(ctx, err)
		}
		return summaryOutput(summary, installationID), nil
	}
}

func dismissNotification(svc *notifications.Service, installationID string) func(context.Context, *contractapi.NotificationPathIdempotentInput) (*contractapi.NotificationSummaryOutput, error) {
	return func(ctx context.Context, input *contractapi.NotificationPathIdempotentInput) (*contractapi.NotificationSummaryOutput, error) {
		if err := validateIdempotencyKey(ctx, string(input.IdempotencyKey)); err != nil {
			return nil, err
		}
		principal, err := principalFromContext(ctx)
		if err != nil {
			return nil, err
		}
		notificationID, err := uuid.Parse(strings.TrimSpace(string(input.NotificationID)))
		if err != nil {
			return nil, badRequest(ctx, "invalid-notification-id", "notification_id must be a UUID", err)
		}
		summary, err := svc.Dismiss(ctx, principal, notifications.DismissRequest{NotificationID: notificationID})
		if err != nil {
			return nil, notificationError(ctx, err)
		}
		return summaryOutput(summary, installationID), nil
	}
}

func markNotificationRead(svc *notifications.Service, installationID string) func(context.Context, *contractapi.NotificationPathIdempotentInput) (*contractapi.NotificationSummaryOutput, error) {
	return func(ctx context.Context, input *contractapi.NotificationPathIdempotentInput) (*contractapi.NotificationSummaryOutput, error) {
		if err := validateIdempotencyKey(ctx, string(input.IdempotencyKey)); err != nil {
			return nil, err
		}
		principal, err := principalFromContext(ctx)
		if err != nil {
			return nil, err
		}
		notificationID, err := uuid.Parse(strings.TrimSpace(string(input.NotificationID)))
		if err != nil {
			return nil, badRequest(ctx, "invalid-notification-id", "notification_id must be a UUID", err)
		}
		summary, err := svc.MarkNotificationRead(ctx, principal, notifications.ReadNotificationRequest{NotificationID: notificationID})
		if err != nil {
			return nil, notificationError(ctx, err)
		}
		return summaryOutput(summary, installationID), nil
	}
}

func clearNotifications(svc *notifications.Service, installationID string) func(context.Context, *contractapi.NotificationIdempotentInput) (*contractapi.NotificationSummaryOutput, error) {
	return func(ctx context.Context, input *contractapi.NotificationIdempotentInput) (*contractapi.NotificationSummaryOutput, error) {
		if err := validateIdempotencyKey(ctx, string(input.IdempotencyKey)); err != nil {
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
		return summaryOutput(summary, installationID), nil
	}
}

func publishTestNotification(svc *notifications.Service) func(context.Context, *contractapi.PublishTestNotificationInput) (*contractapi.NotificationAcceptedOutput, error) {
	return func(ctx context.Context, input *contractapi.PublishTestNotificationInput) (*contractapi.NotificationAcceptedOutput, error) {
		if err := validateIdempotencyKey(ctx, string(input.IdempotencyKey)); err != nil {
			return nil, err
		}
		principal, err := principalFromContext(ctx)
		if err != nil {
			return nil, err
		}
		accepted, err := svc.PublishSyntheticTest(ctx, principal, notifications.TestRequest{
			Title:     contractString(input.Body.Title),
			Body:      contractString(input.Body.Body),
			ActionURL: contractString(input.Body.ActionURL),
		})
		if err != nil {
			return nil, notificationError(ctx, err)
		}
		return &contractapi.NotificationAcceptedOutput{Body: contractapi.NotificationAccepted{
			EventID:     contractapi.NotificationID(accepted.EventID.String()),
			Traceparent: optionalContractString[contractapi.TraceParent](accepted.Traceparent),
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

func listContract(result notifications.ListResult, installationID string) contractapi.ListNotificationsOutputBody {
	out := contractapi.ListNotificationsOutputBody{
		Summary:       summaryContract(result.Summary, installationID),
		Notifications: make(contractapi.NotificationsList, 0, len(result.Notifications)),
		NextCursor:    optionalContractString[contractapi.PageToken](result.NextCursor),
	}
	for _, notification := range result.Notifications {
		out.Notifications = append(out.Notifications, notificationContract(notification, installationID))
	}
	return out
}

func summaryOutput(summary notifications.Summary, installationID string) *contractapi.NotificationSummaryOutput {
	return &contractapi.NotificationSummaryOutput{Body: summaryContract(summary, installationID)}
}

func summaryContract(summary notifications.Summary, installationID string) contractapi.NotificationSummary {
	return contractapi.NotificationSummary{
		OrgID:              contractapi.OrgID(summary.OrgID),
		SubjectID:          contractapi.SubjectID(summary.SubjectID),
		UnreadCount:        contractapi.UnreadCount(summary.UnreadCount),
		LatestSequence:     contractapi.DecimalInt64(strconv.FormatInt(summary.LatestSequence, 10)),
		ReadUpToSequence:   contractapi.DecimalInt64(strconv.FormatInt(summary.ReadUpToSequence, 10)),
		Preferences:        preferencesContract(summary.Preferences),
		LatestNotification: notificationPtrContract(summary.LatestNotification, installationID),
	}
}

func preferencesContract(preferences notifications.Preferences) contractapi.NotificationPreferencesView {
	return contractapi.NotificationPreferencesView{
		Enabled:      preferences.Enabled,
		WebEnabled:   preferences.WebEnabled,
		EmailEnabled: preferences.EmailEnabled,
		PushEnabled:  preferences.PushEnabled,
		SmsEnabled:   preferences.SMSEnabled,
		Version:      contractapi.PreferenceVersion(preferences.Version),
		UpdatedAt:    formatContractTime(preferences.UpdatedAt),
		UpdatedBy:    optionalContractString[contractapi.SubjectID](preferences.UpdatedBy),
	}
}

func notificationPtrContract(notification *notifications.Notification, installationID string) *contractapi.NotificationRecord {
	if notification == nil {
		return nil
	}
	out := notificationContract(*notification, installationID)
	return &out
}

func notificationContract(notification notifications.Notification, installationID string) contractapi.NotificationRecord {
	return contractapi.NotificationRecord{
		NotificationID:     contractapi.NotificationID(notification.NotificationID.String()),
		ResourceName:       notificationResourceName(installationID, notification.OrgID, notification.NotificationID.String()),
		OrgID:              contractapi.OrgID(notification.OrgID),
		RecipientSubjectID: contractapi.SubjectID(notification.RecipientSubjectID),
		RecipientSequence:  contractapi.DecimalInt64(strconv.FormatInt(notification.RecipientSequence, 10)),
		Kind:               contractapi.NotificationKind(notification.Kind),
		Priority:           contractapi.NotificationPriority(notification.Priority),
		Title:              contractapi.NotificationTitle(notification.Title),
		Body:               contractapi.RequiredNotificationBody(notification.Body),
		ActionURL:          optionalContractString[contractapi.ActionURL](notification.ActionURL),
		TargetResourceName: notificationTargetResourceName(notification),
		CreatedAt:          formatContractTime(notification.CreatedAt),
		ExpiresAt:          optionalContractTime(notification.ExpiresAt),
		ReadAt:             optionalContractTime(notification.ReadAt),
		DismissedAt:        optionalContractTime(notification.DismissedAt),
	}
}

func notificationResourceName(installationID string, orgID string, notificationID string) contractapi.ResourceName {
	return contractapi.ResourceName(fmt.Sprintf("urn:verself:%s:orgs/%s/notifications/%s", strings.TrimSpace(installationID), strings.TrimSpace(orgID), strings.TrimSpace(notificationID)))
}

func notificationTargetResourceName(notification notifications.Notification) *contractapi.ResourceName {
	switch strings.TrimSpace(notification.ResourceKind) {
	case "resource_name", "target_resource_name":
		return optionalContractString[contractapi.ResourceName](notification.ResourceID)
	default:
		return nil
	}
}

func contractString[T ~string](value *T) string {
	if value == nil {
		return ""
	}
	return string(*value)
}

func optionalContractString[T ~string](value string) *T {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	out := T(value)
	return &out
}

func optionalContractTime(value *time.Time) *string {
	if value == nil || value.IsZero() {
		return nil
	}
	out := formatContractTime(*value)
	return &out
}

func formatContractTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func int32FromContractInt(value int64, field string) (int32, error) {
	const maxInt32 = int64(1<<31 - 1)
	if value < 0 || value > maxInt32 {
		return 0, fmt.Errorf("%s is outside int32 range", field)
	}
	return int32(value), nil
}

func operationRateLimitKey(identity *auth.Identity) string {
	if identity == nil {
		return "anonymous"
	}
	return strings.TrimSpace(identity.OrgID) + "\x00" + strings.TrimSpace(identity.Subject)
}

var apiOperationRateLimiter = runtimeiam.NewFixedWindowOperationRateLimiter(map[runtimeiam.RateLimitClass]runtimeiam.RateLimitRule{
	"read":                  {Limit: 6000, Window: time.Minute},
	"notification_mutation": {Limit: 600, Window: time.Minute},
})

func rateLimitExceeded(ctx context.Context, retryAfter time.Duration) error {
	err := tooManyRequests(ctx, "rate-limit-exceeded", "rate limit exceeded")
	if retryAfter <= 0 {
		return err
	}
	headers := http.Header{}
	headers.Set("Retry-After", strconv.FormatInt(int64(retryAfter.Seconds()), 10))
	return huma.ErrorWithHeaders(err, headers)
}
