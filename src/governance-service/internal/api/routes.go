package api

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/forge-metal/apiwire"
	"github.com/forge-metal/governance-service/internal/governance"
)

var apiTracer = otel.Tracer("governance-service/internal/api")

func RegisterRoutes(api huma.API, svc *governance.Service) {
	registerSecured(api, svc, secured(huma.Operation{
		OperationID: "list-audit-events",
		Method:      http.MethodGet,
		Path:        "/api/v1/governance/audit/events",
		Summary:     "List organization audit events",
	}, operationPolicy{
		Permission:         permissionAuditRead,
		TargetKind:         "audit_event",
		Action:             "list",
		OrgScope:           "token_org_id",
		RateLimitClass:     "read",
		AuditEvent:         "governance.audit_event.list",
		SourceProductArea:  "Governance",
		OperationDisplay:   "List audit events",
		OperationType:      "read",
		EventCategory:      "governance",
		RiskLevel:          "medium",
		DataClassification: "restricted",
	}), listAuditEvents(svc))

	registerSecured(api, svc, secured(huma.Operation{
		OperationID: "list-data-exports",
		Method:      http.MethodGet,
		Path:        "/api/v1/governance/exports",
		Summary:     "List organization data exports",
	}, operationPolicy{
		Permission:         permissionExportRead,
		TargetKind:         "data_export",
		Action:             "list",
		OrgScope:           "token_org_id",
		RateLimitClass:     "read",
		AuditEvent:         "governance.data_export.list",
		SourceProductArea:  "Governance",
		OperationDisplay:   "List data exports",
		OperationType:      "read",
		EventCategory:      "export",
		RiskLevel:          "medium",
		DataClassification: "restricted",
	}), listExports(svc))

	registerSecured(api, svc, secured(huma.Operation{
		OperationID:   "create-data-export",
		Method:        http.MethodPost,
		Path:          "/api/v1/governance/exports",
		Summary:       "Create an organization data export",
		DefaultStatus: 201,
	}, operationPolicy{
		Permission:         permissionExportCreate,
		TargetKind:         "data_export",
		Action:             "create",
		OrgScope:           "token_org_id",
		RateLimitClass:     "export_create",
		Idempotency:        idempotencyHeaderKey,
		AuditEvent:         "governance.data_export.create",
		SourceProductArea:  "Governance",
		OperationDisplay:   "Create data export",
		OperationType:      "export",
		EventCategory:      "export",
		RiskLevel:          "high",
		DataClassification: "restricted",
		BodyLimitBytes:     bodyLimitSmallJSON,
	}), createExport(svc))

	registerSecured(api, svc, secured(huma.Operation{
		OperationID: "get-data-export",
		Method:      http.MethodGet,
		Path:        "/api/v1/governance/exports/{export_id}",
		Summary:     "Get an organization data export",
	}, operationPolicy{
		Permission:         permissionExportRead,
		TargetKind:         "data_export",
		Action:             "read",
		OrgScope:           "token_org_id",
		RateLimitClass:     "read",
		AuditEvent:         "governance.data_export.read",
		SourceProductArea:  "Governance",
		OperationDisplay:   "Read data export",
		OperationType:      "read",
		EventCategory:      "export",
		RiskLevel:          "medium",
		DataClassification: "restricted",
	}), getExport(svc))

	registerSecured(api, svc, secured(huma.Operation{
		OperationID: "download-data-export",
		Method:      http.MethodGet,
		Path:        "/api/v1/governance/exports/{export_id}/download",
		Summary:     "Download an organization data export artifact",
		Responses: map[string]*huma.Response{
			"200": {
				Description: "tar.gz export artifact",
				Content: map[string]*huma.MediaType{
					"application/gzip": {Schema: &huma.Schema{Type: "string", Format: "binary"}},
				},
			},
		},
	}, operationPolicy{
		Permission:         permissionExportRead,
		TargetKind:         "data_export",
		Action:             "download",
		OrgScope:           "token_org_id",
		RateLimitClass:     "export_download",
		AuditEvent:         "governance.data_export.download",
		SourceProductArea:  "Governance",
		OperationDisplay:   "Download data export",
		OperationType:      "export",
		EventCategory:      "export",
		RiskLevel:          "high",
		DataClassification: "restricted",
	}), downloadExport(svc))
}

