package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"go.opentelemetry.io/otel/trace"

	"github.com/verself/iam-service/internal/identity"
	"github.com/verself/service-runtime/requestmeta"
)

type inviteAcceptService interface {
	CompleteMemberInvite(ctx context.Context, input identity.CompleteMemberInviteRequest) error
}

type inviteAcceptRequest struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}

type inviteAcceptResponse struct {
	OK       bool   `json:"ok"`
	LoginURL string `json:"login_url"`
}

func RegisterInviteAcceptanceRoutes(mux *http.ServeMux, svc inviteAcceptService) {
	mux.HandleFunc("/api/v1/auth/invites/accept", handleInviteAcceptance(svc))
}

func handleInviteAcceptance(svc inviteAcceptService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := requestmeta.WithAttribution(r.Context(), requestmeta.FromHTTP(r))
		r = r.WithContext(ctx)
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			writeInviteProblem(ctx, w, http.StatusMethodNotAllowed, "method-not-allowed", "method not allowed")
			return
		}
		if svc == nil {
			writeInviteProblem(ctx, w, http.StatusInternalServerError, "invite-service-unavailable", "invite acceptance is unavailable")
			return
		}
		attribution := requestmeta.FromHTTP(r)
		if decision := apiOperationRateLimiter.Allow(rateLimitSignup, "invite_accept:"+attribution.ClientIP, time.Now()); !decision.Allowed {
			if decision.RetryAfter > 0 {
				w.Header().Set("Retry-After", strconv.FormatInt(int64(decision.RetryAfter.Seconds()), 10))
			}
			writeInviteProblem(ctx, w, http.StatusTooManyRequests, "rate-limit-exceeded", "rate limit exceeded")
			return
		}
		var input inviteAcceptRequest
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil {
			writeInviteProblem(ctx, w, http.StatusBadRequest, "invalid-request", "invite request is invalid")
			return
		}
		err := svc.CompleteMemberInvite(ctx, identity.CompleteMemberInviteRequest{
			AcceptanceToken: input.Token,
			Password:        input.Password,
		})
		if err != nil {
			writeInviteAcceptanceError(ctx, w, err)
			return
		}
		writeInviteJSON(w, http.StatusOK, inviteAcceptResponse{OK: true, LoginURL: "/login"})
	}
}

func writeInviteAcceptanceError(ctx context.Context, w http.ResponseWriter, err error) {
	switch {
	case identity.IsInvalid(err), errors.Is(err, identity.ErrMemberMissing):
		writeInviteProblem(ctx, w, http.StatusBadRequest, "invalid-invite", "invite could not be completed")
	case errors.Is(err, identity.ErrZitadelUnavailable):
		writeInviteProblem(ctx, w, http.StatusBadGateway, "zitadel-unavailable", "identity provider unavailable")
	case errors.Is(err, identity.ErrStoreUnavailable):
		writeInviteProblem(ctx, w, http.StatusInternalServerError, "iam-store-unavailable", "iam store unavailable")
	default:
		writeInviteProblem(ctx, w, http.StatusInternalServerError, "invite-acceptance-failed", "invite could not be completed")
	}
	trace.SpanFromContext(ctx).RecordError(err)
}

func writeInviteProblem(ctx context.Context, w http.ResponseWriter, status int, code string, detail string) {
	instance := ""
	if spanContext := trace.SpanContextFromContext(ctx); spanContext.HasTraceID() {
		instance = "urn:verself:trace:" + spanContext.TraceID().String()
	}
	writeInviteJSON(w, status, map[string]any{
		"type":     problemTypePrefix + code,
		"title":    http.StatusText(status),
		"status":   status,
		"detail":   detail,
		"instance": instance,
	})
}

func writeInviteJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
