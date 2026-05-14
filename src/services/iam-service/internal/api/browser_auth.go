package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"golang.org/x/oauth2"

	"github.com/verself/iam-service/internal/authz"
	"github.com/verself/iam-service/internal/identity"
	identitystore "github.com/verself/iam-service/internal/store"
)

const (
	browserAuthCookieName      = "verself_session"
	browserAuthLoginCookieName = "verself_login"
	browserAuthSessionTTL      = 30 * 24 * time.Hour
	browserAuthLoginTTL        = 5 * time.Minute
	browserAuthRefreshLeeway   = 60 * time.Second
	browserAuthCallbackPath    = "/api/v1/auth/callback"
	browserAuthDefaultRedirect = "/"
	zitadelResourceOwnerID     = "urn:zitadel:iam:user:resourceowner:id"
	zitadelResourceOwnerName   = "urn:zitadel:iam:user:resourceowner:name"
	accountOrganizationRetries = 8
)

var browserAuthTracer = otel.Tracer("github.com/verself/iam-service/browser-auth")

type BrowserAuthConfig struct {
	PG              *pgxpool.Pool
	Logger          *slog.Logger
	IssuerURL       string
	ClientID        string
	ClientSecret    string
	PublicBaseURL   string
	ProductAudience string
	HTTPClient      *http.Client
	Authz           *authz.Service
}

type BrowserAuth struct {
	q                  *identitystore.Queries
	store              identity.SQLStore
	logger             *slog.Logger
	provider           *oidc.Provider
	verifier           *oidc.IDTokenVerifier
	oauth              oauth2.Config
	httpClient         *http.Client
	authz              *authz.Service
	productAudience    string
	publicBaseURL      *url.URL
	postLogoutURL      string
	endSessionEndpoint string
}

type browserAuthProviderMetadata struct {
	EndSessionEndpoint string `json:"end_session_endpoint"`
}

func NewBrowserAuth(ctx context.Context, cfg BrowserAuthConfig) (*BrowserAuth, error) {
	if cfg.PG == nil {
		return nil, errors.New("identity browser auth postgres pool is required")
	}
	if strings.TrimSpace(cfg.IssuerURL) == "" {
		return nil, errors.New("identity browser auth issuer URL is required")
	}
	if strings.TrimSpace(cfg.ClientID) == "" {
		return nil, errors.New("identity browser auth client ID is required")
	}
	if strings.TrimSpace(cfg.ClientSecret) == "" {
		return nil, errors.New("identity browser auth client secret is required")
	}
	publicBaseURL, err := parseBrowserAuthPublicBaseURL(cfg.PublicBaseURL)
	if err != nil {
		return nil, err
	}
	productAudience := strings.TrimSpace(cfg.ProductAudience)
	if productAudience == "" {
		return nil, errors.New("identity browser auth product audience is required")
	}
	if cfg.Authz == nil {
		return nil, errors.New("identity browser auth authorization graph is required")
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	provider, err := oidc.NewProvider(oidc.ClientContext(ctx, httpClient), cfg.IssuerURL)
	if err != nil {
		return nil, fmt.Errorf("identity browser auth oidc discovery: %w", err)
	}
	var metadata browserAuthProviderMetadata
	if err := provider.Claims(&metadata); err != nil {
		return nil, fmt.Errorf("identity browser auth oidc provider metadata: %w", err)
	}
	scopes := []string{
		"openid",
		"profile",
		"email",
		"offline_access",
		"urn:zitadel:iam:user:resourceowner",
		"urn:zitadel:iam:org:project:id:" + productAudience + ":aud",
	}
	callbackURL := publicBaseURL.ResolveReference(&url.URL{Path: browserAuthCallbackPath}).String()
	// Zitadel matches post_logout_redirect_uri against the registered string.
	postLogoutURL := publicBaseURL.String()
	return &BrowserAuth{
		q:        identitystore.New(cfg.PG),
		store:    identity.SQLStore{PG: cfg.PG},
		logger:   cfg.Logger,
		provider: provider,
		verifier: provider.Verifier(&oidc.Config{
			ClientID: cfg.ClientID,
		}),
		oauth: oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			Endpoint:     provider.Endpoint(),
			RedirectURL:  callbackURL,
			Scopes:       scopes,
		},
		httpClient:         httpClient,
		authz:              cfg.Authz,
		productAudience:    productAudience,
		publicBaseURL:      publicBaseURL,
		postLogoutURL:      postLogoutURL,
		endSessionEndpoint: strings.TrimSpace(metadata.EndSessionEndpoint),
	}, nil
}

func parseBrowserAuthPublicBaseURL(raw string) (*url.URL, error) {
	publicBaseURL, err := url.Parse(raw)
	if err != nil || !publicBaseURL.IsAbs() || publicBaseURL.Host == "" {
		return nil, fmt.Errorf("identity browser auth public base URL must be absolute: %q", raw)
	}
	if publicBaseURL.Path != "" && publicBaseURL.Path != "/" {
		return nil, fmt.Errorf("identity browser auth public base URL must not include a path: %q", raw)
	}
	if publicBaseURL.RawQuery != "" || publicBaseURL.Fragment != "" {
		return nil, fmt.Errorf("identity browser auth public base URL must not include query or fragment: %q", raw)
	}
	publicBaseURL.Path = ""
	publicBaseURL.RawPath = ""
	publicBaseURL.ForceQuery = false
	return publicBaseURL, nil
}

func RegisterBrowserAuthRoutes(mux *http.ServeMux, auth *BrowserAuth) {
	mux.Handle("/api/v1/auth/", http.StripPrefix("/api/v1/auth", auth))
}

func (a *BrowserAuth) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/login":
		a.requireMethod(w, r, http.MethodGet, a.handleLogin)
	case "/callback":
		a.requireMethod(w, r, http.MethodGet, a.handleCallback)
	case "/session":
		a.requireMethod(w, r, http.MethodGet, a.handleSession)
	case "/organization":
		a.requireMethod(w, r, http.MethodPost, a.handleOrganization)
	case "/resource-token":
		a.requireMethod(w, r, http.MethodPost, a.handleResourceToken)
	case "/logout":
		a.requireMethod(w, r, http.MethodGet, a.handleLogout)
	default:
		http.NotFound(w, r)
	}
}