type listAuditEventsInput struct {
	Limit             int    `query:"limit,omitempty" minimum:"1" maximum:"200" doc:"Maximum events to return."`
	Cursor            string `query:"cursor,omitempty" maxLength:"1024"`
	ActorID           string `query:"actor_id,omitempty" maxLength:"255"`
	AuditEvent        string `query:"audit_event,omitempty" maxLength:"255"`
	HighRisk          bool   `query:"high_risk,omitempty" doc:"Return events that are high or critical risk, write/delete/export operations, denials, or errors."`
	OperationID       string `query:"operation_id,omitempty" maxLength:"128"`
	OperationType     string `query:"operation_type,omitempty" enum:"read,write,delete,authn,authz,billing,export,system,unknown"`
	Result            string `query:"result,omitempty" enum:"allowed,denied,error"`
	RiskLevel         string `query:"risk_level,omitempty" enum:"low,medium,high,critical"`
	ServiceName       string `query:"service_name,omitempty" maxLength:"128"`
	SourceProductArea string `query:"source_product_area,omitempty" maxLength:"128"`
	TargetID          string `query:"target_id,omitempty" maxLength:"255"`
	TargetKind        string `query:"target_kind,omitempty" maxLength:"128"`
}

type auditEventsOutput struct {
	Body apiwire.GovernanceAuditEvents
}

func listAuditEvents(svc *governance.Service) func(context.Context, governance.Principal, *listAuditEventsInput) (*auditEventsOutput, error) {
	return func(ctx context.Context, principal governance.Principal, input *listAuditEventsInput) (*auditEventsOutput, error) {
		page, err := svc.ListAuditEvents(ctx, principal, governance.AuditListFilters{
			Limit:             input.Limit,
			Cursor:            input.Cursor,
			ActorID:           input.ActorID,
			AuditEvent:        input.AuditEvent,
			HighRisk:          input.HighRisk,
			OperationID:       input.OperationID,
			OperationType:     input.OperationType,
			Result:            input.Result,
			RiskLevel:         input.RiskLevel,
			ServiceName:       input.ServiceName,
			SourceProductArea: input.SourceProductArea,
			TargetID:          input.TargetID,
			TargetKind:        input.TargetKind,
		})
		if err != nil {
			return nil, err
		}
		out := apiwire.GovernanceAuditEvents{
			Events:     make([]apiwire.GovernanceAuditEvent, 0, len(page.Events)),
			NextCursor: page.NextCursor,
			Limit:      int32(page.Limit),
			Filters: apiwire.GovernanceAuditFilters{
				ActorID:           input.ActorID,
				AuditEvent:        input.AuditEvent,
				HighRisk:          input.HighRisk,
				OperationID:       input.OperationID,
				OperationType:     input.OperationType,
				Result:            input.Result,
				RiskLevel:         input.RiskLevel,
				ServiceName:       input.ServiceName,
				SourceProductArea: input.SourceProductArea,
				TargetID:          input.TargetID,
				TargetKind:        input.TargetKind,
			},
		}
		for _, event := range page.Events {
			out.Events = append(out.Events, auditEventDTO(event))
		}
		return &auditEventsOutput{Body: out}, nil
	}
}

type exportsOutput struct {
	Body apiwire.GovernanceExportJobs
}

func listExports(svc *governance.Service) func(context.Context, governance.Principal, *struct{}) (*exportsOutput, error) {
	return func(ctx context.Context, principal governance.Principal, input *struct{}) (*exportsOutput, error) {
		jobs, err := svc.ListExports(ctx, principal)
		if err != nil {
			return nil, err
		}
		return &exportsOutput{Body: apiwire.GovernanceExportJobs{Exports: exportJobDTOs(jobs, svc.PublicBaseURL)}}, nil
	}
}

