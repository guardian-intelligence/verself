package verself

import (
	"context"
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	governancecore "github.com/verself/verself-go/internal/generated/governance"
)

type GovernanceAuditOutcome string

const (
	GovernanceAuditOutcomeAllowed GovernanceAuditOutcome = "allowed"
	GovernanceAuditOutcomeDenied  GovernanceAuditOutcome = "denied"
	GovernanceAuditOutcomeError   GovernanceAuditOutcome = "error"
)

type GovernanceAuditOrder string

const (
	GovernanceAuditOrderAsc  GovernanceAuditOrder = "asc"
	GovernanceAuditOrderDesc GovernanceAuditOrder = "desc"
)

type GovernanceExportScope string

const (
	GovernanceExportScopeIdentity GovernanceExportScope = "identity"
	GovernanceExportScopeBilling  GovernanceExportScope = "billing"
	GovernanceExportScopeSandbox  GovernanceExportScope = "sandbox"
	GovernanceExportScopeAudit    GovernanceExportScope = "audit"
)

type ListGovernanceAuditEventsOptions struct {
	Limit              int
	Cursor             string
	Order              GovernanceAuditOrder
	ActorID            string
	AuditEvent         string
	CredentialID       string
	EventName          string
	EventSource        string
	Outcome            GovernanceAuditOutcome
	TargetID           string
	TargetType         string
	TargetResourceName string
}

type GovernanceAuditEvent struct {
	ActorID            string    `json:"actor_id"`
	ActorType          string    `json:"actor_type"`
	AuditEvent         string    `json:"audit_event"`
	CredentialID       string    `json:"credential_id,omitempty"`
	DetailSHA256       string    `json:"detail_sha256"`
	ErrorCode          string    `json:"error_code,omitempty"`
	EventID            string    `json:"event_id"`
	EventName          string    `json:"event_name"`
	EventSource        string    `json:"event_source"`
	HMACKeyID          string    `json:"hmac_key_id,omitempty"`
	OrgID              string    `json:"org_id"`
	Outcome            string    `json:"outcome"`
	Permission         string    `json:"permission"`
	PrevHMAC           string    `json:"prev_hmac"`
	RecordedAt         time.Time `json:"recorded_at"`
	RowHMAC            string    `json:"row_hmac"`
	Sequence           string    `json:"sequence"`
	TargetID           string    `json:"target_id,omitempty"`
	TargetType         string    `json:"target_type"`
	TargetResourceName string    `json:"targetResourceName,omitempty"`
	TraceID            string    `json:"trace_id,omitempty"`
}

type GovernanceAuditFilters struct {
	ActorID            string `json:"actor_id,omitempty"`
	AuditEvent         string `json:"audit_event,omitempty"`
	CredentialID       string `json:"credential_id,omitempty"`
	EventName          string `json:"event_name,omitempty"`
	EventSource        string `json:"event_source,omitempty"`
	Outcome            string `json:"outcome,omitempty"`
	TargetID           string `json:"target_id,omitempty"`
	TargetType         string `json:"target_type,omitempty"`
	TargetResourceName string `json:"targetResourceName,omitempty"`
}

type GovernanceAuditEvents struct {
	Events     []GovernanceAuditEvent `json:"events"`
	Filters    GovernanceAuditFilters `json:"filters"`
	Limit      int32                  `json:"limit"`
	NextCursor string                 `json:"next_cursor,omitempty"`
}

type CreateGovernanceExportInput struct {
	Scopes         []GovernanceExportScope
	IncludeLogs    bool
	IdempotencyKey string
}

type GovernanceExportFile struct {
	Path        string `json:"path"`
	ContentType string `json:"content_type"`
	Rows        int64  `json:"rows"`
	Bytes       int64  `json:"bytes"`
	SHA256      string `json:"sha256"`
}

