package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/verself/email-service/internal/app"
	"github.com/verself/email-service/internal/mailstore"
)

func RegisterInternalRoutes(mux *http.ServeMux, svc interface {
	SendEmail(context.Context, app.SendEmailRequest) (app.SendEmailResult, error)
},
) {
	mux.HandleFunc("/internal/v1/email/send", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		defer func() { _ = r.Body.Close() }()
		var input app.SendEmailRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&input); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		input.RequireSendAs = false
		result, err := svc.SendEmail(r.Context(), input)
		if err != nil {
			if errors.Is(err, mailstore.ErrNotFound) {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
			if errors.Is(err, mailstore.ErrIdempotencyPayloadMismatch) {
				http.Error(w, err.Error(), http.StatusConflict)
				return
			}
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(result); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	})
}
