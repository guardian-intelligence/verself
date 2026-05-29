package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/verself/iam-service/internal/identity"
	identitystore "github.com/verself/iam-service/internal/store"
)

const browserIDPCallbackPath = "/api/v1/auth/idp/callback"

type idpLoginStartRequest struct {
	RedirectTo string `json:"redirectTo"`
	Purpose    string `json:"purpose"`
}

type idpLoginStartResponse struct {
	RedirectURL string `json:"redirectURL"`
}

// handleGithubLoginStart begins a "Sign in with GitHub" flow. It starts a
// Zitadel external-IdP intent, persists a single-use pending row keyed by the
// idp-login state cookie, and returns the GitHub authorization URL for the SPA
// to navigate to. The OIDC auth request and full login transaction are created
// later, on the callback, mirroring the password-login self-initiated branch.
func (a *BrowserAuth) handleGithubLoginStart(w http.ResponseWriter, r *http.Request) {
	if !a.sameOrigin(r) {
		a.writeProblem(w, r, http.StatusForbidden, problemAuthOriginNotAllowed, "origin not allowed")
		return
	}
	githubIDPID := a.githubLoginIDP()
	if githubIDPID == "" {
		a.writeProblem(w, r, http.StatusServiceUnavailable, problemServiceUnavailable, "GitHub login is not configured")
		return
	}
	var input idpLoginStartRequest
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil && !errors.Is(err, io.EOF) {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			a.writeProblem(w, r, http.StatusRequestEntityTooLarge, problemRequestBodyTooLarge, "request body is too large")
			return
		}
		a.writeProblem(w, r, http.StatusBadRequest, problemRequestValidationFailed, "invalid login payload")
		return
	}
	client, clientSecret, err := a.ensureBrowserClient(w, r)
	if err != nil {
		a.serverError(w, r, "ensure browser client", err)
		return
	}
	state, err := randomToken(32)
	if err != nil {
		a.serverError(w, r, "generate idp login state", err)
		return
	}
	stateHash := hashToken(state)
	successURL := a.publicBaseURL.ResolveReference(&url.URL{Path: browserIDPCallbackPath}).String()
	failureURL := a.publicBaseURL.ResolveReference(&url.URL{Path: "/login", RawQuery: "error=github_login_failed"}).String()
	start, err := a.providerLogin.StartIDPIntent(r.Context(), githubIDPID, successURL, failureURL)
	if err != nil {
		a.serverError(w, r, "start github idp intent", err)
		return
	}
	if err := a.q.InsertBrowserIDPLoginIntent(r.Context(), identitystore.InsertBrowserIDPLoginIntentParams{
		StateHash:  stateHash,
		ClientHash: client.ClientHash,
		RedirectTo: a.sanitizeRedirectTarget(input.RedirectTo),
		Purpose:    browserLoginPurpose(input.Purpose),
		ExpiresAt:  timestamptz(time.Now().UTC().Add(browserAuthLoginTTL)),
	}); err != nil {
		a.serverError(w, r, "persist idp login intent", err)
		return
	}
	a.setLoginCookie(w, stateHash)
	a.setClientCookie(w, clientSecret)
	trace.SpanFromContext(r.Context()).AddEvent("iam.browser_idp_login.started", trace.WithAttributes(
		attribute.String("auth.idp_provider", "github"),
		attribute.String("auth.client_handle", browserClientHandle(client.ClientHash)),
	))
	a.writeJSON(w, http.StatusOK, idpLoginStartResponse{RedirectURL: start.AuthURL})
}

