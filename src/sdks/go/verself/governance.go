package verself

import (
	"context"
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
	Limit        int
	Cursor       string
	Order        GovernanceAuditOrder
	ActorID      string
	AuditEvent   string
	CredentialID string
	EventName    string
	EventSource  string
	Outcome      GovernanceAuditOutcome
	TargetID     string
	TargetType   string
}

type GovernanceAuditEvent struct {
	ActorID      string    `json:"actor_id"`
	ActorType    string    `json:"actor_type"`
	AuditEvent   string    `json:"audit_event"`
	CredentialID string    `json:"credential_id,omitempty"`
	DetailSHA256 string    `json:"detail_sha256"`
	ErrorCode    string    `json:"error_code,omitempty"`
	EventID      string    `json:"event_id"`
	EventName    string    `json:"event_name"`
	EventSource  string    `json:"event_source"`
	HMACKeyID    string    `json:"hmac_key_id,omitempty"`
	OrgID        string    `json:"org_id"`
	Outcome      string    `json:"outcome"`
	Permission   string    `json:"permission"`
	PrevHMAC     string    `json:"prev_hmac"`
	RecordedAt   time.Time `json:"recorded_at"`
	RowHMAC      string    `json:"row_hmac"`
	Sequence     string    `json:"sequence"`
	TargetID     string    `json:"target_id,omitempty"`
	TargetType   string    `json:"target_type"`
	TraceID      string    `json:"trace_id,omitempty"`
}

type GovernanceAuditFilters struct {
	ActorID      string `json:"actor_id,omitempty"`
	AuditEvent   string `json:"audit_event,omitempty"`
	CredentialID string `json:"credential_id,omitempty"`
	EventName    string `json:"event_name,omitempty"`
	EventSource  string `json:"event_source,omitempty"`
	Outcome      string `json:"outcome,omitempty"`
	TargetID     string `json:"target_id,omitempty"`
	TargetType   string `json:"target_type,omitempty"`
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
	client *governancecore.ClientWithResponses
}

func (c *GovernanceClient) ListAuditEvents(ctx context.Context, options ListGovernanceAuditEventsOptions) (GovernanceAuditEvents, error) {
	if c == nil || c.client == nil {
		return GovernanceAuditEvents{}, fmt.Errorf("verself sdk: governance client is not initialized")
	}
	params := &governancecore.ListAuditEventsParams{}
	if options.Limit > 0 {
		limit := int64(options.Limit)
		params.Limit = &limit
	}
	if strings.TrimSpace(options.Cursor) != "" {
		cursor := strings.TrimSpace(options.Cursor)
		params.Cursor = &cursor
	}
	if options.Order != "" {
		order := governancecore.ListAuditEventsParamsOrder(options.Order)
		params.Order = &order
	}
	if strings.TrimSpace(options.ActorID) != "" {
		actorID := strings.TrimSpace(options.ActorID)
		params.ActorId = &actorID
	}
	if strings.TrimSpace(options.AuditEvent) != "" {
		auditEvent := strings.TrimSpace(options.AuditEvent)
		params.AuditEvent = &auditEvent
	}
	if strings.TrimSpace(options.CredentialID) != "" {
		credentialID := strings.TrimSpace(options.CredentialID)
		params.CredentialId = &credentialID
	}
	if strings.TrimSpace(options.EventName) != "" {
		eventName := strings.TrimSpace(options.EventName)
		params.EventName = &eventName
	}
	if strings.TrimSpace(options.EventSource) != "" {
		eventSource := strings.TrimSpace(options.EventSource)
		params.EventSource = &eventSource
	}
	if options.Outcome != "" {
		outcome := governancecore.ListAuditEventsParamsOutcome(options.Outcome)
		params.Outcome = &outcome
	}
	if strings.TrimSpace(options.TargetID) != "" {
		targetID := strings.TrimSpace(options.TargetID)
		params.TargetId = &targetID
	}
	if strings.TrimSpace(options.TargetType) != "" {
		targetType := strings.TrimSpace(options.TargetType)
		params.TargetType = &targetType
	}

	response, err := c.client.ListAuditEventsWithResponse(ctx, params)
	if err != nil {
		return GovernanceAuditEvents{}, err
	}
	if response.JSON200 == nil {
		return GovernanceAuditEvents{}, governanceAPIError("list audit events", response.StatusCode(), response.ApplicationproblemJSONDefault, response.Body)
	}
	return governanceAuditEventsFromGenerated(*response.JSON200), nil
}

