package api

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/verself/governance-service/internal/contractapi"
	"github.com/verself/governance-service/internal/governance"
	runtimeiam "github.com/verself/service-runtime/iam"
)

var apiTracer = otel.Tracer("governance-service/internal/api")

func RegisterRoutes(api huma.API, svc *governance.Service, authorizer runtimeiam.ResourceAuthorizer) {
	registerSecured(api, svc, authorizer, securedContract(contractapi.ListAPIActivities.Descriptor, "List organization OCSF API Activity events"), listAPIActivities(svc))
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
		Errors:        contractProblemStatuses(desc.Problems),
		Extensions:    map[string]any{"x-verself-contract": contractExtension(desc)},
	}
	for _, option := range options {
		option(&op)
	}
	return secured(op, operationPolicyFromContract(desc))
}

func contractExtension(desc contractapi.OperationDescriptor) map[string]any {
	return map[string]any{
		"shape_id":               desc.ShapeID,
		"operation_id":           desc.OperationID,
		"identity":               desc.Identity.Mode,
		"audience":               desc.Identity.Audience,
		"permission":             desc.Authorization.Permission,
		"organization_source":    desc.Authorization.OrganizationSource,
		"organization_member":    desc.Authorization.OrganizationMember,
		"api_event_code":         desc.Audit.Event,
		"resource":               desc.Audit.Resource,
		"action":                 desc.Audit.Action,
		"rate_limit_bucket":      desc.RateLimitBucket,
		"request_body_max_bytes": desc.RequestBodyMaxBytes,
		"idempotency":            desc.Idempotency.Policy,
	}
}

