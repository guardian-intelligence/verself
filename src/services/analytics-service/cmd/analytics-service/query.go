package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/verself/analytics-service/internal/analytics"
	iamclient "github.com/verself/iam-service/client"
	auth "github.com/verself/service-runtime/auth"
	runtimeiam "github.com/verself/service-runtime/iam"
)

const (
	analyticsDatasetResourceType = "analytics_dataset"
	projectResourceType          = "project"
	orgResourceType              = "org"
	parentProjectRelation        = "parent_project"
	parentOrgRelation            = "parent_org"
)

func queryEventsHandler(svc *analytics.Service, authorizer runtimeiam.ResourceAuthorizer, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed")
			return
		}
		identity := auth.FromContext(r.Context())
		if identity == nil {
			writeJSONError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		request, err := queryEventsRequestFromHTTP(r, identity.OrgID, svc.Dataset.Environment)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, compactErrorCode(err))
			return
		}
		resourcePermission := "read"
		operationPermission := analytics.PermissionDatasetRead
		if request.Raw {
			resourcePermission = "read_raw"
			operationPermission = analytics.PermissionDatasetReadRaw
		}
		subjectType, subjectID := accessSubject(identity)
		access := analytics.AccessEventRow{
			OrgID:               request.OrgID,
			ProjectID:           request.ProjectID,
			DatasetID:           request.DatasetID,
			Environment:         request.Environment,
			SubjectType:         subjectType,
			SubjectID:           subjectID,
			OperationPermission: operationPermission,
			ResourcePermission:  resourcePermission,
		}
		decision, err := authorizeAnalyticsDataset(r.Context(), authorizer, identity, request, operationPermission, resourcePermission)
		if err != nil {
			access.Outcome = analytics.OutcomeError
			access.ErrorCode = compactErrorCode(err)
			recordAccessEvent(r.Context(), svc, access, logger)
			writeJSONError(w, http.StatusServiceUnavailable, "iam_authorizer_unavailable")
			return
		}
		if decision.SubjectType != "" {
			access.SubjectType = decision.SubjectType
		}
		if decision.SubjectID != "" {
			access.SubjectID = decision.SubjectID
		}
		if !decision.Allowed {
			access.Outcome = analytics.OutcomeDenied
			recordAccessEvent(r.Context(), svc, access, logger)
			writeJSONError(w, http.StatusForbidden, "permission_denied")
			return
		}
		page, err := svc.QueryEvents(r.Context(), request)
		if err != nil {
			access.Outcome = analytics.OutcomeError
			access.ErrorCode = compactErrorCode(err)
			recordAccessEvent(r.Context(), svc, access, logger)
			writeJSONError(w, http.StatusInternalServerError, "analytics_query_failed")
			return
		}
		access.Outcome = analytics.OutcomeAllowed
		access.ResultCount = uint32(len(page.Events))
		recordAccessEvent(r.Context(), svc, access, logger)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(page); err != nil {
			logger.WarnContext(r.Context(), "analytics query response encode failed", "error", err)
		}
	})
}

func authorizeAnalyticsDataset(ctx context.Context, authorizer runtimeiam.ResourceAuthorizer, identity *auth.Identity, request analytics.QueryRequest, operationPermission, resourcePermission string) (runtimeiam.ResourceAuthorizationDecision, error) {
	if authorizer == nil {
		return runtimeiam.ResourceAuthorizationDecision{}, runtimeiam.ErrAuthorizerUnavailable
	}
	projectResource := runtimeiam.ResourceRef{Type: projectResourceType, ID: request.ProjectID}
	datasetResource := runtimeiam.ResourceRef{Type: analyticsDatasetResourceType, ID: analytics.DatasetResourceID(request.ProjectID, request.DatasetID)}
	// The analytics service materializes parent edges at read time until projects-service owns project Zanzibar writes.
	if _, err := authorizer.EnsureResourceParent(ctx, runtimeiam.ResourceParentEdgeRequest{
		OrgID:     request.OrgID,
		Resource:  projectResource,
		Relation:  parentOrgRelation,
		Parent:    runtimeiam.ResourceRef{Type: orgResourceType, ID: request.OrgID},
		Operation: "analytics.project.parent_org.ensure",
	}); err != nil {
		return runtimeiam.ResourceAuthorizationDecision{}, err
	}
	if _, err := authorizer.EnsureResourceParent(ctx, runtimeiam.ResourceParentEdgeRequest{
		OrgID:     request.OrgID,
		Resource:  datasetResource,
		Relation:  parentProjectRelation,
		Parent:    projectResource,
		Operation: "analytics.dataset.parent_project.ensure",
	}); err != nil {
		return runtimeiam.ResourceAuthorizationDecision{}, err
	}
	return authorizer.AuthorizeResource(ctx, identity, runtimeiam.ResourceAuthorizationRequest{
		OrgID:               request.OrgID,
		OperationPermission: runtimeiam.Permission(operationPermission),
		Resource:            datasetResource,
		ResourcePermission:  resourcePermission,
	})
}