func (a *BrowserAuth) requireMethod(w http.ResponseWriter, r *http.Request, method string, next http.HandlerFunc) {
	if r.Method != method {
		w.Header().Set("Allow", method)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	next(w, r)
}

func (a *BrowserAuth) handleLogin(w http.ResponseWriter, r *http.Request) {
	if err := a.q.DeleteExpiredBrowserLoginTransactions(r.Context()); err != nil {
		a.serverError(w, "cleanup login transactions", err)
		return
	}
	state, err := randomToken(32)
	if err != nil {
		a.serverError(w, "generate oidc state", err)
		return
	}
	nonce, err := randomToken(32)
	if err != nil {
		a.serverError(w, "generate oidc nonce", err)
		return
	}
	verifier, err := randomToken(48)
	if err != nil {
		a.serverError(w, "generate oidc verifier", err)
		return
	}
	redirectTo := a.sanitizeRedirectTarget(r.URL.Query().Get("redirect_to"))
	stateHash := hashToken(state)
	if err := a.q.InsertBrowserLoginTransaction(r.Context(), identitystore.InsertBrowserLoginTransactionParams{
		StateHash:    stateHash,
		Nonce:        nonce,
		CodeVerifier: verifier,
		RedirectTo:   redirectTo,
		ExpiresAt:    timestamptz(time.Now().UTC().Add(browserAuthLoginTTL)),
	}); err != nil {
		a.serverError(w, "persist login transaction", err)
		return
	}
	a.setLoginCookie(w, stateHash)
	http.Redirect(w, r, a.oauth.AuthCodeURL(
		state,
		oauth2.SetAuthURLParam("nonce", nonce),
		oauth2.S256ChallengeOption(verifier),
	), http.StatusSeeOther)
}

func (a *BrowserAuth) handleCallback(w http.ResponseWriter, r *http.Request) {
	a.clearLoginCookie(w)
	if oauthErr := r.URL.Query().Get("error"); oauthErr != "" {
		description := r.URL.Query().Get("error_description")
		if description != "" {
			http.Error(w, oauthErr+": "+description, http.StatusBadRequest)
			return
		}
		http.Error(w, oauthErr, http.StatusBadRequest)
		return
	}
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	state := strings.TrimSpace(r.URL.Query().Get("state"))
	if code == "" || state == "" {
		http.Error(w, "OIDC callback is missing code or state", http.StatusBadRequest)
		return
	}
	stateHash := hashToken(state)
	loginStateHash, ok := loginStateHashFromRequest(r)
	if !ok || subtle.ConstantTimeCompare([]byte(loginStateHash), []byte(stateHash)) != 1 {
		http.Error(w, "OIDC callback state did not originate from this browser", http.StatusBadRequest)
		return
	}
	pending, err := a.q.DeleteBrowserLoginTransaction(r.Context(), identitystore.DeleteBrowserLoginTransactionParams{
		StateHash: stateHash,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		http.Error(w, "OIDC callback state is missing or expired", http.StatusBadRequest)
		return
	}
	if err != nil {
		a.serverError(w, "load login transaction", err)
		return
	}
	tokens, err := a.exchangeToken(r.Context(), url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {a.oauth.RedirectURL},
		"code_verifier": {pending.CodeVerifier},
	})
	if err != nil {
		a.serverError(w, "exchange authorization code", err)
		return
	}
	if strings.TrimSpace(tokens.IDToken) == "" {
		http.Error(w, "OIDC callback returned no id_token", http.StatusBadGateway)
		return
	}
	verified, err := a.verifier.Verify(a.oidcContext(r.Context()), tokens.IDToken)
	if err != nil {
		http.Error(w, "OIDC callback returned an invalid id_token", http.StatusBadGateway)
		return
	}
	var idClaims map[string]any
	if err := verified.Claims(&idClaims); err != nil {
		a.serverError(w, "decode id_token claims", err)
		return
	}
	if nonce, _ := idClaims["nonce"].(string); nonce != pending.Nonce {
		http.Error(w, "OIDC callback nonce mismatch", http.StatusBadRequest)
		return
	}
	user, err := a.userSnapshot(r.Context(), tokens, idClaims, nil)
	if err != nil {
		a.serverError(w, "build browser auth session", err)
		return
	}
	sessionID, err := randomToken(32)
	if err != nil {
		a.serverError(w, "generate browser session", err)
		return
	}
	cachePartition, err := randomToken(24)
	if err != nil {
		a.serverError(w, "generate browser cache partition", err)
		return
	}
	sessionHash := hashToken(sessionID)
	if err := a.writeSession(r.Context(), sessionHash, cachePartition, tokens, user); err != nil {
		a.serverError(w, "persist browser session", err)
		return
	}
	a.setSessionCookie(w, sessionID)
	http.Redirect(w, r, a.absoluteRedirectTarget(pending.RedirectTo), http.StatusSeeOther)
}

func (a *BrowserAuth) handleSession(w http.ResponseWriter, r *http.Request) {
	session, err := a.sessionFromRequest(w, r)
	if err != nil {
		a.serverError(w, "load browser session", err)
		return
	}
	a.writeJSON(w, http.StatusOK, snapshotForSession(session))
}

func (a *BrowserAuth) handleOrganization(w http.ResponseWriter, r *http.Request) {
	if !a.sameOrigin(r) {
		http.Error(w, "origin not allowed", http.StatusForbidden)
		return
	}
	var input struct {
		OrgID string `json:"orgID"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "invalid organization payload", http.StatusBadRequest)
		return
	}
	orgID := strings.TrimSpace(input.OrgID)
	if orgID == "" {
		http.Error(w, "orgID is required", http.StatusBadRequest)
		return
	}
	session, err := a.requireSession(w, r)
	if err != nil {
		return
	}
	if _, ok := session.User.organization(orgID); !ok {
		http.Error(w, "selected organization is not available to this session", http.StatusForbidden)
		return
	}
	cachePartition, err := randomToken(24)
	if err != nil {
		a.serverError(w, "generate browser cache partition", err)
		return
	}
	if err := a.q.UpdateBrowserSessionOrganization(r.Context(), identitystore.UpdateBrowserSessionOrganizationParams{
		SelectedOrgID:        textValue(orgID),
		ClientCachePartition: cachePartition,
		SessionHash:          session.SessionHash,
	}); err != nil {
		a.serverError(w, "update selected organization", err)
		return
	}
	if err := a.q.DeleteBrowserResourceTokens(r.Context(), identitystore.DeleteBrowserResourceTokensParams{SessionHash: session.SessionHash}); err != nil {
		a.serverError(w, "delete browser resource tokens", err)
		return
	}
	next, err := a.readSession(r.Context(), session.SessionHash)
	if err != nil {
		a.serverError(w, "reload browser session", err)
		return
	}
	a.writeJSON(w, http.StatusOK, snapshotForSession(next))
}

func (a *BrowserAuth) handleResourceToken(w http.ResponseWriter, r *http.Request) {
	if !a.sameOrigin(r) {
		http.Error(w, "origin not allowed", http.StatusForbidden)
		return
	}
	session, err := a.requireSession(w, r)
	if err != nil {
		return
	}
	token, err := a.resourceToken(r.Context(), session)
	if err != nil {
		a.serverError(w, "exchange browser resource token", err)
		return
	}
	a.writeJSON(w, http.StatusOK, map[string]string{"accessToken": token})
}

func (a *BrowserAuth) handleLogout(w http.ResponseWriter, r *http.Request) {
	sessionID, ok := sessionIDFromRequest(r)
	var idToken string
	if ok {
		sessionHash := hashToken(sessionID)
		if session, err := a.readSession(r.Context(), sessionHash); err == nil && session.IDToken != "" {
			idToken = session.IDToken
		}
		if err := a.q.DeleteBrowserSession(r.Context(), identitystore.DeleteBrowserSessionParams{SessionHash: sessionHash}); err != nil {
			a.serverError(w, "delete browser session", err)
			return
		}
	}
	a.clearSessionCookie(w)
	a.clearLoginCookie(w)
	if idToken == "" || a.endSessionEndpoint == "" {
		http.Redirect(w, r, a.postLogoutURL, http.StatusSeeOther)
		return
	}
	logoutURL, err := url.Parse(a.endSessionEndpoint)
	if err != nil {
		a.serverError(w, "parse oidc end-session endpoint", err)
		return
	}
	query := logoutURL.Query()
	query.Set("id_token_hint", idToken)
	query.Set("post_logout_redirect_uri", a.postLogoutURL)
	logoutURL.RawQuery = query.Encode()
	http.Redirect(w, r, logoutURL.String(), http.StatusSeeOther)
}

func (a *BrowserAuth) sessionFromRequest(w http.ResponseWriter, r *http.Request) (*browserSession, error) {
	sessionID, ok := sessionIDFromRequest(r)
	if !ok {
		return nil, nil
	}
	session, err := a.readSession(r.Context(), hashToken(sessionID))
	if errors.Is(err, pgx.ErrNoRows) {
		a.clearSessionCookie(w)
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if time.Until(session.ExpiresAt) > browserAuthRefreshLeeway {
		return session, nil
	}
	refreshed, err := a.refreshSession(r.Context(), session)
	if err != nil {
		if a.logger != nil {
			a.logger.WarnContext(r.Context(), "browser auth token refresh failed", "error", err, "subject", session.User.Sub)
		}
		if err := a.q.DeleteBrowserSession(r.Context(), identitystore.DeleteBrowserSessionParams{SessionHash: session.SessionHash}); err != nil {
			return nil, err
		}
		a.clearSessionCookie(w)
		return nil, nil
	}
	return refreshed, nil
}

func (a *BrowserAuth) requireSession(w http.ResponseWriter, r *http.Request) (*browserSession, error) {
	session, err := a.sessionFromRequest(w, r)
	if err != nil {
		a.serverError(w, "load browser session", err)
		return nil, err
	}
	if session == nil {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return nil, errors.New("authentication required")
	}
	return session, nil
}

func (a *BrowserAuth) refreshSession(ctx context.Context, session *browserSession) (*browserSession, error) {
	if session.RefreshToken == "" {
		return nil, errors.New("browser session has no refresh token")
	}
	tokens, err := a.exchangeToken(ctx, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {session.RefreshToken},
	})
	if err != nil {
		return nil, err
	}
	if tokens.RefreshToken == "" {
		tokens.RefreshToken = session.RefreshToken
	}
	if tokens.IDToken == "" {
		return nil, errors.New("refresh response returned no id_token")
	}
	verified, err := a.verifier.Verify(a.oidcContext(ctx), tokens.IDToken)
	if err != nil {
		return nil, fmt.Errorf("verify refreshed id_token: %w", err)
	}
	var idClaims map[string]any
	if err := verified.Claims(&idClaims); err != nil {
		return nil, fmt.Errorf("decode refreshed id_token claims: %w", err)
	}
	previousSelectedOrgID := session.User.SelectedOrgID
	user, err := a.userSnapshot(ctx, tokens, idClaims, previousSelectedOrgID)
	if err != nil {
		return nil, err
	}
	cachePartition := session.ClientCachePartition
	if stringValue(previousSelectedOrgID) != stringValue(user.SelectedOrgID) {
		var err error
		cachePartition, err = randomToken(24)
		if err != nil {
			return nil, err
		}
		if err := a.q.DeleteBrowserResourceTokens(ctx, identitystore.DeleteBrowserResourceTokensParams{SessionHash: session.SessionHash}); err != nil {
			return nil, err
		}
	}
	if err := a.writeSession(ctx, session.SessionHash, cachePartition, tokens, user); err != nil {
		return nil, err
	}
	return a.readSession(ctx, session.SessionHash)
}

func (a *BrowserAuth) resourceToken(ctx context.Context, session *browserSession) (string, error) {
	selectedOrgID := stringValue(session.User.SelectedOrgID)
	if selectedOrgID == "" {
		return "", errors.New("selected organization is required for resource token exchange")
	}
	audience := strings.TrimSpace(a.productAudience)
	if audience == "" {
		return "", errors.New("product audience is required for resource token exchange")
	}
	selectedOrganization, ok := session.User.organization(selectedOrgID)
	if !ok || strings.TrimSpace(selectedOrganization.IdentityProviderOrgID) == "" {
		return "", errors.New("selected organization provider id is required for resource token exchange")
	}
	providerOrgID := strings.TrimSpace(selectedOrganization.IdentityProviderOrgID)
	requestedScopes := []string{
		"openid",
		"profile",
		"email",
		"urn:zitadel:iam:org:id:" + providerOrgID,
		"urn:zitadel:iam:org:project:id:" + audience + ":aud",
	}
	requestedScope := strings.Join(requestedScopes, " ")
	scopeHash := hashToken(requestedScope)
	ctx, span := browserAuthTracer.Start(ctx, "identity.browser_auth.resource_token.exchange")
	defer span.End()
	span.SetAttributes(
		attribute.String("auth.audience", audience),
		attribute.String("auth.selected_org_id", selectedOrgID),
		attribute.String("auth.scope_hash", scopeHash),
	)
	cached, err := a.q.GetBrowserResourceToken(ctx, identitystore.GetBrowserResourceTokenParams{
		SessionHash:   session.SessionHash,
		Audience:      audience,
		SelectedOrgID: selectedOrgID,
		ScopeHash:     scopeHash,
	})
	if err == nil && time.Until(requiredTime(cached.ExpiresAt)) > browserAuthRefreshLeeway {
		if err := a.verifyAccessToken(ctx, cached.AccessToken, audience, providerOrgID); err == nil {
			span.SetAttributes(attribute.Bool("auth.cache_hit", true))
			span.SetStatus(codes.Ok, "")
			return cached.AccessToken, nil
		}
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return "", err
	}
	span.SetAttributes(attribute.Bool("auth.cache_hit", false))
	tokens, err := a.exchangeToken(ctx, url.Values{
		"grant_type":           {"urn:ietf:params:oauth:grant-type:token-exchange"},
		"subject_token":        {session.AccessToken},
		"subject_token_type":   {"urn:ietf:params:oauth:token-type:access_token"},
		"requested_token_type": {"urn:ietf:params:oauth:token-type:jwt"},
		"audience":             {audience},
		"scope":                {requestedScope},
	})
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return "", err
	}
	if strings.ToLower(tokens.TokenType) != "bearer" || tokens.AccessToken == "" {
		err := errors.New("token exchange did not return a bearer access token")
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return "", err
	}
	expiresAt, _, err := a.verifyAccessTokenClaims(ctx, tokens.AccessToken, audience, providerOrgID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return "", err
	}
	if err := a.q.UpsertBrowserResourceToken(ctx, identitystore.UpsertBrowserResourceTokenParams{
		SessionHash:   session.SessionHash,
		Audience:      audience,
		SelectedOrgID: selectedOrgID,
		ScopeHash:     scopeHash,
		AccessToken:   tokens.AccessToken,
		TokenScope:    nullableText(tokens.Scope),
		ExpiresAt:     timestamptz(expiresAt),
	}); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return "", err
	}
	span.SetStatus(codes.Ok, "")
	return tokens.AccessToken, nil
}

func (a *BrowserAuth) verifyAccessToken(ctx context.Context, accessToken, audience, selectedOrgID string) error {
	_, _, err := a.verifyAccessTokenClaims(ctx, accessToken, audience, selectedOrgID)
	return err
}

func (a *BrowserAuth) verifyAccessTokenClaims(ctx context.Context, accessToken, audience, selectedOrgID string) (time.Time, map[string]any, error) {
	verifier := a.provider.Verifier(&oidc.Config{ClientID: audience})
	token, err := verifier.Verify(a.oidcContext(ctx), accessToken)
	if err != nil {
		return time.Time{}, nil, err
	}
	var claims map[string]any
	if err := token.Claims(&claims); err != nil {
		return time.Time{}, nil, err
	}
	if err := verifySelectedOrganizationClaims(claims, selectedOrgID); err != nil {
		return time.Time{}, nil, err
	}
	return token.Expiry, claims, nil
}

func verifySelectedOrganizationClaims(claims map[string]any, selectedOrgID string) error {
	if asserted, ok := claims["urn:zitadel:iam:org:id"].(string); ok && asserted != "" && asserted != selectedOrgID {
		return errors.New("access token selected organization mismatch")
	}
	return nil
}

func (a *BrowserAuth) userSnapshot(ctx context.Context, tokens tokenResponse, idClaims map[string]any, previousSelectedOrgID *string) (browserUser, error) {
	accessClaims, err := decodeJWTPayload(tokens.AccessToken)
	if err != nil {
		return browserUser{}, err
	}
	userInfo, err := a.fetchUserInfo(ctx, tokens.AccessToken)
	if err != nil {
		return browserUser{}, err
	}
	claims := mergeClaims(accessClaims, idClaims, userInfo)
	email := stringClaim(claims, "email")
	preferredUsername := stringClaim(claims, "preferred_username")
	name := stringClaim(claims, "name")
	if name == nil {
		name = firstString(preferredUsername, email, stringClaim(idClaims, "sub"))
	}
	providerHomeOrgID := stringClaim(claims, zitadelResourceOwnerID)
	sub := stringValue(stringClaim(idClaims, "sub"))
	if sub == "" {
		return browserUser{}, errors.New("id_token subject is required")
	}
	if providerHomeOrgID == nil {
		return browserUser{}, errors.New("id_token resource owner organization is required")
	}
	if _, err := a.ensureAccountOrganization(ctx, accountOrganizationInput{
		Subject:             sub,
		ProviderOrgID:       *providerHomeOrgID,
		ProviderDisplayName: stringClaim(claims, zitadelResourceOwnerName),
		Email:               email,
		PreferredUsername:   preferredUsername,
	}); err != nil {
		return browserUser{}, err
	}
	organizations, err := a.publicOrganizationContexts(ctx, sub)
	if err != nil {
		return browserUser{}, err
	}
	if len(organizations) == 0 {
		return browserUser{}, errors.New("signed-in subject has no accessible IAM organization")
	}
	homeOrgID := publicOrgIDForProviderOrgID(organizations, providerHomeOrgID)
	selectedOrgID := selectInitialOrganizationID(organizations, providerHomeOrgID, previousSelectedOrgID)
	return browserUser{
		Sub:                    sub,
		Email:                  email,
		Name:                   name,
		PreferredUsername:      preferredUsername,
		HomeOrgID:              homeOrgID,
		SelectedOrgID:          selectedOrgID,
		OrgID:                  selectedOrgID,
		AvailableOrganizations: organizations,
		Claims:                 claims,
	}, nil
}

func (a *BrowserAuth) fetchUserInfo(ctx context.Context, accessToken string) (map[string]any, error) {
	info, err := a.provider.UserInfo(a.oidcContext(ctx), oauth2.StaticTokenSource(&oauth2.Token{
		AccessToken: accessToken,
		TokenType:   "Bearer",
	}))
	if err != nil {
		return nil, fmt.Errorf("fetch oidc userinfo: %w", err)
	}
	var claims map[string]any
	if err := info.Claims(&claims); err != nil {
		return nil, fmt.Errorf("decode oidc userinfo: %w", err)
	}
	return claims, nil
}

type accountOrganizationInput struct {
	Subject             string
	ProviderOrgID       string
	ProviderDisplayName *string
	Email               *string
	PreferredUsername   *string
}

func (a *BrowserAuth) ensureAccountOrganization(ctx context.Context, input accountOrganizationInput) (profile identity.OrganizationProfile, err error) {
	input.Subject = strings.TrimSpace(input.Subject)
	input.ProviderOrgID = strings.TrimSpace(input.ProviderOrgID)
	ctx, span := browserAuthTracer.Start(ctx, "iam.browser_auth.account_organization.ensure")
	defer func() {
		span.SetAttributes(
			attribute.String("iam.identity_provider_org_id", input.ProviderOrgID),
			attribute.String("enduser.id", input.Subject),
		)
		if profile.OrgID != "" {
			span.SetAttributes(attribute.String("verself.org_id", profile.OrgID), attribute.String("iam.org_slug", profile.Slug))
		}
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()
	if input.Subject == "" {
		return identity.OrganizationProfile{}, errors.New("account organization subject is required")
	}
	if input.ProviderOrgID == "" {
		return identity.OrganizationProfile{}, errors.New("account organization provider org id is required")
	}
	profile, err = a.store.ResolveOrganizationProfile(ctx, identity.ResolveOrganizationRequest{
		IdentityProviderOrgID: input.ProviderOrgID,
		RequireActive:         true,
	})
	if err == nil {
		if profile.CreatedBy == input.Subject {
			if repaired, repairErr := a.ensureCreatorOwnerPolicyIfEmpty(ctx, profile.OrgID, input.Subject); repairErr != nil {
				return identity.OrganizationProfile{}, repairErr
			} else if repaired {
				span.SetAttributes(attribute.Bool("iam.account_organization.owner_policy_repaired", true))
			}
		}
		span.SetAttributes(attribute.Bool("iam.account_organization.created", false))
		return profile, nil
	}
	if !errors.Is(err, identity.ErrOrganizationMissing) {
		return identity.OrganizationProfile{}, fmt.Errorf("resolve account organization: %w", err)
	}
	profile, err = a.createAccountOrganizationProfile(ctx, input)
	if err != nil {
		return identity.OrganizationProfile{}, err
	}
	if err := a.setInitialOwnerPolicy(ctx, profile.OrgID, input.Subject); err != nil {
		return identity.OrganizationProfile{}, err
	}
	span.SetAttributes(attribute.Bool("iam.account_organization.created", true))
	return profile, nil
}

func (a *BrowserAuth) createAccountOrganizationProfile(ctx context.Context, input accountOrganizationInput) (identity.OrganizationProfile, error) {
	displayName := accountOrganizationDisplayName(input)
	slugBase := accountOrganizationSlugBase(displayName, input)
	for attempt := 0; attempt < accountOrganizationRetries; attempt++ {
		orgID, err := randomPublicOrganizationID()
		if err != nil {
			return identity.OrganizationProfile{}, fmt.Errorf("generate account organization id: %w", err)
		}
		slug := slugBase
		if attempt > 0 {
			slug = accountOrganizationSlugWithSuffix(slugBase, orgID[len(orgID)-6:])
		}
		profile, err := a.store.CreateOrganizationProfile(ctx, identity.CreateOrganizationRequest{
			OrgID:                 orgID,
			IdentityProviderOrgID: input.ProviderOrgID,
			DisplayName:           displayName,
			Slug:                  slug,
			ActorID:               input.Subject,
		})
		if err == nil {
			return profile, nil
		}
		if !errors.Is(err, identity.ErrOrganizationConflict) {
			return identity.OrganizationProfile{}, fmt.Errorf("create account organization profile: %w", err)
		}
	}
	return identity.OrganizationProfile{}, fmt.Errorf("%w: account organization profile is unavailable", identity.ErrOrganizationConflict)
}

func (a *BrowserAuth) setInitialOwnerPolicy(ctx context.Context, orgID, subject string) (err error) {
	ctx, span := browserAuthTracer.Start(ctx, "iam.browser_auth.account_owner_policy.set")
	defer func() {
		span.SetAttributes(attribute.String("verself.org_id", orgID), attribute.String("enduser.id", subject))
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()
	_, err = a.authz.SetOrganizationPolicy(ctx, orgID, authz.Policy{
		Bindings: []authz.PolicyBinding{{
			Role:    "roles/owner",
			Members: []string{"user:" + subject},
		}},
	}, "iam.browser_auth.account_organization_create")
	if err != nil {
		return fmt.Errorf("set initial account owner policy: %w", err)
	}
	return nil
}

func (a *BrowserAuth) ensureCreatorOwnerPolicyIfEmpty(ctx context.Context, orgID, subject string) (bool, error) {
	policy, err := a.authz.GetOrganizationPolicy(ctx, orgID)
	if err != nil {
		return false, fmt.Errorf("read account organization owner policy: %w", err)
	}
	if len(policy.Bindings) > 0 {
		return false, nil
	}
	return true, a.setInitialOwnerPolicy(ctx, orgID, subject)
}

func accountOrganizationDisplayName(input accountOrganizationInput) string {
	for _, candidate := range []string{
		stringValue(input.ProviderDisplayName),
		domainFromEmail(input.Email),
		stringValue(input.PreferredUsername),
		stringValue(input.Email),
		"Organization " + input.Subject,
	} {
		if value := fitOrganizationDisplayName(candidate); value != "" {
			return value
		}
	}
	return "Organization"
}

func accountOrganizationSlugBase(displayName string, input accountOrganizationInput) string {
	for _, candidate := range []string{
		displayName,
		domainFromEmail(input.Email),
		stringValue(input.PreferredUsername),
		"organization",
	} {
		if slug := trimOrganizationSlug(identity.NormalizeOrganizationSlug(candidate)); slug != "" {
			return slug
		}
	}
	return "organization"
}

func accountOrganizationSlugWithSuffix(base, suffix string) string {
	suffix = identity.NormalizeOrganizationSlug(suffix)
	if suffix == "" {
		suffix = "new"
	}
	maxBase := 80 - len(suffix) - 1
	if maxBase < 1 {
		maxBase = 1
	}
	base = trimOrganizationSlug(base)
	if len(base) > maxBase {
		base = strings.Trim(base[:maxBase], "-")
	}
	if base == "" {
		base = "org"
	}
	return base + "-" + suffix
}

func trimOrganizationSlug(slug string) string {
	if len(slug) <= 80 {
		return strings.Trim(slug, "-")
	}
	return strings.Trim(slug[:80], "-")
}

func fitOrganizationDisplayName(value string) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if value == "" {
		return ""
	}
	runes := []rune(value)
	if len(runes) > 120 {
		value = string(runes[:120])
	}
	for len(value) > 240 {
		runes = []rune(value)
		if len(runes) == 0 {
			return ""
		}
		value = string(runes[:len(runes)-1])
	}
	return strings.TrimSpace(value)
}

func domainFromEmail(value *string) string {
	raw := strings.TrimSpace(stringValue(value))
	idx := strings.LastIndexByte(raw, '@')
	if idx < 0 || idx == len(raw)-1 {
		return ""
	}
	return raw[idx+1:]
}

func (a *BrowserAuth) exchangeToken(ctx context.Context, params url.Values) (tokenResponse, error) {
	params.Set("client_id", a.oauth.ClientID)
	params.Set("client_secret", a.oauth.ClientSecret)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.oauth.Endpoint.TokenURL, strings.NewReader(params.Encode()))
	if err != nil {
		return tokenResponse{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return tokenResponse{}, fmt.Errorf("oidc token request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		var body struct {
			Error            string `json:"error"`
			ErrorDescription string `json:"error_description"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&body)
		if body.ErrorDescription != "" {
			return tokenResponse{}, fmt.Errorf("oidc token request failed: %s", body.ErrorDescription)
		}
		if body.Error != "" {
			return tokenResponse{}, fmt.Errorf("oidc token request failed: %s", body.Error)
		}
		return tokenResponse{}, fmt.Errorf("oidc token request failed with HTTP %d", resp.StatusCode)
	}
	var tokens tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokens); err != nil {
		return tokenResponse{}, err
	}
	if tokens.AccessToken == "" {
		return tokenResponse{}, errors.New("oidc token response is missing access_token")
	}
	if tokens.ExpiresIn <= 0 {
		return tokenResponse{}, errors.New("oidc token response is missing expires_in")
	}
	tokens.ExpiresAt = time.Now().UTC().Add(time.Duration(tokens.ExpiresIn) * time.Second)
	return tokens, nil
}

