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
	"net/http"
	"strings"

	"github.com/forge-metal/sandbox-rental-service/internal/jobs"
)

const forgejoWebhookPath = "/webhooks/forgejo"

type ForgejoWebhookConfig struct {
	PlatformOrgID uint64
	ActorID       string
	Secret        string
}

type forgejoWebhookRepository struct {
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

type forgejoWebhookResponse struct {
	Status               string `json:"status"`
	Event                string `json:"event"`
	DeliveryID           string `json:"delivery_id"`
	RepoID               string `json:"repo_id,omitempty"`
	ExecutionID          string `json:"execution_id,omitempty"`
	AttemptID            string `json:"attempt_id,omitempty"`
	BootstrapExecutionID string `json:"bootstrap_execution_id,omitempty"`
	BootstrapAttemptID   string `json:"bootstrap_attempt_id,omitempty"`
	Message              string `json:"message,omitempty"`
}

func RegisterPublicRoutes(mux *http.ServeMux, svc *jobs.Service, cfg ForgejoWebhookConfig) {
	if mux == nil || svc == nil {
		return
	}
	if cfg.PlatformOrgID == 0 || strings.TrimSpace(cfg.Secret) == "" {
		return
	}
	mux.HandleFunc(forgejoWebhookPath, forgejoWebhookHandler(svc, cfg))
}

func forgejoWebhookHandler(svc *jobs.Service, cfg ForgejoWebhookConfig) http.HandlerFunc {
	actorID := strings.TrimSpace(cfg.ActorID)
	if actorID == "" {
		actorID = "system:forgejo-webhook"
	}

	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
		if err != nil {
			var maxBytesErr *http.MaxBytesError
			if errors.As(err, &maxBytesErr) {
				http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
				return
			}
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if !validForgejoSignature(cfg.Secret, body, r.Header.Get("X-Forgejo-Signature")) {
			http.Error(w, "invalid forgejo signature", http.StatusUnauthorized)
			return
		}

		eventType := strings.TrimSpace(r.Header.Get("X-Forgejo-Event"))
		deliveryID := strings.TrimSpace(r.Header.Get("X-Forgejo-Delivery"))
		if eventType == "" || deliveryID == "" {
			http.Error(w, "missing forgejo event headers", http.StatusBadRequest)
			return
		}

		ctx := jobs.WithCorrelationID(r.Context(), deliveryID)
		response, status, err := processForgejoWebhook(ctx, svc, cfg.PlatformOrgID, actorID, eventType, deliveryID, body)
		if err != nil {
			http.Error(w, err.Error(), status)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(response)
	}
}

func processForgejoWebhook(
	ctx context.Context,
	svc *jobs.Service,
	orgID uint64,
	actorID string,
	eventType string,
	deliveryID string,
	body []byte,
) (forgejoWebhookResponse, int, error) {
	switch eventType {
	case "push":
		var payload forgejoPushWebhook
		if err := json.Unmarshal(body, &payload); err != nil {
			return forgejoWebhookResponse{}, http.StatusBadRequest, fmt.Errorf("decode push payload: %w", err)
		}
		response, err := handleForgejoPush(ctx, svc, orgID, actorID, deliveryID, payload)
		if err != nil {
			return forgejoWebhookResponse{}, http.StatusInternalServerError, err
		}
		return response, http.StatusAccepted, nil
	case "pull_request":
		var payload forgejoPullRequestWebhook
		if err := json.Unmarshal(body, &payload); err != nil {
			return forgejoWebhookResponse{}, http.StatusBadRequest, fmt.Errorf("decode pull_request payload: %w", err)
		}
		response, err := handleForgejoPullRequest(ctx, svc, orgID, actorID, deliveryID, payload)
		if err != nil {
			return forgejoWebhookResponse{}, http.StatusInternalServerError, err
		}
		return response, http.StatusAccepted, nil
	default:
		return forgejoWebhookResponse{
			Status:     "ignored",
			Event:      eventType,
			DeliveryID: deliveryID,
			Message:    "unsupported forgejo event",
		}, http.StatusOK, nil
	}
}

func handleForgejoPush(
	ctx context.Context,
	svc *jobs.Service,
	orgID uint64,
	actorID string,
	deliveryID string,
	payload forgejoPushWebhook,
) (forgejoWebhookResponse, error) {
	repo, err := upsertForgejoRepo(ctx, svc, orgID, payload.Repository)
	if err != nil {
		return forgejoWebhookResponse{}, err
	}

	response := forgejoWebhookResponse{
		Status:     "accepted",
		Event:      "push",
		DeliveryID: deliveryID,
		RepoID:     repo.RepoID.String(),
	}

	defaultBranchRef := "refs/heads/" + repo.DefaultBranch
	if strings.TrimSpace(payload.Ref) == defaultBranchRef {
		refreshedRepo := repo
		if refreshed, rescanErr := svc.RescanRepo(ctx, orgID, repo.RepoID); rescanErr != nil {
			return forgejoWebhookResponse{}, fmt.Errorf("rescan repo for default branch push: %w", rescanErr)
		} else {
			refreshedRepo = refreshed
		}
		if refreshedRepo.LastReadySHA != strings.TrimSpace(payload.After) {
			bootstrap, bootstrapErr := svc.QueueRepoBootstrap(ctx, orgID, actorID, refreshedRepo.RepoID, jobs.GenerationTriggerDefaultBranchPush)
			if bootstrapErr != nil && !errors.Is(bootstrapErr, jobs.ErrRepoNotReady) {
				return forgejoWebhookResponse{}, fmt.Errorf("queue repo bootstrap: %w", bootstrapErr)
			}
			if bootstrap != nil {
				response.BootstrapExecutionID = bootstrap.ExecutionID.String()
				response.BootstrapAttemptID = bootstrap.AttemptID.String()
			}
			repo = refreshedRepo
		}
	}

	if repo.State != jobs.RepoStateReady && repo.State != jobs.RepoStateDegraded {
		response.Status = "repo_not_ready"
		response.Message = "repo is not ready for forgejo runner execution"
		return response, nil
	}

	executionID, attemptID, err := svc.Submit(ctx, orgID, actorID, jobs.SubmitRequest{
		Kind:            jobs.KindForgejoRunner,
		ProductID:       "sandbox",
		Provider:        "forgejo",
		IdempotencyKey:  forgejoWebhookIdempotencyKey("push", deliveryID),
		RepoID:          repo.RepoID.String(),
		Ref:             strings.TrimSpace(payload.Ref),
		ProviderRunID:   deliveryID,
		ProviderJobID:   strings.TrimPrefix(strings.TrimSpace(payload.Ref), "refs/"),
		WorkflowJobName: "push",
	})
	if err != nil {
		if errors.Is(err, jobs.ErrRepoNotReady) {
			response.Status = "repo_not_ready"
			response.Message = "repo is not ready for forgejo runner execution"
			return response, nil
		}
		return forgejoWebhookResponse{}, fmt.Errorf("submit forgejo runner execution: %w", err)
	}
	response.ExecutionID = executionID.String()
	response.AttemptID = attemptID.String()
	return response, nil
}

func handleForgejoPullRequest(
	ctx context.Context,
	svc *jobs.Service,
	orgID uint64,
	actorID string,
	deliveryID string,
	payload forgejoPullRequestWebhook,
) (forgejoWebhookResponse, error) {
	repo, err := upsertForgejoRepo(ctx, svc, orgID, payload.Repository)
	if err != nil {
		return forgejoWebhookResponse{}, err
	}

	response := forgejoWebhookResponse{
		Status:     "accepted",
		Event:      "pull_request",
		DeliveryID: deliveryID,
		RepoID:     repo.RepoID.String(),
	}
	if repo.State != jobs.RepoStateReady && repo.State != jobs.RepoStateDegraded {
		response.Status = "repo_not_ready"
		response.Message = "repo is not ready for forgejo runner execution"
		return response, nil
	}

	ref := fmt.Sprintf("refs/pull/%d/head", payload.Number)
	if payload.Number <= 0 && strings.TrimSpace(payload.PullRequest.Head.Ref) != "" {
		ref = strings.TrimSpace(payload.PullRequest.Head.Ref)
	}
	executionID, attemptID, err := svc.Submit(ctx, orgID, actorID, jobs.SubmitRequest{
		Kind:            jobs.KindForgejoRunner,
		ProductID:       "sandbox",
		Provider:        "forgejo",
		IdempotencyKey:  forgejoWebhookIdempotencyKey("pull_request", deliveryID),
		RepoID:          repo.RepoID.String(),
		Ref:             ref,
		ProviderRunID:   deliveryID,
		ProviderJobID:   fmt.Sprintf("pr-%d", payload.Number),
		WorkflowJobName: "pull_request",
	})
	if err != nil {
		if errors.Is(err, jobs.ErrRepoNotReady) {
			response.Status = "repo_not_ready"
			response.Message = "repo is not ready for forgejo runner execution"
			return response, nil
		}
		return forgejoWebhookResponse{}, fmt.Errorf("submit forgejo runner execution: %w", err)
	}
	response.ExecutionID = executionID.String()
	response.AttemptID = attemptID.String()
	return response, nil
}

func upsertForgejoRepo(ctx context.Context, svc *jobs.Service, orgID uint64, repository forgejoWebhookRepository) (*jobs.RepoRecord, error) {
	importReq := jobs.ImportRepoRequest{
		Provider:       "forgejo",
		ProviderRepoID: strings.TrimSpace(repository.FullName),
		Owner:          strings.TrimSpace(repository.Owner.Login),
		Name:           strings.TrimSpace(repository.Name),
		FullName:       strings.TrimSpace(repository.FullName),
		CloneURL:       strings.TrimSpace(repository.CloneURL),
		DefaultBranch:  strings.TrimSpace(repository.DefaultBranch),
	}
	if repo, ok, err := svc.FindRepoByExternalKey(ctx, orgID, importReq.Provider, importReq.ProviderRepoID, importReq.FullName); err != nil {
		return nil, fmt.Errorf("find repo by forgejo key: %w", err)
	} else if ok {
		if err := svc.UpdateRepoImportMetadata(ctx, repo.RepoID, importReq); err != nil {
			return nil, fmt.Errorf("update repo import metadata: %w", err)
		}
		return svc.GetRepo(ctx, orgID, repo.RepoID)
	}
	return svc.ImportRepo(ctx, orgID, importReq)
}

func validForgejoSignature(secret string, body []byte, provided string) bool {
	provided = strings.TrimSpace(strings.ToLower(provided))
	if secret == "" || provided == "" {
		return false
	}

	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	expected := make([]byte, hex.EncodedLen(mac.Size()))
	hex.Encode(expected, mac.Sum(nil))
	return hmac.Equal([]byte(provided), expected)
}

func forgejoWebhookIdempotencyKey(eventType, deliveryID string) string {
	return "forgejo-webhook:" + strings.TrimSpace(eventType) + ":" + strings.TrimSpace(deliveryID)
}
