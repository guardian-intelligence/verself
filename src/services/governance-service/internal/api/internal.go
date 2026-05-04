package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/verself/governance-service/internal/governance"
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
	huma.Register(api, huma.Operation{
		OperationID: "append-audit-event",
		Method:      http.MethodPost,
		Path:        "/internal/v1/audit/events",
		Summary:     "Append governance audit event",
		Description: "SPIFFE-mTLS internal endpoint for repo-owned services to append governance audit events.",
		Security:    []map[string][]string{{"mutualTLS": {}}},
	}, appendAuditEvent(svc))
}

func appendAuditEvent(svc *governance.Service) func(context.Context, *appendAuditEventInput) (*appendAuditEventOutput, error) {
	return func(ctx context.Context, input *appendAuditEventInput) (*appendAuditEventOutput, error) {
		peerID, ok := workloadauth.PeerIDFromContext(ctx)
		if !ok {
			return nil, unauthorized(ctx, "missing-workload-identity", "missing SPIFFE peer identity")
		}
		record := input.Body
		if strings.TrimSpace(record.ActorSPIFFEID) == "" {
			record.ActorSPIFFEID = peerID.String()
		}
		if strings.TrimSpace(record.CredentialID) == "" {
			record.CredentialID = peerID.String()
		}
		if strings.TrimSpace(record.AuthMethod) == "" {
			record.AuthMethod = "spiffe"
		}
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
