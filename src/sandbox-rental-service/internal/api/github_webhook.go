package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/forge-metal/sandbox-rental-service/internal/jobs"
)

const (
	githubActionsWebhookPath       = "/webhooks/github/actions"
	githubInstallationCallbackPath = "/github/installations/callback"
	publicWebhookBodyLimit         = 1 << 20
)

type githubActionsWebhookResponse struct {
	Status     string `json:"status"`
	DeliveryID string `json:"delivery_id,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

func RegisterPublicRoutes(mux *http.ServeMux, svc *jobs.Service) {
	if mux == nil || svc == nil {
		return
	}
	mux.HandleFunc(githubActionsWebhookPath, githubActionsWebhookHandler(svc))
	mux.HandleFunc(githubInstallationCallbackPath, githubInstallationCallbackHandler(svc))
}

func githubActionsWebhookHandler(svc *jobs.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if svc.GitHubRunner == nil || !svc.GitHubRunner.Configured() {
			http.Error(w, "github runner is not configured", http.StatusServiceUnavailable)
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, publicWebhookBodyLimit))
		if err != nil {
			var maxBytesErr *http.MaxBytesError
			if errors.As(err, &maxBytesErr) {
				http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
				return
			}
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		eventName := strings.TrimSpace(r.Header.Get("X-GitHub-Event"))
		deliveryID := strings.TrimSpace(r.Header.Get("X-GitHub-Delivery"))
		if eventName == "" || deliveryID == "" {
			http.Error(w, "missing github event headers", http.StatusBadRequest)
			return
		}
		ctx := jobs.WithCorrelationID(r.Context(), deliveryID)
		if err := svc.GitHubRunner.HandleWebhook(ctx, eventName, deliveryID, body, r.Header.Get("X-Hub-Signature-256")); err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, jobs.ErrGitHubRunnerNotConfigured) {
				status = http.StatusServiceUnavailable
			}
			writePublicWebhookError(w, status, err)
			return
		}
		status := "recorded"
		reason := ""
		if eventName != "workflow_job" {
			status = "ignored"
			reason = "unsupported event"
		}
		writeGitHubActionsWebhookResponse(w, http.StatusAccepted, githubActionsWebhookResponse{
			Status:     status,
			DeliveryID: deliveryID,
			Reason:     reason,
		})
	}
}

func writeGitHubActionsWebhookResponse(w http.ResponseWriter, status int, response githubActionsWebhookResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(response)
}

func githubInstallationCallbackHandler(svc *jobs.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if svc.GitHubRunner == nil || !svc.GitHubRunner.Configured() {
			http.Error(w, "github runner is not configured", http.StatusServiceUnavailable)
			return
		}
		query := r.URL.Query()
		state := strings.TrimSpace(query.Get("state"))
		code := strings.TrimSpace(query.Get("code"))
		rawInstallationID := strings.TrimSpace(query.Get("installation_id"))
		installationID, err := strconv.ParseInt(rawInstallationID, 10, 64)
		if err != nil || installationID <= 0 || state == "" || code == "" {
			http.Error(w, "invalid github installation callback", http.StatusBadRequest)
			return
		}
		record, err := svc.GitHubRunner.CompleteInstallation(r.Context(), state, code, installationID)
		if err != nil {
			status := http.StatusInternalServerError
			switch {
			case errors.Is(err, jobs.ErrGitHubRunnerNotConfigured):
				status = http.StatusServiceUnavailable
			case errors.Is(err, jobs.ErrGitHubInstallationInvalid), errors.Is(err, jobs.ErrGitHubInstallationStateInvalid):
				status = http.StatusBadRequest
			}
			writePublicWebhookError(w, status, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(githubInstallationRecord(record))
	}
}

func writePublicWebhookError(w http.ResponseWriter, status int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error": strings.TrimSpace(err.Error()),
	})
}
