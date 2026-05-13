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
	desc := internalcontractapi.AppendAuditEvent.Descriptor
	huma.Register(api, huma.Operation{
		OperationID:   desc.OperationID,
		Method:        desc.Method,
		Path:          desc.Path,
		Summary:       "Append governance audit event",
		Description:   "SPIFFE-mTLS internal endpoint for repo-owned services to append governance audit events.",
		DefaultStatus: desc.DefaultStatus,
		MaxBodyBytes:  desc.RequestBodyMaxBytes,
		Errors:        internalContractProblemStatuses(desc.Problems),
		Extensions:    map[string]any{"x-verself-contract": internalContractExtension(desc)},
		Security:      []map[string][]string{{"mutualTLS": {}}},
	}, appendAuditEvent(svc))
}

func appendAuditEvent(svc *governance.Service) func(context.Context, *internalcontractapi.AppendAuditEventInput) (*internalcontractapi.AppendAuditEventOutput, error) {
	return func(ctx context.Context, input *internalcontractapi.AppendAuditEventInput) (*internalcontractapi.AppendAuditEventOutput, error) {
		peerID, ok := workloadauth.PeerIDFromContext(ctx)
		if !ok {
			return nil, unauthorized(ctx, "missing-workload-identity", "missing SPIFFE peer identity")
		}
		record, err := auditRecordFromContract(input.Body)
		if err != nil {
			return nil, problem(ctx, 400, "invalid-audit-record", "audit record is invalid", err)
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
		if record.Detail == nil {
			record.Detail = map[string]any{}
		}
		record.Detail["actor_spiffe_id"] = peerID.String()
		record.Detail["auth_method"] = "spiffe"
		event, err := svc.RecordAuditEvent(ctx, record)
		if err != nil {
			return nil, mapError(ctx, err)
		}
		return &internalcontractapi.AppendAuditEventOutput{Body: internalcontractapi.AppendAuditEventOutputBody{Accepted: internalcontractapi.AppendAuditEventAccepted{
			EventID:  internalcontractapi.AuditEventID(event.EventID.String()),
			Sequence: internalcontractapi.DecimalUint64(strconv.FormatUint(event.Sequence, 10)),
			RowHMAC:  internalcontractapi.HMACHex(event.RowHMAC),
		}}}, nil
	}
}

func auditRecordFromContract(input internalcontractapi.AuditRecord) (governance.AuditRecord, error) {
	recordedAt, err := optionalContractTime(input.RecordedAt)
	if err != nil {
		return governance.AuditRecord{}, err
	}
	detail := map[string]any{}
	if input.Detail != nil {
		detail = *input.Detail
	}
	return governance.AuditRecord{
		SchemaVersion:      optionalContractValue(input.SchemaVersion),
		OrgID:              string(input.OrgID),
		EventSource:        string(input.EventSource),
		EventName:          string(input.EventName),
		AuditEvent:         string(input.AuditEvent),
		ActorType:          string(input.ActorType),
		ActorID:            string(input.ActorID),
		CredentialID:       optionalContractValue(input.CredentialID),
		TargetType:         string(input.TargetType),
		TargetID:           optionalContractValue(input.TargetID),
		TargetResourceName: optionalContractValue(input.TargetResourceName),
		Permission:         string(input.Permission),
		Outcome:            string(input.Outcome),
		ErrorCode:          optionalContractValue(input.ErrorCode),
		TraceID:            optionalContractValue(input.TraceID),
		HMACKeyID:          optionalContractValue(input.HMACKeyID),
		Detail:             detail,
		RecordedAt:         recordedAt,
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
		"audit_event":            desc.Audit.Event,
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
