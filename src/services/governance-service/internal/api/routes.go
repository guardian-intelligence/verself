package api

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/verself/domain-transfer-objects"
	"github.com/verself/governance-service/internal/contractapi"
	"github.com/verself/governance-service/internal/governance"
	runtimeiam "github.com/verself/service-runtime/iam"
)

var apiTracer = otel.Tracer("governance-service/internal/api")

func RegisterRoutes(api huma.API, svc *governance.Service, authorizer runtimeiam.ResourceAuthorizer) {
	registerSecured(api, svc, authorizer, securedContract(contractapi.ListAuditEvents.Descriptor, "List organization audit events"), listAuditEvents(svc))
	registerSecured(api, svc, authorizer, securedContract(contractapi.ListDataExports.Descriptor, "List organization data exports"), listExports(svc))
	registerSecured(api, svc, authorizer, securedContract(contractapi.CreateDataExport.Descriptor, "Create an organization data export"), createExport(svc))
	registerSecured(api, svc, authorizer, securedContract(contractapi.GetDataExport.Descriptor, "Get an organization data export"), getExport(svc))
	registerSecured(api, svc, authorizer, securedContract(contractapi.DownloadDataExport.Descriptor, "Download an organization data export artifact", func(op *huma.Operation) {
		op.Responses = map[string]*huma.Response{
			"200": {
				Description: "tar.gz export artifact",
				Content: map[string]*huma.MediaType{
					"application/gzip": {Schema: &huma.Schema{Type: "string", Format: "binary"}},
				},
			},
		}
	}), downloadExport(svc))
}

func securedContract(desc contractapi.OperationDescriptor, summary string, options ...func(*huma.Operation)) securedOperation {
	op := huma.Operation{
		OperationID:   desc.OperationID,
		Method:        desc.Method,
		Path:          desc.Path,
		Summary:       summary,
		DefaultStatus: desc.DefaultStatus,
	}
	for _, option := range options {
		option(&op)
	}
	return secured(op, operationPolicyFromContract(desc))
}

func operationPolicyFromContract(desc contractapi.OperationDescriptor) runtimeiam.OperationPolicy {
	idempotency := runtimeiam.IdempotencyPolicy(desc.Idempotency.Policy)
	switch idempotency {
	case runtimeiam.IdempotencyNone, runtimeiam.IdempotencyHeaderKey:
	default:
		panic("unsupported idempotency policy for operation " + desc.OperationID + ": " + desc.Idempotency.Policy)
	}
	return runtimeiam.OperationPolicy{
		Permission:     runtimeiam.Permission(desc.Authorization.Permission),
		Resource:       runtimeiam.ResourceKind(desc.Audit.Resource),
		Action:         runtimeiam.Action(desc.Audit.Action),
		OrgScope:       runtimeiam.OrgScope(desc.Authorization.OrganizationSource),
		RateLimitClass: runtimeiam.RateLimitClass(desc.RateLimitBucket),
		Idempotency:    idempotency,
		AuditEvent:     runtimeiam.AuditEvent(desc.Audit.Event),
		BodyLimitBytes: desc.RequestBodyMaxBytes,
	}
}

type listAuditEventsInput struct {
	Limit              int    `query:"limit,omitempty" minimum:"1" maximum:"200" doc:"Maximum events to return."`
	Cursor             string `query:"cursor,omitempty" maxLength:"64" doc:"Opaque pagination cursor returned by the previous page."`
	Order              string `query:"order,omitempty" enum:"asc,desc" doc:"Time ordering of results; defaults to 'desc' (newest first)."`
	ActorID            string `query:"actor_id,omitempty" maxLength:"255"`
	AuditEvent         string `query:"audit_event,omitempty" maxLength:"255"`
	CredentialID       string `query:"credential_id,omitempty" maxLength:"255"`
	EventName          string `query:"event_name,omitempty" maxLength:"128"`
	EventSource        string `query:"event_source,omitempty" maxLength:"128"`
	Outcome            string `query:"outcome,omitempty" enum:"allowed,denied,error"`
	TargetID           string `query:"target_id,omitempty" maxLength:"255"`
	TargetType         string `query:"target_type,omitempty" maxLength:"128"`
	TargetResourceName string `query:"targetResourceName,omitempty" maxLength:"1024"`
}

type auditEventsOutput struct {
	Body dto.GovernanceAuditEvents
}