func (c *GovernanceClient) ListDataExports(ctx context.Context) ([]GovernanceExportJob, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("verself sdk: governance client is not initialized")
	}
	response, err := c.client.ListDataExportsWithResponse(ctx)
	if err != nil {
		return nil, err
	}
	if response.JSON200 == nil {
		return nil, governanceAPIError("list data exports", response.StatusCode(), response.ApplicationproblemJSONDefault, response.Body)
	}
	out := make([]GovernanceExportJob, 0, len(response.JSON200.Exports))
	for _, export := range response.JSON200.Exports {
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
	scopes := make([]string, 0, len(input.Scopes))
	for _, scope := range input.Scopes {
		if strings.TrimSpace(string(scope)) != "" {
			scopes = append(scopes, strings.TrimSpace(string(scope)))
		}
	}
	body := governancecore.CreateDataExportJSONRequestBody{
		IncludeLogs: &input.IncludeLogs,
	}
	if len(scopes) > 0 {
		body.Scopes = &scopes
	}
	response, err := c.client.CreateDataExportWithResponse(ctx, &governancecore.CreateDataExportParams{
		IdempotencyKey: key,
	}, body)
	if err != nil {
		return GovernanceExportJob{}, err
	}
	if response.JSON201 == nil {
		return GovernanceExportJob{}, governanceAPIError("create data export", response.StatusCode(), response.ApplicationproblemJSONDefault, response.Body)
	}
	return governanceExportJobFromGenerated(*response.JSON201)
}

func (c *GovernanceClient) GetDataExport(ctx context.Context, exportID string) (GovernanceExportJob, error) {
	if c == nil || c.client == nil {
		return GovernanceExportJob{}, fmt.Errorf("verself sdk: governance client is not initialized")
	}
	id, err := parseUUIDInput(exportID, "export id")
	if err != nil {
		return GovernanceExportJob{}, err
	}
	response, err := c.client.GetDataExportWithResponse(ctx, id)
	if err != nil {
		return GovernanceExportJob{}, err
	}
	if response.JSON200 == nil {
		return GovernanceExportJob{}, governanceAPIError("get data export", response.StatusCode(), response.ApplicationproblemJSONDefault, response.Body)
	}
	return governanceExportJobFromGenerated(*response.JSON200)
}

func (c *GovernanceClient) DownloadDataExport(ctx context.Context, exportID string) (GovernanceExportArtifact, error) {
	if c == nil || c.client == nil {
		return GovernanceExportArtifact{}, fmt.Errorf("verself sdk: governance client is not initialized")
	}
	id, err := parseUUIDInput(exportID, "export id")
	if err != nil {
		return GovernanceExportArtifact{}, err
	}
	response, err := c.client.DownloadDataExportWithResponse(ctx, id, acceptGzipRequest)
	if err != nil {
		return GovernanceExportArtifact{}, err
	}
	if response.StatusCode() != http.StatusOK {
		return GovernanceExportArtifact{}, governanceAPIError("download data export", response.StatusCode(), response.ApplicationproblemJSONDefault, response.Body)
	}
	contentType := response.HTTPResponse.Header.Get("Content-Type")
	if strings.TrimSpace(contentType) == "" {
		contentType = "application/gzip"
	}
	return GovernanceExportArtifact{
		ExportID:    id.String(),
		FileName:    governanceArtifactFileName(response.HTTPResponse.Header.Get("Content-Disposition"), id),
		ContentType: contentType,
		Body:        append([]byte(nil), response.Body...),
	}, nil
}