func (a *BrowserAuth) readSession(ctx context.Context, sessionHash string) (*browserSession, error) {
	row, err := a.q.GetBrowserSession(ctx, identitystore.GetBrowserSessionParams{SessionHash: sessionHash})
	if err != nil {
		return nil, err
	}
	var organizations []authOrganizationContext
	if err := json.Unmarshal([]byte(row.AvailableOrgContextsJson), &organizations); err != nil {
		return nil, err
	}
	var claims map[string]any
	if err := json.Unmarshal([]byte(row.UserClaimsJson), &claims); err != nil {
		return nil, err
	}
	user := browserUser{
		Sub:                    row.Subject,
		Email:                  stringFromText(row.Email),
		Name:                   stringFromText(row.DisplayName),
		PreferredUsername:      stringFromText(row.PreferredUsername),
		HomeOrgID:              stringFromText(row.HomeOrgID),
		SelectedOrgID:          stringFromText(row.SelectedOrgID),
		OrgID:                  stringFromText(row.SelectedOrgID),
		AvailableOrganizations: organizations,
		Claims:                 claims,
	}
	return &browserSession{
		SessionHash:          row.SessionHash,
		ClientCachePartition: row.ClientCachePartition,
		AccessToken:          row.AccessToken,
		RefreshToken:         stringValue(stringFromText(row.RefreshToken)),
		IDToken:              stringValue(stringFromText(row.IDToken)),
		TokenScope:           stringFromText(row.TokenScope),
		ExpiresAt:            requiredTime(row.ExpiresAt),
		CreatedAt:            requiredTime(row.CreatedAt),
		UpdatedAt:            requiredTime(row.UpdatedAt),
		User:                 user,
	}, nil
}

