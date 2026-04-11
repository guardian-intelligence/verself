package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"

	"github.com/forge-metal/sandbox-rental-service/internal/jobs"
)

const (
	webhookIngestPathPrefix       = "/webhooks/ingest/"
	webhookIngestBodyLimit        = 1 << 20
	webhookDeliveryWorkerInterval = 500 * time.Millisecond
	webhookDeliveryWorkerBatch    = 25
	webhookActorID                = "system:webhook-ingest"
)

type forgejoWebhookRepository struct {
	ID            int64  `json:"id"`
	FullName      string `json:"full_name"`
	CloneURL      string `json:"clone_url"`
	DefaultBranch string `json:"default_branch"`
	Name          string `json:"name"`
	Owner         struct {
		Login string `json:"login"`
	} `json:"owner"`
}

type forgejoPushWebhook struct {
	Ref        string                   `json:"ref"`
	After      string                   `json:"after"`
	Repository forgejoWebhookRepository `json:"repository"`
}

type forgejoPullRequestWebhook struct {
	Action      string                   `json:"action"`
	Number      int                      `json:"number"`
	Repository  forgejoWebhookRepository `json:"repository"`
	PullRequest struct {
		Head struct {
			Ref string `json:"ref"`
			Sha string `json:"sha"`
		} `json:"head"`
		Base struct {
			Ref string `json:"ref"`
		} `json:"base"`
	} `json:"pull_request"`
}

type webhookIngestResponse struct {
	Status             string `json:"status"`
	EndpointID         string `json:"endpoint_id"`
	DeliveryID         string `json:"delivery_id"`
	ProviderDeliveryID string `json:"provider_delivery_id"`
	Event              string `json:"event"`
	Duplicate          bool   `json:"duplicate"`
}

type forgejoWebhookResponse struct {
	Status     string `json:"status"`
	Event      string `json:"event"`
	DeliveryID string `json:"delivery_id"`
	RepoID     string `json:"repo_id,omitempty"`
	Message    string `json:"message,omitempty"`
}

type webhookDeliveryOutcome struct {
	Ignored bool
	Reason  string
}

func RegisterPublicRoutes(mux *http.ServeMux, svc *jobs.Service) {
	if mux == nil || svc == nil {
		return
	}
	mux.HandleFunc(webhookIngestPathPrefix, webhookIngestHandler(svc))
}

func webhookIngestHandler(svc *jobs.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		endpointID, err := webhookEndpointIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		endpoint, err := svc.LookupWebhookEndpointForIngest(r.Context(), endpointID)
		if err != nil {
			if errors.Is(err, jobs.ErrWebhookEndpointMissing) {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			writeWebhookError(w, r.Context(), http.StatusInternalServerError, err)
			return
		}

		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, webhookIngestBodyLimit))
		if err != nil {
			var maxBytesErr *http.MaxBytesError
			if errors.As(err, &maxBytesErr) {
				http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
				return
			}
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		deliveryID, eventType, status, err := validateWebhookDelivery(endpoint, body, r.Header)
		if err != nil {
			writeWebhookError(w, r.Context(), status, err)
			return
		}

		ctx := jobs.WithCorrelationID(r.Context(), deliveryID)
		delivery, inserted, err := svc.RecordWebhookDelivery(ctx, jobs.RecordWebhookDeliveryRequest{
			EndpointID:         endpoint.EndpointID,
			IntegrationID:      endpoint.IntegrationID,
			OrgID:              endpoint.OrgID,
			Provider:           endpoint.Provider,
			ProviderHost:       endpoint.ProviderHost,
			ProviderDeliveryID: deliveryID,
			EventType:          eventType,
			Payload:            json.RawMessage(body),
			TraceID:            traceIDFromContext(ctx),
		})
		if err != nil {
			writeWebhookError(w, ctx, http.StatusInternalServerError, err)
			return
		}

		statusText := "queued"
		if !inserted {
			statusText = "duplicate"
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(webhookIngestResponse{
			Status:             statusText,
			EndpointID:         endpoint.EndpointID.String(),
			DeliveryID:         delivery.DeliveryID.String(),
			ProviderDeliveryID: delivery.ProviderDeliveryID,
			Event:              delivery.EventType,
			Duplicate:          !inserted,
		})
	}
}

func webhookEndpointIDFromPath(path string) (uuid.UUID, error) {
	if !strings.HasPrefix(path, webhookIngestPathPrefix) {
		return uuid.Nil, fmt.Errorf("invalid webhook path")
	}
	raw := strings.TrimSpace(strings.TrimPrefix(path, webhookIngestPathPrefix))
	if raw == "" || strings.Contains(raw, "/") {
		return uuid.Nil, fmt.Errorf("invalid endpoint id")
	}
	endpointID, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, err
	}
	return endpointID, nil
}