// handleGithubLoginCallback resumes a "Sign in with GitHub" flow. Zitadel
// redirects here with the idp intent `id` and `token` after the user returns
// from GitHub through Zitadel's /idps/callback. We resolve the external
// identity, mint a Zitadel session from the intent, then mirror the
// password-login self-initiated branch: start an OIDC auth request, persist the
// login transaction, finalize, and redirect to the OIDC RP callback that builds
// the browser account.
func (a *BrowserAuth) handleGithubLoginCallback(w http.ResponseWriter, r *http.Request) {
	if a.githubLoginIDP() == "" {
		a.clearLoginCookie(w)
		a.redirectLoginError(w, r, "github_login_failed")
		return
	}
	query := r.URL.Query()
	intentID := strings.TrimSpace(query.Get("id"))
	intentToken := strings.TrimSpace(query.Get("token"))
	if strings.TrimSpace(query.Get("error")) != "" || intentID == "" || intentToken == "" {
		a.clearLoginCookie(w)
		a.redirectLoginError(w, r, "github_login_failed")
		return
	}
	loginStateHash, ok := loginStateHashFromRequest(r)
	if !ok {
		a.clearLoginCookie(w)
		a.redirectLoginError(w, r, "github_login_failed")
		return
	}
	pending, err := a.q.DeleteBrowserIDPLoginIntent(r.Context(), identitystore.DeleteBrowserIDPLoginIntentParams{StateHash: loginStateHash})
	if errors.Is(err, pgx.ErrNoRows) {
		a.clearLoginCookie(w)
		a.redirectLoginError(w, r, "github_login_expired")
		return
	}
	if err != nil {
		a.serverError(w, r, "load idp login intent", err)
		return
	}
	clientSecret, ok := browserClientSecretFromRequest(r)
	if !ok || subtle.ConstantTimeCompare([]byte(hashToken(clientSecret)), []byte(pending.ClientHash)) != 1 {
		a.clearLoginCookie(w)
		a.redirectLoginError(w, r, "github_login_failed")
		return
	}
	result, err := a.providerLogin.RetrieveIDPIntent(r.Context(), intentID, intentToken)
	if err != nil {
		a.serverError(w, r, "retrieve github idp intent", err)
		return
	}
	session, err := a.providerLogin.CreateSessionFromIDPIntent(r.Context(), intentID, intentToken, result.UserID, identity.LoginSessionInput{
		UserAgent: strings.TrimSpace(r.Header.Get("User-Agent")),
		IP:        requestmetaIP(r.Context()),
		Lifetime:  browserAuthSessionTTL,
	})
	if err != nil {
		a.serverError(w, r, "create github idp session", err)
		return
	}
	state, nonce, verifier, err := newOIDCLoginSecrets()
	if err != nil {
		a.discardProviderSession(r.Context(), session.SessionID)
		a.serverError(w, r, "generate oidc login state", err)
		return
	}
	authRequestID, err := a.startProviderOIDCAuthRequest(r.Context(), state, nonce, verifier, nil)
	if err != nil {
		a.discardProviderSession(r.Context(), session.SessionID)
		a.serverError(w, r, "start provider auth request", err)
		return
	}
	oidcStateHash := hashToken(state)
	providerSessionToken, err := a.tokenVault.seal(session.SessionToken, oidcStateHash, "provider_session")
	if err != nil {
		a.discardProviderSession(r.Context(), session.SessionID)
		a.serverError(w, r, "seal provider session token", err)
		return
	}
	if err := a.q.InsertBrowserLoginTransaction(r.Context(), identitystore.InsertBrowserLoginTransactionParams{
		StateHash:                      oidcStateHash,
		ClientHash:                     pending.ClientHash,
		Nonce:                          nonce,
		CodeVerifier:                   verifier,
		RedirectTo:                     pending.RedirectTo,
		Purpose:                        pending.Purpose,
		LoginHint:                      nullableText(result.Email),
		ProviderSessionID:              session.SessionID,
		ProviderSessionTokenCiphertext: providerSessionToken,
		ExpiresAt:                      timestamptz(time.Now().UTC().Add(browserAuthLoginTTL)),
	}); err != nil {
		a.discardProviderSession(r.Context(), session.SessionID)
		a.serverError(w, r, "persist login transaction", err)
		return
	}
	callbackURL, err := a.providerLogin.FinalizeOIDCAuthRequest(r.Context(), authRequestID, session)
	if err != nil {
		_, _ = a.q.DeleteBrowserLoginTransaction(context.WithoutCancel(r.Context()), identitystore.DeleteBrowserLoginTransactionParams{StateHash: oidcStateHash})
		a.discardProviderSession(r.Context(), session.SessionID)
		a.serverError(w, r, "finalize provider auth request", err)
		return
	}
	a.setLoginCookie(w, oidcStateHash)
	a.setClientCookie(w, clientSecret)
	trace.SpanFromContext(r.Context()).AddEvent("iam.browser_idp_login.completed", trace.WithAttributes(
		attribute.String("auth.idp_provider", "github"),
		attribute.String("auth.client_handle", browserClientHandle(pending.ClientHash)),
		attribute.String("auth.idp_external_subject_hash", hashToken(result.ExternalSubject)),
		attribute.Bool("auth.idp_linked", strings.TrimSpace(result.UserID) != ""),
	))
	http.Redirect(w, r, callbackURL, http.StatusSeeOther)
}

func (a *BrowserAuth) discardProviderSession(ctx context.Context, sessionID string) {
	_ = a.providerSessions.DeleteSession(context.WithoutCancel(ctx), sessionID)
}

func (a *BrowserAuth) redirectLoginError(w http.ResponseWriter, r *http.Request, code string) {
	target := a.publicBaseURL.ResolveReference(&url.URL{Path: "/login", RawQuery: "error=" + url.QueryEscape(code)}).String()
	http.Redirect(w, r, target, http.StatusSeeOther)
}