func (a *BrowserAuth) writeSession(ctx context.Context, sessionHash, cachePartition string, tokens tokenResponse, user browserUser) error {
	organizations, err := json.Marshal(user.AvailableOrganizations)
	if err != nil {
		return err
	}
	claims, err := json.Marshal(user.Claims)
	if err != nil {
		return err
	}
	return a.q.UpsertBrowserSession(ctx, identitystore.UpsertBrowserSessionParams{
		SessionHash:              sessionHash,
		ClientCachePartition:     cachePartition,
		Subject:                  user.Sub,
		Email:                    nullableTextPtr(user.Email),
		DisplayName:              nullableTextPtr(user.Name),
		PreferredUsername:        nullableTextPtr(user.PreferredUsername),
		OrgID:                    nullableTextPtr(user.OrgID),
		HomeOrgID:                nullableTextPtr(user.HomeOrgID),
		SelectedOrgID:            nullableTextPtr(user.SelectedOrgID),
		AvailableOrgContextsJson: organizations,
		UserClaimsJson:           claims,
		IDToken:                  nullableText(tokens.IDToken),
		AccessToken:              tokens.AccessToken,
		RefreshToken:             nullableText(tokens.RefreshToken),
		TokenScope:               nullableText(tokens.Scope),
		ExpiresAt:                timestamptz(tokens.ExpiresAt),
	})
}