type createExportInput struct {
	Body apiwire.GovernanceCreateExportRequest
}

type exportOutput struct {
	Body apiwire.GovernanceExportJob
}

func createExport(svc *governance.Service) func(context.Context, governance.Principal, *createExportInput) (*exportOutput, error) {
	return func(ctx context.Context, principal governance.Principal, input *createExportInput) (*exportOutput, error) {
		info := operationRequestInfoFromContext(ctx)
		job, err := svc.CreateExport(ctx, principal, governance.CreateExportRequest{
			Scopes:         input.Body.Scopes,
			IncludeLogs:    input.Body.IncludeLogs,
			IdempotencyKey: info.IdempotencyKey,
		})
		if err != nil {
			return nil, err
		}
		return &exportOutput{Body: exportJobDTO(*job, svc.PublicBaseURL)}, nil
	}
}

type exportPathInput struct {
	ExportID string `path:"export_id" format:"uuid"`
}

func getExport(svc *governance.Service) func(context.Context, governance.Principal, *exportPathInput) (*exportOutput, error) {
	return func(ctx context.Context, principal governance.Principal, input *exportPathInput) (*exportOutput, error) {
		job, err := svc.GetExport(ctx, principal, input.ExportID)
		if err != nil {
			return nil, err
		}
		return &exportOutput{Body: exportJobDTO(*job, svc.PublicBaseURL)}, nil
	}
}

type downloadOutput struct {
	ContentType        string `header:"Content-Type"`
	ContentDisposition string `header:"Content-Disposition"`
	Body               []byte
}

func downloadExport(svc *governance.Service) func(context.Context, governance.Principal, *exportPathInput) (*downloadOutput, error) {
	return func(ctx context.Context, principal governance.Principal, input *exportPathInput) (*downloadOutput, error) {
		ctx, span := apiTracer.Start(ctx, "governance.export.download")
		defer span.End()
		job, err := svc.GetExport(ctx, principal, input.ExportID)
		if err != nil {
			return nil, err
		}
		if job.State != "completed" || job.ArtifactPath == "" {
			return nil, governance.ErrNotFound
		}
		if time.Now().UTC().After(job.ExpiresAt) {
			return nil, governance.ErrNotFound
		}
		body, err := os.ReadFile(job.ArtifactPath)
		if err != nil {
			return nil, fmt.Errorf("%w: read artifact: %v", governance.ErrStore, err)
		}
		if err := svc.MarkExportDownloaded(ctx, principal, job.ExportID); err != nil {
			return nil, err
		}
		span.SetAttributes(
			attribute.String("forge_metal.org_id", principal.OrgID),
			attribute.String("forge_metal.export_id", job.ExportID.String()),
			attribute.Int("forge_metal.export_bytes", len(body)),
		)
		return &downloadOutput{
			ContentType:        "application/gzip",
			ContentDisposition: fmt.Sprintf(`attachment; filename="forge-metal-%s-%s.tar.gz"`, principal.OrgID, job.ExportID.String()),
			Body:               body,
		}, nil
	}
}

