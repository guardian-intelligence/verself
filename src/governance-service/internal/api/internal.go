package api

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/forge-metal/governance-service/internal/governance"
)

func RegisterInternalRoutes(mux *http.ServeMux, svc *governance.Service, token string) {
	mux.HandleFunc("/internal/v1/audit/events", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !validInternalBearer(token, r.Header.Get("Authorization")) {
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

func validInternalBearer(expected, header string) bool {
	expected = strings.TrimSpace(expected)
	header = strings.TrimSpace(header)
	if expected == "" || !strings.HasPrefix(header, "Bearer ") {
		return false
	}
	got := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
	if len(got) != len(expected) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(expected)) == 1
}