func (a *BrowserAuth) sanitizeRedirectTarget(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return browserAuthDefaultRedirect
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return browserAuthDefaultRedirect
	}
	resolved := a.publicBaseURL.ResolveReference(parsed)
	if resolved.Scheme != a.publicBaseURL.Scheme || resolved.Host != a.publicBaseURL.Host {
		return browserAuthDefaultRedirect
	}
	switch resolved.Path {
	case "/login", "/callback", "/logout", browserAuthCallbackPath:
		return browserAuthDefaultRedirect
	}
	if resolved.Path == "" {
		resolved.Path = "/"
	}
	return (&url.URL{Path: resolved.Path, RawQuery: resolved.RawQuery, Fragment: resolved.Fragment}).String()
}

func (a *BrowserAuth) absoluteRedirectTarget(path string) string {
	parsed, err := url.Parse(path)
	if err != nil {
		return a.publicBaseURL.ResolveReference(&url.URL{Path: browserAuthDefaultRedirect}).String()
	}
	return a.publicBaseURL.ResolveReference(parsed).String()
}

func (a *BrowserAuth) sameOrigin(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	return err == nil && parsed.Scheme == a.publicBaseURL.Scheme && parsed.Host == a.publicBaseURL.Host
}

func (a *BrowserAuth) oidcContext(ctx context.Context) context.Context {
	return oidc.ClientContext(ctx, a.httpClient)
}

