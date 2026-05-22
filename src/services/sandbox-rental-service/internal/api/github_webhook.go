package api

import (
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/verself/observability/bazeltelemetry"
	"github.com/verself/sandbox-rental-service/internal/jobs"
)

const (
	runnerBootstrapConfigPath  = "/internal/sandbox/v1/runner-bootstrap"
	runnerBootstrapTokenHeader = "X-Verself-Runner-Bootstrap"
	githubCheckoutBundlePath   = "/internal/sandbox/v1/github-checkout/bundle"
	githubBazelTelemetryPath   = "/internal/sandbox/v1/bazel-telemetry/invocations"
	bazelTelemetryBodyLimit    = 64 << 20
)

type githubCheckoutBundleRequest struct {
	Repository  string `json:"repository"`
	Ref         string `json:"ref"`
	SHA         string `json:"sha"`
	GitHubToken string `json:"github_token"`
}

func RegisterPublicRoutes(mux *http.ServeMux, svc *jobs.Service, installationID string) {
	if mux == nil || svc == nil {
		return
	}
	mux.HandleFunc(runnerBootstrapConfigPath, runnerBootstrapConfigHandler(svc))
	mux.HandleFunc(githubCheckoutBundlePath, githubCheckoutBundleHandler(svc))
	mux.HandleFunc(githubBazelTelemetryPath, githubBazelTelemetryHandler(svc))
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
		config, err := svc.ConsumeRunnerBootstrapConfig(r.Context(), token, "")
		if err != nil {
			writePublicWebhookError(w, http.StatusNotFound, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte(config))
	}
}

func githubCheckoutBundleHandler(svc *jobs.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		identity, err := svc.AuthenticateCheckout(
			r.Context(),
			r.Header.Get("X-Verself-Execution-Id"),
			r.Header.Get("X-Verself-Attempt-Id"),
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
		bundle, err := svc.PrepareCheckoutBundle(r.Context(), identity, jobs.CheckoutBundleRequest{
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
		defer func() { _ = file.Close() }()
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Verself-Checkout-Bundle-Cache-Hit", strconv.FormatBool(bundle.BundleCacheHit))
		w.Header().Set("X-Verself-Checkout-Size-Bytes", strconv.FormatInt(bundle.SizeBytes, 10))
		w.Header().Set("X-Verself-Checkout-Sha", bundle.SHA)
		_, _ = io.Copy(w, file)
	}
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

func githubBazelTelemetryHandler(svc *jobs.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		identity, err := svc.AuthenticateBazelTelemetry(
			r.Context(),
			r.Header.Get("X-Verself-Execution-Id"),
			r.Header.Get("X-Verself-Attempt-Id"),
			r.Header.Get("Authorization"),
		)
		if err != nil {
			writePublicWebhookError(w, http.StatusUnauthorized, err)
			return
		}
		body := http.MaxBytesReader(w, r.Body, bazelTelemetryBodyLimit)
		var reader io.Reader = body
		if strings.EqualFold(strings.TrimSpace(r.Header.Get("Content-Encoding")), "gzip") {
			gz, err := gzip.NewReader(body)
			if err != nil {
				writeBazelTelemetryError(w, jobs.ErrBazelTelemetryInvalid)
				return
			}
			defer func() { _ = gz.Close() }()
			reader = gz
		}
		var upload bazeltelemetry.Upload
		if err := json.NewDecoder(reader).Decode(&upload); err != nil {
			writeBazelTelemetryError(w, jobs.ErrBazelTelemetryInvalid)
			return
		}
		if err := svc.RecordBazelTelemetry(r.Context(), identity, upload); err != nil {
			writeBazelTelemetryError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status":        "recorded",
			"invocation_id": upload.Invocation.InvocationID,
		})
	}
}

func writeBazelTelemetryError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, jobs.ErrBazelTelemetryInvalid):
		status = http.StatusBadRequest
	case errors.Is(err, jobs.ErrBazelTelemetryUnauthorized):
		status = http.StatusUnauthorized
	case errors.Is(err, jobs.ErrBazelTelemetryUnavailable):
		status = http.StatusServiceUnavailable
	}
	writePublicWebhookError(w, status, err)
}

func writePublicWebhookError(w http.ResponseWriter, status int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error": strings.TrimSpace(err.Error()),
	})
}
