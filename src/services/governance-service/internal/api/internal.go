package api

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/verself/governance-service/internal/governance"
	"github.com/verself/governance-service/internal/internalcontractapi"
	workloadauth "github.com/verself/service-runtime/workload"
)

func RegisterInternalRoutes(api huma.API, svc *governance.Service) {
	desc := internalcontractapi.AppendAPIActivity.Descriptor
	huma.Register(api, huma.Operation{
		OperationID:   desc.OperationID,
		Method:        desc.Method,
		Path:          desc.Path,
		Summary:       "Append OCSF API Activity",
		Description:   "SPIFFE-mTLS internal endpoint for repo-owned services to append OCSF API Activity events.",
		DefaultStatus: desc.DefaultStatus,
		MaxBodyBytes:  desc.RequestBodyMaxBytes,
		Errors:        internalContractProblemStatuses(desc.Problems),
		Extensions:    map[string]any{"x-verself-contract": internalContractExtension(desc)},
		Security:      []map[string][]string{{"mutualTLS": {}}},
	}, appendAPIActivity(svc))
}

func appendAPIActivity(svc *governance.Service) func(context.Context, *internalcontractapi.AppendAPIActivityInput) (*internalcontractapi.AppendAPIActivityOutput, error) {
	return func(ctx context.Context, input *internalcontractapi.AppendAPIActivityInput) (*internalcontractapi.AppendAPIActivityOutput, error) {
		peerID, ok := workloadauth.PeerIDFromContext(ctx)
		if !ok {
			return nil, unauthorized(ctx, "missing-workload-identity", "missing SPIFFE peer identity")
		}
		record, err := apiActivityRecordFromContract(input.Body)
		if err != nil {
			return nil, problem(ctx, 400, "invalid-api-activity", "API activity is invalid", err)
		}
		if strings.TrimSpace(record.ActorType) == "" {
			record.ActorType = "workload"
		}
		if strings.TrimSpace(record.ActorID) == "" {
			record.ActorID = peerID.String()
		}
		if strings.TrimSpace(record.CredentialID) == "" {
			record.CredentialID = peerID.String()
		}
		if record.Unmapped == nil {
			record.Unmapped = map[string]any{}
		}
		record.Unmapped["verself.actor_spiffe_id"] = peerID.String()
		record.Unmapped["verself.auth_method"] = "spiffe"
		event, err := svc.RecordAPIActivity(ctx, record)
		if err != nil {
			return nil, mapError(ctx, err)
		}
		return &internalcontractapi.AppendAPIActivityOutput{Body: internalcontractapi.AppendAPIActivityOutputBody{Accepted: internalcontractapi.APIActivityAccepted{
			MetadataUID: internalcontractapi.OCSFMetadataUID(event.MetadataUID.String()),
			Sequence:    internalcontractapi.DecimalUint64(strconv.FormatUint(event.Sequence, 10)),
			RowHMAC:     internalcontractapi.HMACHex(event.RowHMAC),
		}}}, nil
	}
}

func apiActivityRecordFromContract(input internalcontractapi.APIActivityRecord) (governance.APIActivityRecord, error) {
	observedAt, err := optionalContractTime(input.ObservedAt)
	if err != nil {
		return governance.APIActivityRecord{}, err
	}
	unmapped := map[string]any{}
	if input.Unmapped != nil {
		unmapped = *input.Unmapped
	}
	resources := make([]governance.APIActivityResource, 0, len(input.Resources))
	for _, resource := range input.Resources {
		roleID := uint8(0)
		if resource.RoleID != nil {
			roleID = *resource.RoleID
		}
		resources = append(resources, governance.APIActivityResource{
			Type:     string(resource.Type),
			UID:      optionalContractValue(resource.UID),
			Name:     optionalContractValue(resource.Name),
			FullName: optionalContractValue(resource.FullName),
			Role:     optionalContractValue(resource.Role),
			RoleID:   roleID,
		})
	}
	return governance.APIActivityRecord{
		OrgID:            string(input.OrgID),
		APIService:       string(input.APIService),
		APIOperation:     string(input.APIOperation),
		APIEventCode:     string(input.APIEventCode),
		APIAction:        string(input.APIAction),
		APIVersion:       optionalContractValue(input.APIVersion),
		ActorType:        string(input.ActorType),
		ActorID:          string(input.ActorUID),
		ActorDisplayName: optionalContractValue(input.ActorName),
		ActorEmail:       optionalContractValue(input.ActorEmail),
		CredentialID:     optionalContractValue(input.CredentialUID),
		Permission:       string(input.Permission),
		Resources:        resources,
		HTTPRequest: governance.APIActivityHTTPRequest{
			UID:           optionalContractValue(input.HTTPRequest.UID),
			Method:        string(input.HTTPRequest.Method),
			Route:         input.HTTPRequest.Route,
			SafeParams:    optionalContractValue(input.HTTPRequest.SafeParams),
			UserAgent:     optionalContractValue(input.HTTPRequest.UserAgent),
			XForwardedFor: optionalContractValue(input.HTTPRequest.XForwardedFor),
			Referrer:      optionalContractValue(input.HTTPRequest.Referrer),
			Host:          optionalContractValue(input.HTTPRequest.Host),
			Scheme:        optionalContractValue(input.HTTPRequest.Scheme),
			ClientIP:      optionalContractValue(input.HTTPRequest.ClientIP),
			SourceName:    optionalContractValue(input.HTTPRequest.SourceName),
		},
		HTTPResponse: governance.APIActivityHTTPResponse{
			Code:    input.HTTPResponse.Code,
			Message: optionalContractValue(input.HTTPResponse.Message),
			Status:  optionalContractValue(input.HTTPResponse.Status),
		},
		AuthorizationDecision: governance.AuthorizationDecision(input.AuthorizationDecision),
		Status:                governance.Status(input.Status),
		StatusCode:            string(input.StatusCode),
		StatusDetail:          optionalContractValue(input.StatusDetail),
		TraceID:               optionalContractValue(input.TraceUID),
		SpanID:                optionalContractValue(input.SpanUID),
		HMACKeyID:             optionalContractValue(input.HMACKeyID),
		Time:                  observedAt,
		Unmapped:              unmapped,
	}, nil
}

func optionalContractValue[T ~string](value *T) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(string(*value))
}

func optionalContractTime(value *string) (time.Time, error) {
	if value == nil || strings.TrimSpace(*value) == "" {
		return time.Time{}, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(*value))
	if err != nil {
		return time.Time{}, err
	}
	return parsed.UTC(), nil
}

func internalContractExtension(desc internalcontractapi.OperationDescriptor) map[string]any {
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

func internalContractProblemStatuses(problems []internalcontractapi.ProblemDescriptor) []int {
	statuses := make([]int, 0, len(problems))
	for _, problem := range problems {
		if problem.Status > 0 {
			statuses = append(statuses, problem.Status)
		}
	}
	return statuses
}