type GovernanceExportJob struct {
	ExportID       string                 `json:"export_id"`
	ResourceName   string                 `json:"resourceName"`
	OrgID          string                 `json:"org_id"`
	RequestedBy    string                 `json:"requested_by"`
	Scopes         []string               `json:"scopes"`
	IncludeLogs    bool                   `json:"include_logs"`
	Format         string                 `json:"format"`
	State          string                 `json:"state"`
	ArtifactSHA256 string                 `json:"artifact_sha256,omitempty"`
	ArtifactBytes  int64                  `json:"artifact_bytes"`
	DownloadURL    string                 `json:"download_url,omitempty"`
	ErrorCode      string                 `json:"error_code,omitempty"`
	ErrorMessage   string                 `json:"error_message,omitempty"`
	Files          []GovernanceExportFile `json:"files"`
	CreatedAt      time.Time              `json:"created_at"`
	UpdatedAt      time.Time              `json:"updated_at"`
	CompletedAt    *time.Time             `json:"completed_at,omitempty"`
	ExpiresAt      time.Time              `json:"expires_at"`
}

type GovernanceExportArtifact struct {
	ExportID    string `json:"export_id"`
	FileName    string `json:"file_name"`
	ContentType string `json:"content_type"`
	Body        []byte `json:"-"`
}

type GovernanceClient struct {
	client *governancecore.Client
}

func (c *GovernanceClient) ListAuditEvents(ctx context.Context, options ListGovernanceAuditEventsOptions) (GovernanceAuditEvents, error) {
	if c == nil || c.client == nil {
		return GovernanceAuditEvents{}, fmt.Errorf("verself sdk: governance client is not initialized")
	}
	request := governancecore.ListAuditEventsRequest{}
	if options.Limit > 0 {
		limit, err := governanceAuditEventsLimit(options.Limit)
		if err != nil {
			return GovernanceAuditEvents{}, err
		}
		request.Limit = &limit
	}
	if strings.TrimSpace(options.Cursor) != "" {
		cursor := governancecore.AuditCursor(strings.TrimSpace(options.Cursor))
		request.Cursor = &cursor
	}
	if options.Order != "" {
		order := governancecore.AuditListOrder(options.Order)
		request.Order = &order
	}
	if strings.TrimSpace(options.ActorID) != "" {
		actorID := governancecore.AuditActorId(strings.TrimSpace(options.ActorID))
		request.ActorID = &actorID
	}
	if strings.TrimSpace(options.AuditEvent) != "" {
		auditEvent := governancecore.GovernanceAuditEventName(strings.TrimSpace(options.AuditEvent))
		request.AuditEvent = &auditEvent
	}
	if strings.TrimSpace(options.CredentialID) != "" {
		credentialID := governancecore.AuditCredentialId(strings.TrimSpace(options.CredentialID))
		request.CredentialID = &credentialID
	}
	if strings.TrimSpace(options.EventName) != "" {
		eventName := governancecore.AuditEventOperationName(strings.TrimSpace(options.EventName))
		request.EventName = &eventName
	}
	if strings.TrimSpace(options.EventSource) != "" {
		eventSource := governancecore.AuditEventSource(strings.TrimSpace(options.EventSource))
		request.EventSource = &eventSource
	}
	if options.Outcome != "" {
		outcome := governancecore.AuditOutcome(options.Outcome)
		request.Outcome = &outcome
	}
	if strings.TrimSpace(options.TargetID) != "" {
		targetID := governancecore.AuditTargetId(strings.TrimSpace(options.TargetID))
		request.TargetID = &targetID
	}
	if strings.TrimSpace(options.TargetType) != "" {
		targetType := governancecore.AuditTargetType(strings.TrimSpace(options.TargetType))
		request.TargetType = &targetType
	}
	if strings.TrimSpace(options.TargetResourceName) != "" {
		targetResourceName := governancecore.ResourceName(strings.TrimSpace(options.TargetResourceName))
		request.TargetResourceName = &targetResourceName
	}

	response, err := c.client.ListAuditEvents(ctx, request)
	if err != nil {
		return GovernanceAuditEvents{}, err
	}
	if response.Result == nil {
		return GovernanceAuditEvents{}, governanceAPIError("list audit events", response.StatusCode, response.Body)
	}
	return governanceAuditEventsFromGenerated(response.Result.Events, response.Result.Filters, response.Result.Limit, response.Result.NextCursor)
}

