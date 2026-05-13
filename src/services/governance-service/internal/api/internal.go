package api

import (
	"context"
	"strconv"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/verself/governance-service/internal/governance"
	"github.com/verself/governance-service/internal/internalcontractapi"
	workloadauth "github.com/verself/service-runtime/workload"
)

type appendAuditEventInput struct {
	Body governance.AuditRecord
}

type appendAuditEventOutput struct {
	Body appendAuditEventAccepted
}

type appendAuditEventAccepted struct {
	EventID  string `json:"event_id"`
	Sequence string `json:"sequence"`
	RowHMAC  string `json:"row_hmac"`
}

func RegisterInternalRoutes(api huma.API, svc *governance.Service) {
	desc := internalcontractapi.AppendAuditEvent.Descriptor
	huma.Register(api, huma.Operation{
		OperationID:   desc.OperationID,
		Method:        desc.Method,
		Path:          desc.Path,
		Summary:       "Append governance audit event",
		Description:   "SPIFFE-mTLS internal endpoint for repo-owned services to append governance audit events.",
		DefaultStatus: desc.DefaultStatus,
		Security:      []map[string][]string{{"mutualTLS": {}}},
	}, appendAuditEvent(svc))
}

func appendAuditEvent(svc *governance.Service) func(context.Context, *appendAuditEventInput) (*appendAuditEventOutput, error) {
	return func(ctx context.Context, input *appendAuditEventInput) (*appendAuditEventOutput, error) {
		peerID, ok := workloadauth.PeerIDFromContext(ctx)
		if !ok {
			return nil, unauthorized(ctx, "missing-workload-identity", "missing SPIFFE peer identity")
		}
		record := input.Body
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
		return &appendAuditEventOutput{Body: appendAuditEventAccepted{
			EventID:  event.EventID.String(),
			Sequence: strconv.FormatUint(event.Sequence, 10),
			RowHMAC:  event.RowHMAC,
		}}, nil
	}
}