func (a *BrowserAuth) writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func (a *BrowserAuth) serverError(w http.ResponseWriter, operation string, err error) {
	if a.logger != nil {
		a.logger.Error("browser auth failed", "operation", operation, "error", err)
	}
	http.Error(w, operation+" failed", http.StatusInternalServerError)
}

func (a *BrowserAuth) setSessionCookie(w http.ResponseWriter, sessionID string) {
	http.SetCookie(w, &http.Cookie{
		Name:     browserAuthCookieName,
		Value:    sessionID,
		Path:     "/",
		Expires:  time.Now().UTC().Add(browserAuthSessionTTL),
		MaxAge:   int(browserAuthSessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
}

func (a *BrowserAuth) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     browserAuthCookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0).UTC(),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
}

func (a *BrowserAuth) setLoginCookie(w http.ResponseWriter, stateHash string) {
	http.SetCookie(w, &http.Cookie{
		Name:     browserAuthLoginCookieName,
		Value:    stateHash,
		Path:     "/api/v1/auth",
		Expires:  time.Now().UTC().Add(browserAuthLoginTTL),
		MaxAge:   int(browserAuthLoginTTL.Seconds()),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
}

func (a *BrowserAuth) clearLoginCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     browserAuthLoginCookieName,
		Value:    "",
		Path:     "/api/v1/auth",
		Expires:  time.Unix(0, 0).UTC(),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
}

func sessionIDFromRequest(r *http.Request) (string, bool) {
	cookie, err := r.Cookie(browserAuthCookieName)
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		return "", false
	}
	return cookie.Value, true
}

func loginStateHashFromRequest(r *http.Request) (string, bool) {
	cookie, err := r.Cookie(browserAuthLoginCookieName)
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		return "", false
	}
	return cookie.Value, true
}