func (c *GovernanceClient) ListDataExports(ctx context.Context) ([]GovernanceExportJob, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("verself sdk: governance client is not initialized")
	}
	response, err := c.client.ListDataExports(ctx, governancecore.ListDataExportsRequest{})
	if err != nil {
		return nil, err
	}
	if response.Result == nil {
		return nil, governanceAPIError("list data exports", response.StatusCode, response.Body)
	}
	out := make([]GovernanceExportJob, 0, len(response.Result.Exports))
	for _, export := range response.Result.Exports {
		job, err := governanceExportJobFromGenerated(export)
		if err != nil {
			return nil, err
		}
		out = append(out, job)
	}
	return out, nil
}

func (c *GovernanceClient) CreateDataExport(ctx context.Context, input CreateGovernanceExportInput) (GovernanceExportJob, error) {
	if c == nil || c.client == nil {
		return GovernanceExportJob{}, fmt.Errorf("verself sdk: governance client is not initialized")
	}
	key, err := mutationKey("governance-export-create", input.IdempotencyKey)
	if err != nil {
		return GovernanceExportJob{}, err
	}
	scopes := make(governancecore.ExportScopes, 0, len(input.Scopes))
	for _, scope := range input.Scopes {
		if strings.TrimSpace(string(scope)) != "" {
			scopes = append(scopes, governancecore.ExportScope(strings.TrimSpace(string(scope))))
		}
	}
	body := governancecore.CreateDataExportInputBody{
		IncludeLogs: &input.IncludeLogs,
	}
	if len(scopes) > 0 {
		body.Scopes = &scopes
	}
	response, err := c.client.CreateDataExport(ctx, governancecore.CreateDataExportRequest{
		IdempotencyKey: governancecore.IdempotencyKey(key),
		Body:           body,
	})
	if err != nil {
		return GovernanceExportJob{}, err
	}
	if response.Result == nil {
		return GovernanceExportJob{}, governanceAPIError("create data export", response.StatusCode, response.Body)
	}
	return governanceExportJobFromGenerated(response.Result.Export)
}

func (c *GovernanceClient) GetDataExport(ctx context.Context, exportID string) (GovernanceExportJob, error) {
	if c == nil || c.client == nil {
		return GovernanceExportJob{}, fmt.Errorf("verself sdk: governance client is not initialized")
	}
	id, err := parseUUIDInput(exportID, "export id")
	if err != nil {
		return GovernanceExportJob{}, err
	}
	response, err := c.client.GetDataExport(ctx, governancecore.GetDataExportRequest{ExportID: governancecore.DataExportId(id.String())})
	if err != nil {
		return GovernanceExportJob{}, err
	}
	if response.Result == nil {
		return GovernanceExportJob{}, governanceAPIError("get data export", response.StatusCode, response.Body)
	}
	return governanceExportJobFromGenerated(response.Result.Export)
}

func (c *GovernanceClient) DownloadDataExport(ctx context.Context, exportID string) (GovernanceExportArtifact, error) {
	if c == nil || c.client == nil {
		return GovernanceExportArtifact{}, fmt.Errorf("verself sdk: governance client is not initialized")
	}
	id, err := parseUUIDInput(exportID, "export id")
	if err != nil {
		return GovernanceExportArtifact{}, err
	}
	response, err := c.client.DownloadDataExport(ctx, governancecore.DownloadDataExportRequest{ExportID: governancecore.DataExportId(id.String())})
	if err != nil {
		return GovernanceExportArtifact{}, err
	}
	if response.Result == nil || response.StatusCode != http.StatusOK {
		return GovernanceExportArtifact{}, governanceAPIError("download data export", response.StatusCode, response.Body)
	}
	contentType := string(response.Result.ContentType)
	if strings.TrimSpace(contentType) == "" {
		contentType = "application/gzip"
	}
	return GovernanceExportArtifact{
		ExportID:    id.String(),
		FileName:    governanceArtifactFileName(string(response.Result.ContentDisposition), id),
		ContentType: contentType,
		Body:        append([]byte(nil), response.Result.Body...),
	}, nil
}