func auditEventDTO(event governance.AuditEvent) apiwire.GovernanceAuditEvent {
	return apiwire.GovernanceAuditEvent{
		EventID:               event.EventID.String(),
		RecordedAt:            event.RecordedAt.UTC().Format(time.RFC3339Nano),
		OrgID:                 event.OrgID,
		SourceProductArea:     event.SourceProductArea,
		ServiceName:           event.ServiceName,
		OperationID:           event.OperationID,
		AuditEvent:            event.AuditEvent,
		OperationDisplay:      event.OperationDisplay,
		OperationType:         event.OperationType,
		EventCategory:         event.EventCategory,
		RiskLevel:             event.RiskLevel,
		ActorType:             event.ActorType,
		ActorID:               event.ActorID,
		ActorDisplay:          event.ActorDisplay,
		ActorOwnerID:          event.ActorOwnerID,
		ActorOwnerDisplay:     event.ActorOwnerDisplay,
		CredentialID:          event.CredentialID,
		CredentialName:        event.CredentialName,
		CredentialFingerprint: event.CredentialFingerprint,
		AuthMethod:            event.AuthMethod,
		MFAPresent:            event.MFAPresent == 1,
		TargetKind:            event.TargetKind,
		TargetID:              event.TargetID,
		TargetDisplay:         event.TargetDisplay,
		TargetScope:           event.TargetScope,
		Permission:            event.Permission,
		Action:                event.Action,
		OrgScope:              event.OrgScope,
		Decision:              event.Decision,
		Result:                event.Result,
		DenialReason:          event.DenialReason,
		TrustClass:            event.TrustClass,
		ErrorCode:             event.ErrorCode,
		ErrorMessage:          event.ErrorMessage,
		ClientIP:              event.ClientIP,
		ClientIPVersion:       event.ClientIPVersion,
		IPChain:               event.IPChain,
		UserAgentHash:         event.UserAgentHash,
		GeoCountry:            event.GeoCountry,
		GeoRegion:             event.GeoRegion,
		ASN:                   strconv.FormatUint(uint64(event.ASN), 10),
		ASNOrg:                event.ASNOrg,
		NetworkType:           event.NetworkType,
		IdempotencyKeyHash:    event.IdempotencyKeyHash,
		RequestID:             event.RequestID,
		TraceID:               event.TraceID,
		Sequence:              strconv.FormatUint(event.Sequence, 10),
		PrevHMAC:              event.PrevHMAC,
		RowHMAC:               event.RowHMAC,
		ContentSHA256:         event.ContentSHA256,
		HMACKeyID:             event.HMACKeyID,
	}
}

func exportJobDTOs(jobs []governance.ExportJob, baseURL string) []apiwire.GovernanceExportJob {
	out := make([]apiwire.GovernanceExportJob, 0, len(jobs))
	for _, job := range jobs {
		out = append(out, exportJobDTO(job, baseURL))
	}
	return out
}

func exportJobDTO(job governance.ExportJob, baseURL string) apiwire.GovernanceExportJob {
	files := make([]apiwire.GovernanceExportFile, 0, len(job.Files))
	for _, file := range job.Files {
		files = append(files, apiwire.GovernanceExportFile{
			Path:        file.Path,
			ContentType: file.ContentType,
			Rows:        strconv.FormatInt(file.Rows, 10),
			Bytes:       strconv.FormatInt(file.Bytes, 10),
			SHA256:      file.SHA256,
		})
	}
	downloadURL := ""
	if job.State == "completed" {
		downloadURL = fmt.Sprintf("/api/v1/governance/exports/%s/download", job.ExportID.String())
	}
	return apiwire.GovernanceExportJob{
		ExportID:       job.ExportID.String(),
		OrgID:          job.OrgID,
		RequestedBy:    job.RequestedBy,
		Scopes:         job.Scopes,
		IncludeLogs:    job.IncludeLogs,
		Format:         job.Format,
		State:          job.State,
		ArtifactSHA256: job.ArtifactSHA256,
		ArtifactBytes:  strconv.FormatInt(job.ArtifactBytes, 10),
		DownloadURL:    downloadURL,
		Files:          files,
		CreatedAt:      job.CreatedAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt:      job.UpdatedAt.UTC().Format(time.RFC3339Nano),
		CompletedAt:    optionalTime(job.CompletedAt),
		ExpiresAt:      job.ExpiresAt.UTC().Format(time.RFC3339Nano),
		ErrorCode:      job.ErrorCode,
		ErrorMessage:   job.ErrorMessage,
	}
}

func optionalTime(value *time.Time) string {
	if value == nil || value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}
