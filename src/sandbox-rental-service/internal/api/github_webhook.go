package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/forge-metal/sandbox-rental-service/internal/jobs"
)

const (
	githubActionsWebhookPath       = "/webhooks/github/actions"
	forgejoActionsWebhookPath      = "/webhooks/forgejo/actions"
	githubInstallationCallbackPath = "/github/installations/callback"
	githubRunnerJITConfigPath      = "/internal/sandbox/v1/github-runner-jit"
	runnerBootstrapConfigPath      = "/internal/sandbox/v1/runner-bootstrap"
	runnerBootstrapTokenHeader     = "X-Forge-Metal-Runner-Bootstrap"
	githubStickyDiskSavePath       = "/internal/sandbox/v1/stickydisk/save"
	githubCheckoutBundlePath       = "/internal/sandbox/v1/github-checkout/bundle"
	publicWebhookBodyLimit         = 1 << 20
)

type githubActionsWebhookResponse struct {
	Status     string `json:"status"`
	DeliveryID string `json:"delivery_id,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

type githubCheckoutBundleRequest struct {
	Repository  string `json:"repository"`
	Ref         string `json:"ref"`
	SHA         string `json:"sha"`
	GitHubToken string `json:"github_token"`
}

type githubStickyDiskSaveRequest struct {
	Key  string `json:"key"`
	Path string `json:"path"`
}

func RegisterPublicRoutes(mux *http.ServeMux, svc *jobs.Service) {
	if mux == nil || svc == nil {
		return
	}
	mux.HandleFunc(githubActionsWebhookPath, githubActionsWebhookHandler(svc))
	mux.HandleFunc(forgejoActionsWebhookPath, forgejoActionsWebhookHandler(svc))
	mux.HandleFunc(githubInstallationCallbackPath, githubInstallationCallbackHandler(svc))
	mux.HandleFunc(githubRunnerJITConfigPath, githubRunnerJITConfigHandler(svc))
	mux.HandleFunc(runnerBootstrapConfigPath, runnerBootstrapConfigHandler(svc))
	mux.HandleFunc(githubStickyDiskSavePath, githubStickyDiskSaveHandler(svc))
	mux.HandleFunc(githubCheckoutBundlePath, githubCheckoutBundleHandler(svc))
}

func forgejoActionsWebhookHandler(svc *jobs.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if svc.ForgejoRunner == nil || !svc.ForgejoRunner.Configured() {
			http.Error(w, "forgejo runner is not configured", http.StatusServiceUnavailable)
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
		eventName := strings.TrimSpace(firstNonEmpty(r.Header.Get("X-Forgejo-Event"), r.Header.Get("X-Gitea-Event")))
		deliveryID := strings.TrimSpace(firstNonEmpty(r.Header.Get("X-Forgejo-Delivery"), r.Header.Get("X-Gitea-Delivery")))
		signature := strings.TrimSpace(r.Header.Get("X-Forgejo-Signature"))
		if signature == "" {
			signature = strings.TrimSpace(r.Header.Get("X-Gitea-Signature"))
		}
		if eventName == "" || deliveryID == "" {
			http.Error(w, "missing forgejo event headers", http.StatusBadRequest)
			return
		}
		ctx := jobs.WithCorrelationID(r.Context(), deliveryID)
		if err := svc.ForgejoRunner.HandleWebhook(ctx, eventName, deliveryID, body, signature); err != nil {
			status := http.StatusInternalServerError
			switch {
			case errors.Is(err, jobs.ErrForgejoRunnerNotConfigured):
				status = http.StatusServiceUnavailable
			case errors.Is(err, jobs.ErrForgejoWebhookSignatureInvalid):
				status = http.StatusUnauthorized
			}
			writePublicWebhookError(w, status, err)
			return
		}
		writeGitHubActionsWebhookResponse(w, http.StatusAccepted, githubActionsWebhookResponse{
			Status:     "recorded",
			DeliveryID: deliveryID,
		})
	}
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
			switch {
			case errors.Is(err, jobs.ErrGitHubRunnerNotConfigured):
				status = http.StatusServiceUnavailable
			case errors.Is(err, jobs.ErrGitHubWebhookSignatureInvalid):
				status = http.StatusUnauthorized
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

func githubRunnerJITConfigHandler(svc *jobs.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if svc.GitHubRunner == nil || !svc.GitHubRunner.Configured() {
			http.Error(w, "github runner is not configured", http.StatusServiceUnavailable)
			return
		}
		token := strings.TrimSpace(r.Header.Get(runnerBootstrapTokenHeader))
		if token == "" {
			http.Error(w, "missing token", http.StatusBadRequest)
			return
		}
		config, err := svc.GitHubRunner.ConsumeJITConfig(r.Context(), token)
		if err != nil {
			writePublicWebhookError(w, http.StatusNotFound, err)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte(config))
	}
}

func runnerBootstrapConfigHandler(svc *jobs.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		token := strings.TrimSpace(r.Header.Get(runnerBootstrapTokenHeader))
		if token == "" {
			http.Error(w, "missing token", http.StatusBadRequest)
			return
		}
		if svc.ForgejoRunner == nil || !svc.ForgejoRunner.Configured() {
			http.Error(w, "forgejo runner is not configured", http.StatusServiceUnavailable)
			return
		}
		config, err := svc.ForgejoRunner.ConsumeBootstrapConfig(r.Context(), token)
		if err != nil {
			writePublicWebhookError(w, http.StatusNotFound, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte(config))
	}
}

func githubStickyDiskSaveHandler(svc *jobs.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if svc.GitHubRunner == nil || !svc.GitHubRunner.Configured() {
			http.Error(w, "github runner is not configured", http.StatusServiceUnavailable)
			return
		}
		identity, ok := authenticateStickyDiskRequest(w, r, svc)
		if !ok {
			return
		}
		var req githubStickyDiskSaveRequest
		body := http.MaxBytesReader(w, r.Body, 16<<10)
		if err := json.NewDecoder(body).Decode(&req); err != nil {
			writeStickyDiskError(w, jobs.ErrStickyDiskInvalid)
			return
		}
		if strings.TrimSpace(req.Key) == "" {
			req.Key = strings.TrimSpace(r.URL.Query().Get("key"))
		}
		save, err := svc.GitHubRunner.RequestStickyDiskCommit(r.Context(), identity, req.Key, req.Path)
		if err != nil {
			writeStickyDiskError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"state":     "queued",
			"commit_id": save.CommitID.String(),
		})
	}
}

func githubCheckoutBundleHandler(svc *jobs.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if svc.GitHubRunner == nil || !svc.GitHubRunner.Configured() {
			http.Error(w, "github runner is not configured", http.StatusServiceUnavailable)
			return
		}
		identity, err := svc.GitHubRunner.AuthenticateCheckout(
			r.Context(),
			r.Header.Get("X-Forge-Metal-Execution-Id"),
			r.Header.Get("X-Forge-Metal-Attempt-Id"),
			r.Header.Get("Authorization"),
		)
		if err != nil {
			writePublicWebhookError(w, http.StatusUnauthorized, err)
			return
		}
		var req githubCheckoutBundleRequest
		body := http.MaxBytesReader(w, r.Body, 16<<10)
		if err := json.NewDecoder(body).Decode(&req); err != nil {
			writeCheckoutError(w, jobs.ErrCheckoutInvalid)
			return
		}
		bundle, err := svc.GitHubRunner.PrepareCheckoutBundle(r.Context(), identity, jobs.CheckoutBundleRequest{
			Repository:  req.Repository,
			Ref:         req.Ref,
			SHA:         req.SHA,
			GitHubToken: req.GitHubToken,
		})
		if err != nil {
			writeCheckoutError(w, err)
			return
		}
		file, err := os.Open(bundle.BundlePath)
		if err != nil {
			writeCheckoutError(w, err)
			return
		}
		defer file.Close()
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Forge-Metal-Checkout-Cache-Hit", strconv.FormatBool(bundle.CacheHit))
		w.Header().Set("X-Forge-Metal-Checkout-Size-Bytes", strconv.FormatInt(bundle.SizeBytes, 10))
		w.Header().Set("X-Forge-Metal-Checkout-Sha", bundle.SHA)
		_, _ = io.Copy(w, file)
	}
}

func authenticateStickyDiskRequest(w http.ResponseWriter, r *http.Request, svc *jobs.Service) (jobs.StickyDiskIdentity, bool) {
	identity, err := svc.GitHubRunner.AuthenticateStickyDisk(
		r.Context(),
		r.Header.Get("X-Forge-Metal-Execution-Id"),
		r.Header.Get("X-Forge-Metal-Attempt-Id"),
		r.Header.Get("Authorization"),
	)
	if err != nil {
		writePublicWebhookError(w, http.StatusUnauthorized, err)
		return jobs.StickyDiskIdentity{}, false
	}
	return identity, true
}

func writeStickyDiskError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	if errors.Is(err, jobs.ErrStickyDiskInvalid) {
		status = http.StatusBadRequest
	}
	if errors.Is(err, jobs.ErrStickyDiskUnauthorized) {
		status = http.StatusUnauthorized
	}
	writePublicWebhookError(w, status, err)
}

func writeCheckoutError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	if errors.Is(err, jobs.ErrCheckoutInvalid) {
		status = http.StatusBadRequest
	}
	if errors.Is(err, jobs.ErrCheckoutUnauthorized) {
		status = http.StatusUnauthorized
	}
	writePublicWebhookError(w, status, err)
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
		rawInstallationID := strings.TrimSpace(query.Get("installation_id"))
		installationID, err := strconv.ParseInt(rawInstallationID, 10, 64)
		if err != nil || installationID <= 0 || state == "" {
			http.Error(w, "invalid github installation callback", http.StatusBadRequest)
			return
		}
		record, err := svc.GitHubRunner.CompleteInstallation(r.Context(), state, "", installationID)
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