func governanceArtifactFileName(contentDisposition string, exportID uuid.UUID) string {
	if strings.TrimSpace(contentDisposition) != "" {
		_, params, err := mime.ParseMediaType(contentDisposition)
		if err == nil {
			// Content-Disposition filenames are caller-controlled response metadata.
			if filename := filepath.Base(strings.TrimSpace(params["filename"])); filename != "" && filename != "." {
				return filename
			}
		}
	}
	return fmt.Sprintf("verself-export-%s.tar.gz", exportID.String())
}

func governanceAuditEventsFromGenerated(events governancecore.AuditEvents, filters governancecore.AuditFilters, limit governancecore.AuditEventsLimit, nextCursor *governancecore.AuditCursor) (GovernanceAuditEvents, error) {
	out := GovernanceAuditEvents{
		Events:  make([]GovernanceAuditEvent, 0, len(events)),
		Filters: governanceAuditFiltersFromGenerated(filters),
		Limit:   int32(limit), // #nosec G115 -- Smithy bounds audit event pages to 200.
	}
	if nextCursor != nil {
		out.NextCursor = string(*nextCursor)
	}
	for _, event := range events {
		converted, err := governanceAuditEventFromGenerated(event)
		if err != nil {
			return GovernanceAuditEvents{}, err
		}
		out.Events = append(out.Events, converted)
	}
	return out, nil
}

func governanceAuditEventsLimit(value int) (governancecore.AuditEventsLimit, error) {
	if value < 1 || value > 200 {
		return 0, fmt.Errorf("verself sdk: governance audit limit must be between 1 and 200")
	}
	return governancecore.AuditEventsLimit(value), nil // #nosec G115 -- Smithy bounds audit event pages to 200.
}

func governanceAuditFiltersFromGenerated(input governancecore.AuditFilters) GovernanceAuditFilters {
	return GovernanceAuditFilters{
		ActorID:            stringValue(input.ActorID),
		AuditEvent:         stringValue(input.AuditEvent),
		CredentialID:       stringValue(input.CredentialID),
		EventName:          stringValue(input.EventName),
		EventSource:        stringValue(input.EventSource),
		Outcome:            stringValue(input.Outcome),
		TargetID:           stringValue(input.TargetID),
		TargetType:         stringValue(input.TargetType),
		TargetResourceName: stringValue(input.TargetResourceName),
	}
}

func governanceAuditEventFromGenerated(input governancecore.AuditEvent) (GovernanceAuditEvent, error) {
	recordedAt, err := parseGeneratedTime(input.RecordedAt, "governance audit recorded_at")
	if err != nil {
		return GovernanceAuditEvent{}, err
	}
	return GovernanceAuditEvent{
		ActorID:            string(input.ActorID),
		ActorType:          string(input.ActorType),
		AuditEvent:         string(input.AuditEvent),
		CredentialID:       stringValue(input.CredentialID),
		DetailSHA256:       string(input.DetailSha256),
		ErrorCode:          stringValue(input.ErrorCode),
		EventID:            string(input.EventID),
		EventName:          string(input.EventName),
		EventSource:        string(input.EventSource),
		HMACKeyID:          stringValue(input.HmacKeyID),
		OrgID:              string(input.OrgID),
		Outcome:            string(input.Outcome),
		Permission:         string(input.Permission),
		PrevHMAC:           string(input.PrevHmac),
		RecordedAt:         recordedAt,
		RowHMAC:            string(input.RowHmac),
		Sequence:           string(input.Sequence),
		TargetID:           stringValue(input.TargetID),
		TargetType:         string(input.TargetType),
		TargetResourceName: stringValue(input.TargetResourceName),
		TraceID:            stringValue(input.TraceID),
	}, nil
}