func validateWebhookDelivery(endpoint *jobs.WebhookEndpointForIngest, body []byte, header http.Header) (string, string, int, error) {
	if endpoint == nil {
		return "", "", http.StatusNotFound, jobs.ErrWebhookEndpointMissing
	}
	switch endpoint.Provider {
	case jobs.WebhookProviderForgejo:
		signature := firstNonEmpty(header.Get("X-Forgejo-Signature"), header.Get("X-Gitea-Signature"))
		if !validWebhookSignature(endpoint.Secrets, body, signature) {
			return "", "", http.StatusUnauthorized, fmt.Errorf("invalid forgejo signature")
		}
		eventType := strings.TrimSpace(header.Get("X-Forgejo-Event"))
		deliveryID := strings.TrimSpace(header.Get("X-Forgejo-Delivery"))
		if eventType == "" || deliveryID == "" {
			return "", "", http.StatusBadRequest, fmt.Errorf("missing forgejo event headers")
		}
		if !json.Valid(body) {
			return "", "", http.StatusBadRequest, fmt.Errorf("webhook payload must be valid JSON")
		}
		return deliveryID, eventType, http.StatusAccepted, nil
	default:
		return "", "", http.StatusNotImplemented, jobs.ErrWebhookProviderUnsupported
	}
}

func writeWebhookError(w http.ResponseWriter, ctx context.Context, status int, err error) {
	trace.SpanFromContext(ctx).RecordError(err)
	message := "webhook ingest failed"
	if status < http.StatusInternalServerError {
		message = http.StatusText(status)
	}
	http.Error(w, message, status)
}