type tokenResponse struct {
	AccessToken  string    `json:"access_token"`
	TokenType    string    `json:"token_type"`
	ExpiresIn    int       `json:"expires_in"`
	RefreshToken string    `json:"refresh_token"`
	IDToken      string    `json:"id_token"`
	Scope        string    `json:"scope"`
	ExpiresAt    time.Time `json:"-"`
}

type browserSession struct {
	SessionHash          string
	ClientCachePartition string
	AccessToken          string
	RefreshToken         string
	IDToken              string
	TokenScope           *string
	ExpiresAt            time.Time
	CreatedAt            time.Time
	UpdatedAt            time.Time
	User                 browserUser
}

type browserUser struct {
	Sub                    string                    `json:"sub"`
	Email                  *string                   `json:"email"`
	Name                   *string                   `json:"name"`
	PreferredUsername      *string                   `json:"preferredUsername"`
	HomeOrgID              *string                   `json:"homeOrgID"`
	SelectedOrgID          *string                   `json:"selectedOrgID"`
	OrgID                  *string                   `json:"orgID"`
	AvailableOrganizations []authOrganizationContext `json:"availableOrganizations"`
	Claims                 map[string]any            `json:"-"`
}

func (u browserUser) organization(orgID string) (authOrganizationContext, bool) {
	for _, organization := range u.AvailableOrganizations {
		if organization.OrgID == orgID {
			return organization, true
		}
	}
	return authOrganizationContext{}, false
}