func governanceExportJobFromGenerated(input governancecore.DataExportJob) (GovernanceExportJob, error) {
	artifactBytes, err := parseDecimalInt64(input.ArtifactBytes, "governance export artifact bytes")
	if err != nil {
		return GovernanceExportJob{}, err
	}
	createdAt, err := parseGeneratedTime(input.CreatedAt, "governance export created_at")
	if err != nil {
		return GovernanceExportJob{}, err
	}
	updatedAt, err := parseGeneratedTime(input.UpdatedAt, "governance export updated_at")
	if err != nil {
		return GovernanceExportJob{}, err
	}
	completedAt, err := parseGeneratedOptionalTime(input.CompletedAt, "governance export completed_at")
	if err != nil {
		return GovernanceExportJob{}, err
	}
	expiresAt, err := parseGeneratedTime(input.ExpiresAt, "governance export expires_at")
	if err != nil {
		return GovernanceExportJob{}, err
	}
	files := make([]GovernanceExportFile, 0, len(input.Files))
	for _, file := range input.Files {
		converted, err := governanceExportFileFromGenerated(file)
		if err != nil {
			return GovernanceExportJob{}, err
		}
		files = append(files, converted)
	}
	return GovernanceExportJob{
		ExportID:       string(input.ExportID),
		ResourceName:   string(input.ResourceName),
		OrgID:          string(input.OrgID),
		RequestedBy:    string(input.RequestedBy),
		Scopes:         governanceExportScopesFromGenerated(input.Scopes),
		IncludeLogs:    input.IncludeLogs,
		Format:         string(input.Format),
		State:          string(input.State),
		ArtifactSHA256: stringValue(input.ArtifactSha256),
		ArtifactBytes:  artifactBytes,
		DownloadURL:    stringValue(input.DownloadURL),
		ErrorCode:      stringValue(input.ErrorCode),
		ErrorMessage:   stringValue(input.ErrorMessage),
		Files:          files,
		CreatedAt:      createdAt,
		UpdatedAt:      updatedAt,
		CompletedAt:    completedAt,
		ExpiresAt:      expiresAt,
	}, nil
}

func governanceExportFileFromGenerated(input governancecore.DataExportFile) (GovernanceExportFile, error) {
	rows, err := parseDecimalInt64(input.Rows, "governance export file rows")
	if err != nil {
		return GovernanceExportFile{}, err
	}
	bytes, err := parseDecimalInt64(input.Bytes, "governance export file bytes")
	if err != nil {
		return GovernanceExportFile{}, err
	}
	return GovernanceExportFile{
		Path:        string(input.Path),
		ContentType: string(input.ContentType),
		Rows:        rows,
		Bytes:       bytes,
		SHA256:      string(input.Sha256),
	}, nil
}

func governanceExportScopesFromGenerated(input governancecore.ExportScopes) []string {
	out := make([]string, 0, len(input))
	for _, scope := range input {
		out = append(out, string(scope))
	}
	return out
}

func parseGeneratedTime(input string, field string) (time.Time, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		return time.Time{}, fmt.Errorf("verself sdk: %s is required", field)
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("verself sdk: parse %s: %w", field, err)
	}
	return parsed, nil
}

func parseGeneratedOptionalTime(input *string, field string) (*time.Time, error) {
	if input == nil || strings.TrimSpace(*input) == "" {
		return nil, nil
	}
	parsed, err := parseGeneratedTime(*input, field)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func governanceAPIError(operation string, statusCode int, body []byte) error {
	var title *string
	var detail *string
	var model struct {
		Title  *string `json:"title"`
		Detail *string `json:"detail"`
	}
	if len(body) > 0 && json.Unmarshal(body, &model) == nil {
		title = model.Title
		detail = model.Detail
	}
	return apiErrorFields("Governance API", operation, statusCode, title, detail, body)
}