func StartWebhookDeliveryWorker(ctx context.Context, svc *jobs.Service) {
	if svc == nil {
		return
	}
	logger := webhookLogger(svc)
	go func() {
		ticker := time.NewTicker(webhookDeliveryWorkerInterval)
		defer ticker.Stop()
		for {
			if _, err := processWebhookDeliveryBatch(ctx, svc, webhookDeliveryWorkerBatch); err != nil {
				logger.ErrorContext(ctx, "webhook delivery worker batch failed", "error", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func ProcessWebhookDeliveries(ctx context.Context, svc *jobs.Service, limit int) (int, error) {
	return processWebhookDeliveryBatch(ctx, svc, limit)
}

func processWebhookDeliveryBatch(ctx context.Context, svc *jobs.Service, limit int) (int, error) {
	if svc == nil {
		return 0, nil
	}
	if limit <= 0 {
		limit = 1
	}
	var (
		processed int
		firstErr  error
	)
	for processed < limit {
		delivery, ok, err := svc.ClaimNextWebhookDelivery(ctx)
		if err != nil {
			return processed, err
		}
		if !ok {
			return processed, firstErr
		}
		processed++
		deliveryCtx := jobs.WithCorrelationID(ctx, firstNonEmpty(delivery.ProviderDeliveryID, delivery.DeliveryID.String()))
		outcome, err := processWebhookDelivery(deliveryCtx, svc, *delivery)
		switch {
		case err != nil:
			if markErr := svc.MarkWebhookDeliveryFailed(deliveryCtx, delivery.DeliveryID, err); markErr != nil && firstErr == nil {
				firstErr = markErr
			}
			if firstErr == nil {
				firstErr = err
			}
		case outcome.Ignored:
			if err := svc.MarkWebhookDeliveryIgnored(deliveryCtx, delivery.DeliveryID, outcome.Reason); err != nil && firstErr == nil {
				firstErr = err
			}
		default:
			if err := svc.MarkWebhookDeliveryProcessed(deliveryCtx, delivery.DeliveryID); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	return processed, firstErr
}

func processWebhookDelivery(ctx context.Context, svc *jobs.Service, delivery jobs.WebhookDeliveryRecord) (webhookDeliveryOutcome, error) {
	switch delivery.Provider {
	case jobs.WebhookProviderForgejo:
		return processForgejoWebhook(ctx, svc, delivery)
	default:
		return webhookDeliveryOutcome{Ignored: true, Reason: "unsupported provider"}, nil
	}
}

func processForgejoWebhook(ctx context.Context, svc *jobs.Service, delivery jobs.WebhookDeliveryRecord) (webhookDeliveryOutcome, error) {
	switch delivery.EventType {
	case "push":
		var payload forgejoPushWebhook
		if err := json.Unmarshal(delivery.Payload, &payload); err != nil {
			return webhookDeliveryOutcome{Ignored: true, Reason: "decode push payload: " + err.Error()}, nil
		}
		_, err := handleForgejoPush(ctx, svc, delivery, payload)
		if err != nil {
			return webhookDeliveryOutcome{}, err
		}
		return webhookDeliveryOutcome{}, nil
	case "pull_request":
		var payload forgejoPullRequestWebhook
		if err := json.Unmarshal(delivery.Payload, &payload); err != nil {
			return webhookDeliveryOutcome{Ignored: true, Reason: "decode pull_request payload: " + err.Error()}, nil
		}
		_, err := handleForgejoPullRequest(ctx, svc, delivery, payload)
		if err != nil {
			return webhookDeliveryOutcome{}, err
		}
		return webhookDeliveryOutcome{}, nil
	default:
		return webhookDeliveryOutcome{Ignored: true, Reason: "unsupported forgejo event"}, nil
	}
}

func handleForgejoPush(
	ctx context.Context,
	svc *jobs.Service,
	delivery jobs.WebhookDeliveryRecord,
	payload forgejoPushWebhook,
) (forgejoWebhookResponse, error) {
	repo, err := upsertForgejoRepo(ctx, svc, delivery, payload.Repository)
	if err != nil {
		return forgejoWebhookResponse{}, err
	}

	response := forgejoWebhookResponse{
		Status:     "accepted",
		Event:      "push",
		DeliveryID: delivery.ProviderDeliveryID,
		RepoID:     repo.RepoID.String(),
	}

	defaultBranchRef := "refs/heads/" + repo.DefaultBranch
	if strings.TrimSpace(payload.Ref) == defaultBranchRef {
		if refreshed, rescanErr := svc.RescanRepo(ctx, delivery.OrgID, repo.RepoID); rescanErr != nil {
			return forgejoWebhookResponse{}, fmt.Errorf("rescan repo for default branch push: %w", rescanErr)
		} else {
			repo = refreshed
		}
	}

	if repo.State != jobs.RepoStateReady {
		response.Status = "repo_not_ready"
		response.Message = "repo metadata updated; repo is not compatible"
		return response, nil
	}

	response.Message = "repo metadata updated; no execution policy is attached"
	return response, nil
}

func handleForgejoPullRequest(
	ctx context.Context,
	svc *jobs.Service,
	delivery jobs.WebhookDeliveryRecord,
	payload forgejoPullRequestWebhook,
) (forgejoWebhookResponse, error) {
	repo, err := upsertForgejoRepo(ctx, svc, delivery, payload.Repository)
	if err != nil {
		return forgejoWebhookResponse{}, err
	}

	response := forgejoWebhookResponse{
		Status:     "accepted",
		Event:      "pull_request",
		DeliveryID: delivery.ProviderDeliveryID,
		RepoID:     repo.RepoID.String(),
	}
	if repo.State != jobs.RepoStateReady {
		response.Status = "repo_not_ready"
		response.Message = "repo metadata updated; repo is not compatible"
		return response, nil
	}

	response.Message = "repo metadata updated; no execution policy is attached"
	return response, nil
}

func upsertForgejoRepo(ctx context.Context, svc *jobs.Service, delivery jobs.WebhookDeliveryRecord, repository forgejoWebhookRepository) (*jobs.RepoRecord, error) {
	importReq := jobs.ImportRepoRequest{
		IntegrationID:  &delivery.IntegrationID,
		Provider:       jobs.WebhookProviderForgejo,
		ProviderHost:   delivery.ProviderHost,
		ProviderRepoID: forgejoRepositoryProviderID(repository),
		Owner:          strings.TrimSpace(repository.Owner.Login),
		Name:           strings.TrimSpace(repository.Name),
		FullName:       strings.TrimSpace(repository.FullName),
		CloneURL:       strings.TrimSpace(repository.CloneURL),
		DefaultBranch:  strings.TrimSpace(repository.DefaultBranch),
	}
	if repo, ok, err := svc.FindRepoByExternalKey(ctx, delivery.OrgID, importReq.Provider, importReq.ProviderHost, importReq.ProviderRepoID, importReq.FullName); err != nil {
		return nil, fmt.Errorf("find repo by forgejo key: %w", err)
	} else if ok {
		if err := svc.UpdateRepoImportMetadata(ctx, repo.RepoID, importReq); err != nil {
			return nil, fmt.Errorf("update repo import metadata: %w", err)
		}
		return svc.GetRepo(ctx, delivery.OrgID, repo.RepoID)
	}
	return svc.ImportRepo(ctx, delivery.OrgID, importReq)
}

func forgejoRepositoryProviderID(repository forgejoWebhookRepository) string {
	if repository.ID > 0 {
		return fmt.Sprintf("%d", repository.ID)
	}
	return strings.TrimSpace(repository.FullName)
}

func validWebhookSignature(secrets []string, body []byte, provided string) bool {
	provided = strings.TrimSpace(strings.ToLower(provided))
	provided = strings.TrimPrefix(provided, "sha256=")
	if provided == "" {
		return false
	}
	if len(provided) != hex.EncodedLen(sha256.Size) {
		return false
	}
	if _, err := hex.DecodeString(provided); err != nil {
		return false
	}
	for _, secret := range secrets {
		if validWebhookSignatureForSecret(strings.TrimSpace(secret), body, provided) {
			return true
		}
	}
	return false
}

func validWebhookSignatureForSecret(secret string, body []byte, provided string) bool {
	if secret == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	expected := make([]byte, hex.EncodedLen(mac.Size()))
	hex.Encode(expected, mac.Sum(nil))
	return hmac.Equal([]byte(provided), expected)
}

func traceIDFromContext(ctx context.Context) string {
	spanContext := trace.SpanContextFromContext(ctx)
	if !spanContext.IsValid() {
		return ""
	}
	return spanContext.TraceID().String()
}

func webhookLogger(svc *jobs.Service) *slog.Logger {
	if svc != nil && svc.Logger != nil {
		return svc.Logger
	}
	return slog.Default()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
