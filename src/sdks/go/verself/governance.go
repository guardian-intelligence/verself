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

	governancecore "github.com/verself/verself-go/internal/transport/governance"
)

type GovernanceAPIActivityOrder string

const (
	GovernanceAPIActivityOrderAsc  GovernanceAPIActivityOrder = "asc"
	GovernanceAPIActivityOrderDesc GovernanceAPIActivityOrder = "desc"
)

type GovernanceExportScope string

const (
	GovernanceExportScopeIdentity    GovernanceExportScope = "identity"
	GovernanceExportScopeBilling     GovernanceExportScope = "billing"
	GovernanceExportScopeSandbox     GovernanceExportScope = "sandbox"
	GovernanceExportScopeAPIActivity GovernanceExportScope = "api_activity"
)

type ListGovernanceAPIActivitiesOptions struct {
	Limit         int
	Cursor        string
	Order         GovernanceAPIActivityOrder
	ActorUID      string
	ActorType     string
	APIService    string
	APIOperation  string
	ActivityID    uint8
	CredentialUID string
	ResourceUID   string
	ResourceType  string
	StatusID      uint8
	StatusCode    string
	TraceUID      string
}

type GovernanceAPIActivity struct {
	MetadataUID             string    `json:"metadata_uid"`
	Time                    time.Time `json:"time"`
	OrgID                   string    `json:"org_id"`
	Sequence                string    `json:"sequence"`
	OCSFVersion             string    `json:"ocsf_version"`
	ClassUID                uint16    `json:"class_uid"`
	ClassName               string    `json:"class_name"`
	TypeUID                 uint32    `json:"type_uid"`
	ActivityID              uint8     `json:"activity_id"`
	ActivityName            string    `json:"activity_name"`
	ActionID                uint8     `json:"action_id"`
	Action                  string    `json:"action"`
	StatusID                uint8     `json:"status_id"`
	Status                  string    `json:"status"`
	StatusCode              string    `json:"status_code"`
	APIService              string    `json:"api_service"`
	APIOperation            string    `json:"api_operation"`
	ActorType               string    `json:"actor_type"`
	ActorUID                string    `json:"actor_uid"`
	CredentialUID           string    `json:"credential_uid,omitempty"`
	PrimaryResourceType     string    `json:"primary_resource_type"`
	PrimaryResourceUID      string    `json:"primary_resource_uid,omitempty"`
	PrimaryResourceFullName string    `json:"primary_resource_full_name,omitempty"`
	Permission              string    `json:"permission"`
	HTTPMethod              string    `json:"http_method"`
	HTTPRoute               string    `json:"http_route"`
	HTTPResponseCode        uint16    `json:"http_response_code"`
	TraceUID                string    `json:"trace_uid,omitempty"`
	SpanUID                 string    `json:"span_uid,omitempty"`
	OCSFSHA256              string    `json:"ocsf_sha256"`
	PrevHMAC                string    `json:"prev_hmac"`
	RowHMAC                 string    `json:"row_hmac"`
	HMACKeyID               string    `json:"hmac_key_id,omitempty"`
}

type GovernanceAPIActivityFilters struct {
	ActorUID      string `json:"actor_uid,omitempty"`
	ActorType     string `json:"actor_type,omitempty"`
	APIService    string `json:"api_service,omitempty"`
	APIOperation  string `json:"api_operation,omitempty"`
	CredentialUID string `json:"credential_uid,omitempty"`
	ResourceUID   string `json:"resource_uid,omitempty"`
	ResourceType  string `json:"resource_type,omitempty"`
	StatusCode    string `json:"status_code,omitempty"`
	TraceUID      string `json:"trace_uid,omitempty"`
	ActivityID    uint8  `json:"activity_id,omitempty"`
	StatusID      uint8  `json:"status_id,omitempty"`
}