func contractProblemStatuses(problems []contractapi.ProblemDescriptor) []int {
	statuses := make([]int, 0, len(problems))
	for _, problem := range problems {
		if problem.Status > 0 {
			statuses = append(statuses, problem.Status)
		}
	}
	sort.Ints(statuses)
	out := statuses[:0]
	previous := 0
	for _, status := range statuses {
		if status == previous {
			continue
		}
		out = append(out, status)
		previous = status
	}
	return out
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

func listAPIActivities(svc *governance.Service) func(context.Context, governance.Principal, *contractapi.ListAPIActivitiesInput) (*contractapi.ListAPIActivitiesOutput, error) {
	return func(ctx context.Context, principal governance.Principal, input *contractapi.ListAPIActivitiesInput) (*contractapi.ListAPIActivitiesOutput, error) {
		page, err := svc.ListAPIActivities(ctx, principal, governance.APIActivityListFilters{
			Limit:         int(input.Limit),
			Cursor:        string(input.Cursor),
			Order:         string(input.Order),
			ActorUID:      string(input.ActorUID),
			ActorType:     string(input.ActorType),
			APIService:    string(input.APIService),
			APIOperation:  string(input.APIOperation),
			ActivityID:    input.ActivityID,
			CredentialUID: string(input.CredentialUID),
			ResourceUID:   string(input.ResourceUID),
			ResourceType:  string(input.ResourceType),
			StatusID:      input.StatusID,
			StatusCode:    input.StatusCode,
			TraceUID:      string(input.TraceUID),
		})
		if err != nil {
			return nil, err
		}
		out := contractapi.ListAPIActivitiesOutput{
			Body: contractapi.ListAPIActivitiesOutputBody{
				APIActivities: make(contractapi.APIActivityEvents, 0, len(page.Events)),
				NextCursor:    optionalContractString[contractapi.APIActivityCursor](page.NextCursor),
				Limit:         contractapi.APIActivityEventsLimit(page.Limit),
				Filters: contractapi.APIActivityFilters{
					ActorUID:      optionalContractString[contractapi.ActorUID](string(input.ActorUID)),
					ActorType:     optionalContractString[contractapi.ActorType](string(input.ActorType)),
					APIService:    optionalContractString[contractapi.APIActivityService](string(input.APIService)),
					APIOperation:  optionalContractString[contractapi.APIActivityOperation](string(input.APIOperation)),
					ActivityID:    optionalUint8(input.ActivityID),
					CredentialUID: optionalContractString[contractapi.CredentialUID](string(input.CredentialUID)),
					ResourceUID:   optionalContractString[contractapi.ResourceUID](string(input.ResourceUID)),
					ResourceType:  optionalContractString[contractapi.ResourceType](string(input.ResourceType)),
					StatusID:      optionalUint8(input.StatusID),
					StatusCode:    optionalContractString[string](input.StatusCode),
					TraceUID:      optionalContractString[contractapi.TraceID](string(input.TraceUID)),
				},
			},
		}
		for _, event := range page.Events {
			out.Body.APIActivities = append(out.Body.APIActivities, apiActivityDTO(event))
		}
		return &out, nil
	}
}

func listExports(svc *governance.Service) func(context.Context, governance.Principal, *contractapi.ListDataExportsInput) (*contractapi.ListDataExportsOutput, error) {
	return func(ctx context.Context, principal governance.Principal, input *contractapi.ListDataExportsInput) (*contractapi.ListDataExportsOutput, error) {
		jobs, err := svc.ListExports(ctx, principal)
		if err != nil {
			return nil, err
		}
		return &contractapi.ListDataExportsOutput{Body: contractapi.ListDataExportsOutputBody{Exports: exportJobDTOs(jobs, svc.PublicBaseURL, svc.InstallationID)}}, nil
	}
}

func createExport(svc *governance.Service) func(context.Context, governance.Principal, *contractapi.CreateDataExportInput) (*contractapi.CreateDataExportOutput, error) {
	return func(ctx context.Context, principal governance.Principal, input *contractapi.CreateDataExportInput) (*contractapi.CreateDataExportOutput, error) {
		info := operationRequestInfoFromContext(ctx)
		scopes := make([]string, 0)
		if input.Body.Scopes != nil {
			for _, scope := range *input.Body.Scopes {
				scopes = append(scopes, string(scope))
			}
		}
		includeLogs := false
		if input.Body.IncludeLogs != nil {
			includeLogs = *input.Body.IncludeLogs
		}
		job, err := svc.CreateExport(ctx, principal, governance.CreateExportRequest{
			Scopes:         scopes,
			IncludeLogs:    includeLogs,
			IdempotencyKey: info.IdempotencyKey,
		})
		if err != nil {
			return nil, err
		}
		return &contractapi.CreateDataExportOutput{Body: contractapi.CreateDataExportOutputBody{Export: exportJobDTO(*job, svc.PublicBaseURL, svc.InstallationID)}}, nil
	}
}

func getExport(svc *governance.Service) func(context.Context, governance.Principal, *contractapi.GetDataExportInput) (*contractapi.GetDataExportOutput, error) {
	return func(ctx context.Context, principal governance.Principal, input *contractapi.GetDataExportInput) (*contractapi.GetDataExportOutput, error) {
		job, err := svc.GetExport(ctx, principal, string(input.ExportID))
		if err != nil {
			return nil, err
		}
		return &contractapi.GetDataExportOutput{Body: contractapi.GetDataExportOutputBody{Export: exportJobDTO(*job, svc.PublicBaseURL, svc.InstallationID)}}, nil
	}
}

func downloadExport(svc *governance.Service) func(context.Context, governance.Principal, *contractapi.DownloadDataExportInput) (*contractapi.DownloadDataExportOutput, error) {
	return func(ctx context.Context, principal governance.Principal, input *contractapi.DownloadDataExportInput) (*contractapi.DownloadDataExportOutput, error) {
		ctx, span := apiTracer.Start(ctx, "governance.export.download")
		defer span.End()
		job, err := svc.GetExport(ctx, principal, string(input.ExportID))
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

func apiActivityDTO(event governance.APIActivityRow) contractapi.APIActivityEvent {
	return contractapi.APIActivityEvent{
		MetadataUID:             contractapi.OCSFMetadataUID(event.MetadataUID.String()),
		Time:                    event.Time.UTC().Format(time.RFC3339Nano),
		OrgID:                   contractapi.GovernanceOrgID(event.OrgID),
		Sequence:                contractapi.DecimalUint64(strconv.FormatUint(event.Sequence, 10)),
		OCSFVersion:             event.OCSFVersion,
		CategoryUID:             event.CategoryUID,
		CategoryName:            event.CategoryName,
		ClassUID:                event.ClassUID,
		ClassName:               contractapi.OCSFClassName(event.ClassName),
		TypeUID:                 event.TypeUID,
		ActivityID:              event.ActivityID,
		ActivityName:            event.ActivityName,
		ActionID:                event.ActionID,
		Action:                  event.Action,
		StatusID:                event.StatusID,
		Status:                  contractapi.OCSFStatus(event.Status),
		StatusCode:              event.StatusCode,
		SeverityID:              event.SeverityID,
		Severity:                event.Severity,
		APIService:              contractapi.APIActivityService(event.APIService),
		APIOperation:            contractapi.APIActivityOperation(event.APIOperation),
		APIVersion:              event.APIVersion,
		ActorType:               contractapi.ActorType(event.ActorType),
		ActorUID:                contractapi.ActorUID(event.ActorUID),
		ActorName:               event.ActorName,
		CredentialUID:           optionalContractString[contractapi.CredentialUID](event.CredentialUID),
		PrimaryResourceType:     contractapi.ResourceType(event.PrimaryResourceType),
		PrimaryResourceUID:      optionalContractString[contractapi.ResourceUID](event.PrimaryResourceUID),
		PrimaryResourceName:     event.PrimaryResourceName,
		PrimaryResourceFullName: optionalContractString[contractapi.ResourceName](event.PrimaryResourceFullName),
		Permission:              contractapi.GovernancePermissionName(event.Permission),
		HTTPRequestUID:          optionalContractString[contractapi.RequestID](event.HTTPRequestUID),
		HTTPMethod:              event.HTTPMethod,
		HTTPRoute:               event.HTTPRoute,
		HTTPArgs:                event.HTTPArgs,
		HTTPUserAgent:           event.HTTPUserAgent,
		SrcEndpointIP:           event.SrcEndpointIP,
		SrcEndpointName:         event.SrcEndpointName,
		HTTPResponseCode:        event.HTTPResponseCode,
		TraceUID:                optionalContractString[contractapi.TraceID](event.TraceUID),
		SpanUID:                 optionalContractString[contractapi.SpanID](event.SpanUID),
		OCSFSHA256:              contractapi.SHA256Hex(event.OCSFSHA256),
		PrevHMAC:                contractapi.HMACHex(event.PrevHMAC),
		RowHMAC:                 contractapi.HMACHex(event.RowHMAC),
		HMACKeyID:               optionalContractString[string](event.HMACKeyID),
	}
}

func exportJobDTOs(jobs []governance.ExportJob, baseURL string, installationID string) contractapi.DataExportJobs {
	out := make(contractapi.DataExportJobs, 0, len(jobs))
	for _, job := range jobs {
		out = append(out, exportJobDTO(job, baseURL, installationID))
	}
	return out
}

func exportJobDTO(job governance.ExportJob, baseURL string, installationID string) contractapi.DataExportJob {
	files := make(contractapi.DataExportFiles, 0, len(job.Files))
	for _, file := range job.Files {
		files = append(files, contractapi.DataExportFile{
			Path:        contractapi.ExportArtifactPath(file.Path),
			ContentType: contractapi.MediaType(file.ContentType),
			Rows:        contractapi.DecimalUint64(strconv.FormatInt(file.Rows, 10)),
			Bytes:       contractapi.DecimalUint64(strconv.FormatInt(file.Bytes, 10)),
			SHA256:      contractapi.SHA256Hex(file.SHA256),
		})
	}
	downloadURL := ""
	if job.State == "completed" {
		downloadURL = fmt.Sprintf("/api/v1/governance/exports/%s/download", job.ExportID.String())
	}
	return contractapi.DataExportJob{
		ExportID:       contractapi.DataExportID(job.ExportID.String()),
		ResourceName:   contractapi.ResourceName(governance.ResourceNameDataExport(installationID, job.OrgID, job.ExportID.String()).String()),
		OrgID:          contractapi.GovernanceOrgID(job.OrgID),
		RequestedBy:    contractapi.ActorUID(job.RequestedBy),
		Scopes:         exportScopes(job.Scopes),
		IncludeLogs:    job.IncludeLogs,
		Format:         contractapi.ExportFormat(job.Format),
		State:          contractapi.ExportState(job.State),
		ArtifactSHA256: optionalContractString[contractapi.SHA256Hex](job.ArtifactSHA256),
		ArtifactBytes:  contractapi.DecimalUint64(strconv.FormatInt(job.ArtifactBytes, 10)),
		DownloadURL:    optionalContractString[contractapi.ExportDownloadURL](downloadURL),
		Files:          files,
		CreatedAt:      job.CreatedAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt:      job.UpdatedAt.UTC().Format(time.RFC3339Nano),
		CompletedAt:    optionalContractString[string](optionalTime(job.CompletedAt)),
		ExpiresAt:      job.ExpiresAt.UTC().Format(time.RFC3339Nano),
		ErrorCode:      optionalContractString[contractapi.ExportErrorCode](job.ErrorCode),
		ErrorMessage:   optionalContractString[contractapi.ExportErrorMessage](job.ErrorMessage),
	}
}

func exportScopes(scopes []string) contractapi.ExportScopes {
	out := make(contractapi.ExportScopes, 0, len(scopes))
	for _, scope := range scopes {
		out = append(out, contractapi.ExportScope(scope))
	}
	return out
}

func optionalContractString[T ~string](value string) *T {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	typed := T(value)
	return &typed
}

func optionalUint8(value uint8) *uint8 {
	if value == 0 {
		return nil
	}
	return &value
}

func optionalTime(value *time.Time) string {
	if value == nil || value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}
