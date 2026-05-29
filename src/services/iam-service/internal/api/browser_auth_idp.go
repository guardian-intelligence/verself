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
	// The headless Session API only authenticates an already-linked external
	// identity (the IdP-level autoLinking/autoCreation options apply to the hosted
	// login, not here). Resolve the Zitadel user before minting the session.
	userID := strings.TrimSpace(result.UserID)
	if userID == "" {
		if !result.EmailVerified || strings.TrimSpace(result.Email) == "" {
			a.clearLoginCookie(w)
			a.redirectLoginError(w, r, "github_email_unverified")
			return
		}
		existing, found, findErr := a.providerLogin.FindHumanByVerifiedEmail(r.Context(), result.Email)
		if findErr != nil {
			a.serverError(w, r, "find human by verified email", findErr)
			return
		}
		if found {
			// Step-up: the email match only selects the candidate account; the link
			// is authorized by proving control of it. Capture the external identity
			// and redirect to a password proof instead of linking here.
			a.beginIDPLinkStepUp(w, r, pending, result, existing, clientSecret)
			return
		}
		// Brand-new user: just-in-time provision a Zitadel org, an IdP-linked human
		// (the credential), and a starter Verself org, then mint the session for the
		// new user. The account graph is materialized by the OIDC RP callback.
		if a.accountProvisioner == nil {
			a.clearLoginCookie(w)
			a.redirectLoginError(w, r, "github_signup_unavailable")
			return
		}
		provisioned, provErr := a.accountProvisioner.ProvisionGithubSignup(r.Context(), identity.GithubSignupRequest{
			Email:            result.Email,
			DisplayName:      result.Username,
			IDPID:            firstNonEmpty(result.IDPID, a.githubLoginIDP()),
			ExternalUserID:   result.ExternalSubject,
			ExternalUserName: result.Username,
		})
		if provErr != nil {
			a.serverError(w, r, "provision github account", provErr)
			return
		}
		userID = provisioned.UserID
		trace.SpanFromContext(r.Context()).AddEvent("iam.browser_idp_login.provisioned", trace.WithAttributes(
			attribute.String("auth.idp_provider", "github"),
			attribute.String("auth.org_id", provisioned.OrgID),
		))
	}
	session, err := a.providerLogin.CreateSessionFromIDPIntent(r.Context(), intentID, intentToken, userID, identity.LoginSessionInput{
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

// beginIDPLinkStepUp captures a completed GitHub identity that matched an
// existing account by verified email and redirects the browser to a password
// proof. The external identity is stored single-use, bound to the originating
// browser client, and carried by the link cookie; the link is performed only
// after a successful password authentication to the matched account (see
// handlePasswordLogin). It abandons the idp-intent state because the session
// will come from the password proof, not the intent.
func (a *BrowserAuth) beginIDPLinkStepUp(w http.ResponseWriter, r *http.Request, pending identitystore.IamBrowserIdpLoginIntent, result identity.IDPIntentResult, userID, clientSecret string) {
	challenge, err := randomToken(32)
	if err != nil {
		a.serverError(w, r, "generate idp link challenge", err)
		return
	}
	if err := a.q.InsertBrowserIDPLinkChallenge(r.Context(), identitystore.InsertBrowserIDPLinkChallengeParams{
		ChallengeHash:    hashToken(challenge),
		ClientHash:       pending.ClientHash,
		ZitadelUserID:    userID,
		IdpID:            firstNonEmpty(result.IDPID, a.githubLoginIDP()),
		ExternalUserID:   result.ExternalSubject,
		ExternalUserName: firstNonEmpty(result.Username, result.ExternalSubject),
		Email:            result.Email,
		RedirectTo:       pending.RedirectTo,
		Purpose:          pending.Purpose,
		ExpiresAt:        timestamptz(time.Now().UTC().Add(browserAuthLoginTTL)),
	}); err != nil {
		a.serverError(w, r, "persist idp link challenge", err)
		return
	}
	a.clearLoginCookie(w)
	a.setLinkChallengeCookie(w, challenge)
	a.setClientCookie(w, clientSecret)
	trace.SpanFromContext(r.Context()).AddEvent("iam.browser_idp_link.challenge_issued", trace.WithAttributes(
		attribute.String("auth.idp_provider", "github"),
		attribute.String("auth.client_handle", browserClientHandle(pending.ClientHash)),
		attribute.String("auth.idp_external_subject_hash", hashToken(result.ExternalSubject)),
	))
	target := a.publicBaseURL.ResolveReference(&url.URL{
		Path:     "/login/email",
		RawQuery: url.Values{"link": {"github"}, "email": {result.Email}}.Encode(),
	}).String()
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// linkGithubFromChallenge completes a step-up GitHub link during a password
// login. It is a no-op unless a valid single-use link challenge cookie is present
// for the same browser client and the proven account matches the challenge email
// (the GitHub identity must bind only to the account whose verified email
// selected it). The challenge is consumed regardless of outcome. A failure to
// link is surfaced loudly by the caller rather than silently dropped, because
// linking is the explicit purpose of this flow.
func (a *BrowserAuth) linkGithubFromChallenge(r *http.Request, w http.ResponseWriter, loginEmail string) error {
	secret, ok := linkChallengeSecretFromRequest(r)
	if !ok {
		return nil
	}
	a.clearLinkChallengeCookie(w)
	challenge, err := a.q.DeleteBrowserIDPLinkChallenge(r.Context(), identitystore.DeleteBrowserIDPLinkChallengeParams{ChallengeHash: hashToken(secret)})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	clientSecret, ok := browserClientSecretFromRequest(r)
	if !ok || subtle.ConstantTimeCompare([]byte(hashToken(clientSecret)), []byte(challenge.ClientHash)) != 1 {
		return nil
	}
	if !sameEmailIdentity(challenge.Email, loginEmail) {
		return nil
	}
	if err := a.providerLogin.AddIDPLink(r.Context(), identity.AddIDPLinkInput{
		UserID:           challenge.ZitadelUserID,
		IDPID:            challenge.IdpID,
		ExternalUserID:   challenge.ExternalUserID,
		ExternalUserName: challenge.ExternalUserName,
	}); err != nil {
		return err
	}
	trace.SpanFromContext(r.Context()).AddEvent("iam.browser_idp_link.linked", trace.WithAttributes(
		attribute.String("auth.idp_provider", "github"),
		attribute.String("auth.client_handle", browserClientHandle(challenge.ClientHash)),
		attribute.String("auth.idp_external_subject_hash", hashToken(challenge.ExternalUserID)),
	))
	return nil
}

func (a *BrowserAuth) redirectLoginError(w http.ResponseWriter, r *http.Request, code string) {
	target := a.publicBaseURL.ResolveReference(&url.URL{Path: "/login", RawQuery: "error=" + url.QueryEscape(code)}).String()
	http.Redirect(w, r, target, http.StatusSeeOther)
}