type GovernanceAPIActivities struct {
	APIActivities []GovernanceAPIActivity      `json:"api_activities"`
	Filters       GovernanceAPIActivityFilters `json:"filters"`
	Limit         int32                        `json:"limit"`
	NextCursor    string                       `json:"next_cursor,omitempty"`
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

func (c *GovernanceClient) ListAPIActivities(ctx context.Context, options ListGovernanceAPIActivitiesOptions) (GovernanceAPIActivities, error) {
	if c == nil || c.client == nil {
		return GovernanceAPIActivities{}, fmt.Errorf("verself sdk: governance client is not initialized")
	}
	request := governancecore.ListAPIActivitiesRequest{}
	if options.Limit > 0 {
		limit, err := governanceAPIActivityLimit(options.Limit)
		if err != nil {
			return GovernanceAPIActivities{}, err
		}
		request.Limit = &limit
	}
	if strings.TrimSpace(options.Cursor) != "" {
		cursor := governancecore.APIActivityCursor(strings.TrimSpace(options.Cursor))
		request.Cursor = &cursor
	}
	if options.Order != "" {
		order := governancecore.APIActivityListOrder(options.Order)
		request.Order = &order
	}
	if strings.TrimSpace(options.ActorUID) != "" {
		actorUID := governancecore.ActorUID(strings.TrimSpace(options.ActorUID))
		request.ActorUID = &actorUID
	}
	if strings.TrimSpace(options.ActorType) != "" {
		actorType := governancecore.ActorType(strings.TrimSpace(options.ActorType))
		request.ActorType = &actorType
	}
	if strings.TrimSpace(options.APIService) != "" {
		apiService := governancecore.APIActivityService(strings.TrimSpace(options.APIService))
		request.APIService = &apiService
	}
	if strings.TrimSpace(options.APIOperation) != "" {
		apiOperation := governancecore.APIActivityOperation(strings.TrimSpace(options.APIOperation))
		request.APIOperation = &apiOperation
	}
	if options.ActivityID != 0 {
		request.ActivityID = &options.ActivityID
	}
	if strings.TrimSpace(options.CredentialUID) != "" {
		credentialUID := governancecore.CredentialUID(strings.TrimSpace(options.CredentialUID))
		request.CredentialUID = &credentialUID
	}
	if strings.TrimSpace(options.ResourceUID) != "" {
		resourceUID := governancecore.ResourceUID(strings.TrimSpace(options.ResourceUID))
		request.ResourceUID = &resourceUID
	}
	if strings.TrimSpace(options.ResourceType) != "" {
		resourceType := governancecore.ResourceType(strings.TrimSpace(options.ResourceType))
		request.ResourceType = &resourceType
	}
	if options.StatusID != 0 {
		request.StatusID = &options.StatusID
	}
	if strings.TrimSpace(options.StatusCode) != "" {
		request.StatusCode = &options.StatusCode
	}
	if strings.TrimSpace(options.TraceUID) != "" {
		traceUID := governancecore.TraceID(strings.TrimSpace(options.TraceUID))
		request.TraceUID = &traceUID
	}

	response, err := c.client.ListAPIActivities(ctx, request)
	if err != nil {
		return GovernanceAPIActivities{}, err
	}
	if response.Result == nil {
		return GovernanceAPIActivities{}, governanceAPIError("list API activities", response.StatusCode, response.Body)
	}
	return governanceAPIActivitiesFromGenerated(response.Result.APIActivities, response.Result.Filters, response.Result.Limit, response.Result.NextCursor)
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
		IdempotencyKey: key,
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
	response, err := c.client.GetDataExport(ctx, governancecore.GetDataExportRequest{ExportID: governancecore.DataExportID(id.String())})
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
	response, err := c.client.DownloadDataExport(ctx, governancecore.DownloadDataExportRequest{ExportID: governancecore.DataExportID(id.String())})
	if err != nil {
		return GovernanceExportArtifact{}, err
	}
	if response.StatusCode != http.StatusOK {
		return GovernanceExportArtifact{}, governanceAPIError("download data export", response.StatusCode, response.Body)
	}
	contentType := response.ContentType
	if strings.TrimSpace(contentType) == "" {
		contentType = "application/gzip"
	}
	return GovernanceExportArtifact{
		ExportID:    id.String(),
		FileName:    governanceArtifactFileName(response.ContentDisposition, id),
		ContentType: contentType,
		Body:        append([]byte(nil), response.Body...),
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

func governanceAPIActivitiesFromGenerated(events governancecore.APIActivityEvents, filters governancecore.APIActivityFilters, limit governancecore.APIActivityEventsLimit, nextCursor *governancecore.APIActivityCursor) (GovernanceAPIActivities, error) {
	out := GovernanceAPIActivities{
		APIActivities: make([]GovernanceAPIActivity, 0, len(events)),
		Filters:       governanceAPIActivityFiltersFromGenerated(filters),
		Limit:         int32(limit), // #nosec G115 -- Smithy bounds API activity pages to 200.
	}
	if nextCursor != nil {
		out.NextCursor = string(*nextCursor)
	}
	for _, event := range events {
		converted, err := governanceAPIActivityFromGenerated(event)
		if err != nil {
			return GovernanceAPIActivities{}, err
		}
		out.APIActivities = append(out.APIActivities, converted)
	}
	return out, nil
}

func governanceAPIActivityLimit(value int) (governancecore.APIActivityEventsLimit, error) {
	if value < 1 || value > 200 {
		return 0, fmt.Errorf("verself sdk: governance API activity limit must be between 1 and 200")
	}
	return governancecore.APIActivityEventsLimit(value), nil // #nosec G115 -- Smithy bounds API activity pages to 200.
}

func governanceAPIActivityFiltersFromGenerated(input governancecore.APIActivityFilters) GovernanceAPIActivityFilters {
	out := GovernanceAPIActivityFilters{
		ActorUID:      stringValue(input.ActorUID),
		ActorType:     stringValue(input.ActorType),
		APIService:    stringValue(input.APIService),
		APIOperation:  stringValue(input.APIOperation),
		CredentialUID: stringValue(input.CredentialUID),
		ResourceUID:   stringValue(input.ResourceUID),
		ResourceType:  stringValue(input.ResourceType),
		StatusCode:    stringValue(input.StatusCode),
		TraceUID:      stringValue(input.TraceUID),
	}
	if input.ActivityID != nil {
		out.ActivityID = *input.ActivityID
	}
	if input.StatusID != nil {
		out.StatusID = *input.StatusID
	}
	return out
}

func governanceAPIActivityFromGenerated(input governancecore.APIActivityEvent) (GovernanceAPIActivity, error) {
	observedAt, err := parseGeneratedTime(input.Time, "governance API activity time")
	if err != nil {
		return GovernanceAPIActivity{}, err
	}
	return GovernanceAPIActivity{
		MetadataUID:             string(input.MetadataUID),
		Time:                    observedAt,
		OrgID:                   string(input.OrgID),
		Sequence:                string(input.Sequence),
		OCSFVersion:             input.OCSFVersion,
		ClassUID:                input.ClassUID,
		ClassName:               string(input.ClassName),
		TypeUID:                 input.TypeUID,
		ActivityID:              input.ActivityID,
		ActivityName:            input.ActivityName,
		ActionID:                input.ActionID,
		Action:                  input.Action,
		StatusID:                input.StatusID,
		Status:                  string(input.Status),
		StatusCode:              input.StatusCode,
		APIService:              string(input.APIService),
		APIOperation:            string(input.APIOperation),
		ActorType:               string(input.ActorType),
		ActorUID:                string(input.ActorUID),
		CredentialUID:           stringValue(input.CredentialUID),
		PrimaryResourceType:     string(input.PrimaryResourceType),
		PrimaryResourceUID:      stringValue(input.PrimaryResourceUID),
		PrimaryResourceFullName: stringValue(input.PrimaryResourceFullName),
		Permission:              string(input.Permission),
		HTTPMethod:              input.HTTPMethod,
		HTTPRoute:               input.HTTPRoute,
		HTTPResponseCode:        input.HTTPResponseCode,
		TraceUID:                stringValue(input.TraceUID),
		SpanUID:                 stringValue(input.SpanUID),
		OCSFSHA256:              string(input.OCSFSHA256),
		PrevHMAC:                string(input.PrevHMAC),
		RowHMAC:                 string(input.RowHMAC),
		HMACKeyID:               stringValue(input.HMACKeyID),
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
		ArtifactSHA256: stringValue(input.ArtifactSHA256),
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
		SHA256:      string(input.SHA256),
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