func queryEventsRequestFromHTTP(r *http.Request, orgID, defaultEnvironment string) (analytics.QueryRequest, error) {
	projectID, datasetID, ok := parseQueryEventsPath(r.URL.Path)
	if !ok {
		return analytics.QueryRequest{}, fmt.Errorf("invalid analytics events path")
	}
	query := r.URL.Query()
	limit, err := parseOptionalInt(query.Get("limit"))
	if err != nil {
		return analytics.QueryRequest{}, fmt.Errorf("limit is invalid")
	}
	from, err := parseOptionalTime(query.Get("from"))
	if err != nil {
		return analytics.QueryRequest{}, fmt.Errorf("from is invalid")
	}
	to, err := parseOptionalTime(query.Get("to"))
	if err != nil {
		return analytics.QueryRequest{}, fmt.Errorf("to is invalid")
	}
	raw, err := parseOptionalBool(firstNonEmpty(query.Get("raw"), query.Get("include_raw")))
	if err != nil {
		return analytics.QueryRequest{}, fmt.Errorf("raw is invalid")
	}
	return analytics.QueryRequest{
		OrgID:       strings.TrimSpace(orgID),
		ProjectID:   projectID,
		DatasetID:   datasetID,
		Environment: firstNonEmpty(query.Get("environment"), defaultEnvironment),
		Limit:       limit,
		From:        from,
		To:          to,
		EventName:   query.Get("event_name"),
		TraceID:     query.Get("trace_id"),
		Raw:         raw,
	}, nil
}

func parseQueryEventsPath(path string) (string, string, bool) {
	const prefix = "/api/v1/projects/"
	if !strings.HasPrefix(path, prefix) {
		return "", "", false
	}
	parts := strings.Split(strings.TrimPrefix(path, prefix), "/")
	if len(parts) != 5 || parts[1] != "analytics" || parts[2] != "datasets" || parts[4] != "events" {
		return "", "", false
	}
	projectID, err := url.PathUnescape(parts[0])
	if err != nil {
		return "", "", false
	}
	datasetID, err := url.PathUnescape(parts[3])
	if err != nil {
		return "", "", false
	}
	projectID = strings.TrimSpace(projectID)
	datasetID = strings.TrimSpace(datasetID)
	return projectID, datasetID, projectID != "" && datasetID != ""
}

func parseOptionalInt(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, err
	}
	if value < 0 {
		return 0, fmt.Errorf("negative integer")
	}
	return value, nil
}

func parseOptionalTime(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, nil
	}
	value, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}, err
	}
	return value.UTC(), nil
}

func parseOptionalBool(raw string) (bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false, nil
	}
	switch strings.ToLower(raw) {
	case "true", "1", "yes":
		return true, nil
	case "false", "0", "no":
		return false, nil
	default:
		return false, fmt.Errorf("invalid boolean")
	}
}

func accessSubject(identity *auth.Identity) (string, string) {
	subject, err := iamclient.AuthorizationSubjectForIdentity(identity)
	if err != nil {
		return "unknown", ""
	}
	return strings.TrimSpace(string(subject.Type)), strings.TrimSpace(subject.Id)
}

func recordAccessEvent(ctx context.Context, svc *analytics.Service, row analytics.AccessEventRow, logger *slog.Logger) {
	if err := svc.RecordAccessEvent(ctx, row); err != nil {
		logger.WarnContext(ctx, "analytics access event write failed", "error", err)
	}
}

func writeJSONError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": strings.TrimSpace(code)})
}

func compactErrorCode(err error) string {
	if err == nil {
		return ""
	}
	raw := strings.ToLower(strings.TrimSpace(err.Error()))
	var out strings.Builder
	for _, r := range raw {
		switch {
		case r >= 'a' && r <= 'z':
			out.WriteRune(r)
		case r >= '0' && r <= '9':
			out.WriteRune(r)
		case r == '_' || r == '-':
			out.WriteRune('_')
		case r == ' ' || r == ':' || r == '/' || r == '.':
			out.WriteRune('_')
		}
		if out.Len() >= 64 {
			break
		}
	}
	code := strings.Trim(out.String(), "_")
	if code == "" {
		return "error"
	}
	return code
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}