type authOrganizationContext struct {
	OrgID                 string `json:"orgID"`
	IdentityProviderOrgID string `json:"identityProviderOrgID"`
}

type authState struct {
	IsAuthenticated bool    `json:"isAuthenticated"`
	UserID          *string `json:"userId"`
	OrgID           *string `json:"orgId"`
	SelectedOrgID   *string `json:"selectedOrgId"`
	CachePartition  *string `json:"cachePartition"`
}

type sessionInfo struct {
	CreatedAt time.Time `json:"createdAt"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type authSnapshot struct {
	IsSignedIn bool         `json:"isSignedIn"`
	Auth       authState    `json:"auth"`
	User       *browserUser `json:"user"`
	Session    *sessionInfo `json:"session"`
}

func snapshotForSession(session *browserSession) authSnapshot {
	if session == nil {
		return authSnapshot{
			IsSignedIn: false,
			Auth: authState{
				IsAuthenticated: false,
			},
		}
	}
	userID := session.User.Sub
	cachePartition := session.ClientCachePartition
	return authSnapshot{
		IsSignedIn: true,
		Auth: authState{
			IsAuthenticated: true,
			UserID:          &userID,
			OrgID:           session.User.SelectedOrgID,
			SelectedOrgID:   session.User.SelectedOrgID,
			CachePartition:  &cachePartition,
		},
		User: &browserUser{
			Sub:                    session.User.Sub,
			Email:                  session.User.Email,
			Name:                   session.User.Name,
			PreferredUsername:      session.User.PreferredUsername,
			HomeOrgID:              session.User.HomeOrgID,
			SelectedOrgID:          session.User.SelectedOrgID,
			OrgID:                  session.User.SelectedOrgID,
			AvailableOrganizations: session.User.AvailableOrganizations,
		},
		Session: &sessionInfo{CreatedAt: session.CreatedAt, ExpiresAt: session.ExpiresAt},
	}
}

func (a *BrowserAuth) publicOrganizationContexts(ctx context.Context, subject string) ([]authOrganizationContext, error) {
	orgIDs, _, err := a.authz.LookupOrganizations(ctx, identity.AuthorizationSubject{
		Kind: identity.AuthorizationSubjectKindUser,
		ID:   subject,
	}, "read", "")
	if err != nil {
		return nil, fmt.Errorf("lookup browser organization contexts: %w", err)
	}
	orgIDs = compactUniqueStrings(orgIDs)
	if len(orgIDs) == 0 {
		return []authOrganizationContext{}, nil
	}
	rows, err := a.q.ListOrganizationMetadataByOrgIDs(ctx, identitystore.ListOrganizationMetadataByOrgIDsParams{OrgIds: orgIDs})
	if err != nil {
		return nil, fmt.Errorf("map browser organization contexts: %w", err)
	}
	seen := map[string]struct{}{}
	out := make([]authOrganizationContext, 0, len(rows))
	for _, row := range rows {
		seen[row.OrgID] = struct{}{}
		out = append(out, authOrganizationContext{OrgID: row.OrgID, IdentityProviderOrgID: row.IdentityProviderOrgID})
	}
	if len(seen) != len(orgIDs) {
		return nil, errors.New("browser organization context missing IAM metadata")
	}
	return out, nil
}

func publicOrgIDForProviderOrgID(contexts []authOrganizationContext, providerOrgID *string) *string {
	if providerOrgID == nil {
		return nil
	}
	for _, context := range contexts {
		if context.IdentityProviderOrgID == *providerOrgID {
			value := context.OrgID
			return &value
		}
	}
	return nil
}

func selectInitialOrganizationID(contexts []authOrganizationContext, providerHomeOrgID, previousSelectedOrgID *string) *string {
	if previousSelectedOrgID != nil {
		for _, context := range contexts {
			if context.OrgID == *previousSelectedOrgID {
				return previousSelectedOrgID
			}
		}
	}
	if providerHomeOrgID != nil {
		for _, context := range contexts {
			if context.IdentityProviderOrgID == *providerHomeOrgID {
				selected := context.OrgID
				return &selected
			}
		}
	}
	if len(contexts) > 0 {
		selected := contexts[0].OrgID
		return &selected
	}
	return nil
}

func decodeJWTPayload(raw string) (map[string]any, error) {
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return nil, errors.New("jwt must have three segments")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, err
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, err
	}
	return claims, nil
}

func mergeClaims(claimSets ...map[string]any) map[string]any {
	merged := map[string]any{}
	for _, claims := range claimSets {
		for key, value := range claims {
			merged[key] = value
		}
	}
	return merged
}

func randomToken(bytes int) (string, error) {
	buf := make([]byte, bytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func randomPublicOrganizationID() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return "org_" + crockfordEncodeBytes(raw)[:publicIDPayloadLength], nil
}

func hashToken(value string) string {
	sum := sha256.Sum256([]byte(value))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func compactUniqueStrings(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}

func stringClaim(claims map[string]any, name string) *string {
	value, ok := claims[name].(string)
	if !ok || strings.TrimSpace(value) == "" {
		return nil
	}
	return &value
}

func firstString(values ...*string) *string {
	for _, value := range values {
		if value != nil && strings.TrimSpace(*value) != "" {
			return value
		}
	}
	return nil
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func timestamptz(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value.UTC(), Valid: true}
}

func requiredTime(value pgtype.Timestamptz) time.Time {
	if !value.Valid {
		return time.Time{}
	}
	return value.Time.UTC()
}

func textValue(value string) pgtype.Text {
	return pgtype.Text{String: value, Valid: true}
}

func nullableText(value string) pgtype.Text {
	if strings.TrimSpace(value) == "" {
		return pgtype.Text{}
	}
	return textValue(value)
}

func nullableTextPtr(value *string) pgtype.Text {
	if value == nil {
		return pgtype.Text{}
	}
	return nullableText(*value)
}

func stringFromText(value pgtype.Text) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}