func listAuditEvents(svc *governance.Service) func(context.Context, governance.Principal, *listAuditEventsInput) (*auditEventsOutput, error) {
	return func(ctx context.Context, principal governance.Principal, input *listAuditEventsInput) (*auditEventsOutput, error) {
		page, err := svc.ListAuditEvents(ctx, principal, governance.AuditListFilters{
			Limit:              input.Limit,
			Cursor:             input.Cursor,
			Order:              input.Order,
			ActorID:            input.ActorID,
			AuditEvent:         input.AuditEvent,
			CredentialID:       input.CredentialID,
			EventName:          input.EventName,
			EventSource:        input.EventSource,
			Outcome:            input.Outcome,
			TargetID:           input.TargetID,
			TargetType:         input.TargetType,
			TargetResourceName: input.TargetResourceName,
		})
		if err != nil {
			return nil, err
		}
		out := dto.GovernanceAuditEvents{
			Events:     make([]dto.GovernanceAuditEvent, 0, len(page.Events)),
			NextCursor: page.NextCursor,
			Limit:      int32FromInt(page.Limit, "audit page limit"),
			Filters: dto.GovernanceAuditFilters{
				ActorID:            input.ActorID,
				AuditEvent:         input.AuditEvent,
				CredentialID:       input.CredentialID,
				EventName:          input.EventName,
				EventSource:        input.EventSource,
				Outcome:            input.Outcome,
				TargetID:           input.TargetID,
				TargetType:         input.TargetType,
				TargetResourceName: dto.ResourceName(input.TargetResourceName),
			},
		}
		for _, event := range page.Events {
			out.Events = append(out.Events, auditEventDTO(event))
		}
		return &auditEventsOutput{Body: out}, nil
	}
}

type exportsOutput struct {
	Body dto.GovernanceExportJobs
}

func listExports(svc *governance.Service) func(context.Context, governance.Principal, *struct{}) (*exportsOutput, error) {
	return func(ctx context.Context, principal governance.Principal, input *struct{}) (*exportsOutput, error) {
		jobs, err := svc.ListExports(ctx, principal)
		if err != nil {
			return nil, err
		}
		return &exportsOutput{Body: dto.GovernanceExportJobs{Exports: exportJobDTOs(jobs, svc.PublicBaseURL, svc.InstallationID)}}, nil
	}
}

type createExportInput struct {
	Body dto.GovernanceCreateExportRequest
}

type exportOutput struct {
	Body dto.GovernanceExportJob
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
		return &exportOutput{Body: exportJobDTO(*job, svc.PublicBaseURL, svc.InstallationID)}, nil
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
		return &exportOutput{Body: exportJobDTO(*job, svc.PublicBaseURL, svc.InstallationID)}, nil
	}
}

func downloadExport(svc *governance.Service) func(context.Context, governance.Principal, *exportPathInput) (*contractapi.DownloadDataExportOutput, error) {
	return func(ctx context.Context, principal governance.Principal, input *exportPathInput) (*contractapi.DownloadDataExportOutput, error) {
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
			attribute.String("verself.org_id", principal.OrgID),
			attribute.String("verself.export_id", job.ExportID.String()),
			attribute.Int("verself.export_bytes", len(body)),
		)
		return &contractapi.DownloadDataExportOutput{
			ContentType:        contractapi.MediaType("application/gzip"),
			ContentDisposition: contractapi.ContentDisposition(fmt.Sprintf(`attachment; filename="verself-%s-%s.tar.gz"`, principal.OrgID, job.ExportID.String())),
			Body:               body,
		}, nil
	}
}

func auditEventDTO(event governance.AuditEvent) dto.GovernanceAuditEvent {
	return dto.GovernanceAuditEvent{
		EventID:            event.EventID.String(),
		RecordedAt:         event.RecordedAt.UTC().Format(time.RFC3339Nano),
		OrgID:              event.OrgID,
		Sequence:           strconv.FormatUint(event.Sequence, 10),
		EventSource:        event.EventSource,
		EventName:          event.EventName,
		AuditEvent:         event.AuditEvent,
		ActorType:          event.ActorType,
		ActorID:            event.ActorID,
		CredentialID:       event.CredentialID,
		TargetType:         event.TargetType,
		TargetID:           event.TargetID,
		TargetResourceName: dto.ResourceName(event.TargetResourceName),
		Permission:         event.Permission,
		Outcome:            event.Outcome,
		ErrorCode:          event.ErrorCode,
		TraceID:            event.TraceID,
		DetailSHA256:       event.DetailSHA256,
		PrevHMAC:           event.PrevHMAC,
		RowHMAC:            event.RowHMAC,
		HMACKeyID:          event.HMACKeyID,
	}
}

func exportJobDTOs(jobs []governance.ExportJob, baseURL string, installationID string) []dto.GovernanceExportJob {
	out := make([]dto.GovernanceExportJob, 0, len(jobs))
	for _, job := range jobs {
		out = append(out, exportJobDTO(job, baseURL, installationID))
	}
	return out
}

func exportJobDTO(job governance.ExportJob, baseURL string, installationID string) dto.GovernanceExportJob {
	files := make([]dto.GovernanceExportFile, 0, len(job.Files))
	for _, file := range job.Files {
		files = append(files, dto.GovernanceExportFile{
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
	return dto.GovernanceExportJob{
		ExportID:       job.ExportID.String(),
		ResourceName:   dto.ResourceNameAuditExport(installationID, job.OrgID, job.ExportID.String()),
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