func acceptGzipRequest(ctx context.Context, req *http.Request) error {
	_ = ctx
	req.Header.Set("Accept", "application/gzip")
	return nil
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

func governanceAuditEventsFromGenerated(input governancecore.GovernanceAuditEvents) GovernanceAuditEvents {
	out := GovernanceAuditEvents{
		Events:  make([]GovernanceAuditEvent, 0, len(input.Events)),
		Filters: governanceAuditFiltersFromGenerated(input.Filters),
		Limit:   input.Limit,
	}
	if input.NextCursor != nil {
		out.NextCursor = *input.NextCursor
	}
	for _, event := range input.Events {
		out.Events = append(out.Events, governanceAuditEventFromGenerated(event))
	}
	return out
}

func governanceAuditFiltersFromGenerated(input governancecore.GovernanceAuditFilters) GovernanceAuditFilters {
	return GovernanceAuditFilters{
		ActorID:      stringValue(input.ActorId),
		AuditEvent:   stringValue(input.AuditEvent),
		CredentialID: stringValue(input.CredentialId),
		EventName:    stringValue(input.EventName),
		EventSource:  stringValue(input.EventSource),
		Outcome:      stringValue(input.Outcome),
		TargetID:     stringValue(input.TargetId),
		TargetType:   stringValue(input.TargetType),
	}
}

func governanceAuditEventFromGenerated(input governancecore.GovernanceAuditEvent) GovernanceAuditEvent {
	return GovernanceAuditEvent{
		ActorID:      input.ActorId,
		ActorType:    input.ActorType,
		AuditEvent:   input.AuditEvent,
		CredentialID: stringValue(input.CredentialId),
		DetailSHA256: input.DetailSha256,
		ErrorCode:    stringValue(input.ErrorCode),
		EventID:      input.EventId,
		EventName:    input.EventName,
		EventSource:  input.EventSource,
		HMACKeyID:    stringValue(input.HmacKeyId),
		OrgID:        input.OrgId,
		Outcome:      input.Outcome,
		Permission:   input.Permission,
		PrevHMAC:     input.PrevHmac,
		RecordedAt:   input.RecordedAt,
		RowHMAC:      input.RowHmac,
		Sequence:     input.Sequence,
		TargetID:     stringValue(input.TargetId),
		TargetType:   input.TargetType,
		TraceID:      stringValue(input.TraceId),
	}
}

func governanceExportJobFromGenerated(input governancecore.GovernanceExportJob) (GovernanceExportJob, error) {
	artifactBytes, err := parseDecimalInt64(input.ArtifactBytes, "governance export artifact bytes")
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
		ExportID:       input.ExportId,
		OrgID:          input.OrgId,
		RequestedBy:    input.RequestedBy,
		Scopes:         append([]string(nil), input.Scopes...),
		IncludeLogs:    input.IncludeLogs,
		Format:         input.Format,
		State:          input.State,
		ArtifactSHA256: stringValue(input.ArtifactSha256),
		ArtifactBytes:  artifactBytes,
		DownloadURL:    stringValue(input.DownloadUrl),
		ErrorCode:      stringValue(input.ErrorCode),
		ErrorMessage:   stringValue(input.ErrorMessage),
		Files:          files,
		CreatedAt:      input.CreatedAt,
		UpdatedAt:      input.UpdatedAt,
		CompletedAt:    input.CompletedAt,
		ExpiresAt:      input.ExpiresAt,
	}, nil
}

func governanceExportFileFromGenerated(input governancecore.GovernanceExportFile) (GovernanceExportFile, error) {
	rows, err := parseDecimalInt64(input.Rows, "governance export file rows")
	if err != nil {
		return GovernanceExportFile{}, err
	}
	bytes, err := parseDecimalInt64(input.Bytes, "governance export file bytes")
	if err != nil {
		return GovernanceExportFile{}, err
	}
	return GovernanceExportFile{
		Path:        input.Path,
		ContentType: input.ContentType,
		Rows:        rows,
		Bytes:       bytes,
		SHA256:      input.Sha256,
	}, nil
}

func governanceAPIError(operation string, statusCode int, model *governancecore.ErrorModel, body []byte) error {
	var title *string
	var detail *string
	if model != nil {
		title = model.Title
		detail = model.Detail
	}
	return apiErrorFields("Governance API", operation, statusCode, title, detail, body)
}
