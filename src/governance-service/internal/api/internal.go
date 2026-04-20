package api

import (
	"encoding/json"
	"net/http"

	workloadauth "github.com/forge-metal/auth-middleware/workload"
	"github.com/forge-metal/governance-service/internal/governance"
)

func RegisterInternalRoutes(mux *http.ServeMux, svc *governance.Service) {
	mux.HandleFunc("/internal/v1/audit/events", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		peerID, ok := workloadauth.PeerIDFromContext(r.Context())
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		defer r.Body.Close()
		var record governance.AuditRecord
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&record); err != nil {
			http.Error(w, "invalid audit event", http.StatusBadRequest)
			return
		}
		if record.ActorSPIFFEID == "" {
			record.ActorSPIFFEID = peerID.String()
		}
		if record.CredentialID == "" {
			record.CredentialID = peerID.String()
		}
		if record.AuthMethod == "" {
			record.AuthMethod = "spiffe"
		}
		event, err := svc.RecordAuditEvent(r.Context(), record)
		if err != nil {
			http.Error(w, "audit write failed", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"event_id": event.EventID.String(),
			"sequence": event.Sequence,
			"row_hmac": event.RowHMAC,
		})
	})
}
