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
	"sort"
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

	identitystore "github.com/verself/identity-service/internal/store"
)

const (
	browserAuthCookieName      = "verself_session"
	browserAuthLoginCookieName = "verself_login"
	browserAuthSessionTTL      = 30 * 24 * time.Hour
	browserAuthLoginTTL        = 5 * time.Minute
	browserAuthRefreshLeeway   = 60 * time.Second
	browserAuthCallbackPath    = "/api/v1/auth/callback"
	browserAuthDefaultRedirect = "/"
)

var browserAuthTracer = otel.Tracer("github.com/verself/identity-service/browser-auth")

type BrowserAuthConfig struct {
	PG             *pgxpool.Pool
	Logger         *slog.Logger
	IssuerURL      string
	ClientID       string
	ClientSecret   string
	PublicBaseURL  string
	LoginAudiences []string
	HTTPClient     *http.Client
}

type BrowserAuth struct {
	q                  *identitystore.Queries
	logger             *slog.Logger
	provider           *oidc.Provider
	verifier           *oidc.IDTokenVerifier
	oauth              oauth2.Config
	httpClient         *http.Client
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
	publicBaseURL, err := url.Parse(cfg.PublicBaseURL)
	if err != nil || !publicBaseURL.IsAbs() || publicBaseURL.Host == "" {
		return nil, fmt.Errorf("identity browser auth public base URL must be absolute: %q", cfg.PublicBaseURL)
	}
	loginAudiences := compactUniqueStrings(cfg.LoginAudiences)
	if len(loginAudiences) == 0 {
		return nil, errors.New("identity browser auth login audiences are required")
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
	}
	for _, audience := range loginAudiences {
		scopes = append(scopes, "urn:zitadel:iam:org:project:id:"+audience+":aud")
	}
	scopes = append(scopes, "urn:zitadel:iam:org:projects:roles")
	callbackURL := publicBaseURL.ResolveReference(&url.URL{Path: browserAuthCallbackPath}).String()
	postLogoutURL := publicBaseURL.ResolveReference(&url.URL{Path: "/"}).String()
	return &BrowserAuth{
		q:        identitystore.New(cfg.PG),
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
		publicBaseURL:      publicBaseURL,
		postLogoutURL:      postLogoutURL,
		endSessionEndpoint: strings.TrimSpace(metadata.EndSessionEndpoint),
	}, nil
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
	organization, ok := session.User.organization(orgID)
	if !ok {
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
		Roles:                organization.Roles,
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
	var input struct {
		Audience            string `json:"audience"`
		RoleAssignmentScope string `json:"roleAssignmentScope"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "invalid resource token payload", http.StatusBadRequest)
		return
	}
	audience := strings.TrimSpace(input.Audience)
	if audience == "" {
		http.Error(w, "audience is required", http.StatusBadRequest)
		return
	}
	roleAssignmentScope := input.RoleAssignmentScope
	if roleAssignmentScope == "" {
		roleAssignmentScope = "selected_org"
	}
	if roleAssignmentScope != "selected_org" && roleAssignmentScope != "all_granted_orgs" {
		http.Error(w, "invalid roleAssignmentScope", http.StatusBadRequest)
		return
	}
	session, err := a.requireSession(w, r)
	if err != nil {
		return
	}
	token, err := a.resourceToken(r.Context(), session, audience, roleAssignmentScope)
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

func (a *BrowserAuth) resourceToken(ctx context.Context, session *browserSession, audience, roleAssignmentScope string) (string, error) {
	selectedOrgID := stringValue(session.User.SelectedOrgID)
	if selectedOrgID == "" {
		return "", errors.New("selected organization is required for resource token exchange")
	}
	requestedScopes := []string{
		"openid",
		"profile",
		"email",
		"urn:zitadel:iam:org:id:" + selectedOrgID,
		"urn:zitadel:iam:org:project:id:" + audience + ":aud",
		"urn:zitadel:iam:org:projects:roles",
	}
	if roleAssignmentScope == "selected_org" {
		requestedScopes = append(requestedScopes[:4], append([]string{"urn:zitadel:iam:org:roles:id:" + selectedOrgID}, requestedScopes[4:]...)...)
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
		if err := a.verifyAccessToken(ctx, cached.AccessToken, audience, selectedOrgID, roleAssignmentScope); err == nil {
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
	expiresAt, claims, err := a.verifyAccessTokenClaims(ctx, tokens.AccessToken, audience, selectedOrgID, roleAssignmentScope)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return "", err
	}
	assignments := extractRoleAssignmentsForProject(claims, audience)
	span.SetAttributes(attribute.Int("auth.role_assignment_count", len(assignments)))
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

func (a *BrowserAuth) verifyAccessToken(ctx context.Context, accessToken, audience, selectedOrgID, roleAssignmentScope string) error {
	_, _, err := a.verifyAccessTokenClaims(ctx, accessToken, audience, selectedOrgID, roleAssignmentScope)
	return err
}

func (a *BrowserAuth) verifyAccessTokenClaims(ctx context.Context, accessToken, audience, selectedOrgID, roleAssignmentScope string) (time.Time, map[string]any, error) {
	verifier := a.provider.Verifier(&oidc.Config{ClientID: audience})
	token, err := verifier.Verify(a.oidcContext(ctx), accessToken)
	if err != nil {
		return time.Time{}, nil, err
	}
	var claims map[string]any
	if err := token.Claims(&claims); err != nil {
		return time.Time{}, nil, err
	}
	if err := verifySelectedOrganizationClaims(claims, audience, selectedOrgID, roleAssignmentScope); err != nil {
		return time.Time{}, nil, err
	}
	return token.Expiry, claims, nil
}

func verifySelectedOrganizationClaims(claims map[string]any, audience, selectedOrgID, roleAssignmentScope string) error {
	if asserted, ok := claims["urn:zitadel:iam:org:id"].(string); ok && asserted != "" && asserted != selectedOrgID {
		return errors.New("access token selected organization mismatch")
	}
	assignments := extractRoleAssignmentsForProject(claims, audience)
	if len(assignments) == 0 {
		return errors.New("access token is missing selected organization roles")
	}
	orgIDs := map[string]struct{}{}
	for _, assignment := range assignments {
		orgIDs[assignment.OrgID] = struct{}{}
	}
	if roleAssignmentScope == "all_granted_orgs" {
		if _, ok := orgIDs[selectedOrgID]; !ok {
			return errors.New("access token is missing selected organization role assignment")
		}
		return nil
	}
	if len(orgIDs) != 1 {
		return errors.New("access token carries roles outside the selected organization")
	}
	if _, ok := orgIDs[selectedOrgID]; !ok {
		return errors.New("access token is missing selected organization role assignment")
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
	homeOrgID := stringClaim(claims, "urn:zitadel:iam:user:resourceowner:id")
	assignments := extractRoleAssignments(claims)
	organizations := buildOrganizationContexts(assignments)
	selectedOrgID := selectInitialOrganizationID(organizations, homeOrgID, previousSelectedOrgID)
	roles := rolesForOrganization(organizations, selectedOrgID)
	if len(roles) == 0 {
		roles = extractRoles(claims)
	}
	return browserUser{
		Sub:                    stringValue(stringClaim(idClaims, "sub")),
		Email:                  email,
		Name:                   name,
		PreferredUsername:      preferredUsername,
		HomeOrgID:              homeOrgID,
		SelectedOrgID:          selectedOrgID,
		OrgID:                  selectedOrgID,
		Roles:                  roles,
		RoleAssignments:        assignments,
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
		Roles:                  append([]string(nil), row.Roles...),
		RoleAssignments:        extractRoleAssignments(claims),
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
		Roles:                    user.Roles,
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
	Roles                  []string                  `json:"roles"`
	RoleAssignments        []authRoleAssignment      `json:"roleAssignments"`
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

type authRoleAssignment struct {
	ProjectID *string `json:"projectID"`
	OrgID     string  `json:"orgID"`
	Role      string  `json:"role"`
}

type authOrganizationContext struct {
	OrgID           string               `json:"orgID"`
	Roles           []string             `json:"roles"`
	RoleAssignments []authRoleAssignment `json:"roleAssignments"`
}

type authState struct {
	IsAuthenticated bool                 `json:"isAuthenticated"`
	UserID          *string              `json:"userId"`
	OrgID           *string              `json:"orgId"`
	SelectedOrgID   *string              `json:"selectedOrgId"`
	Roles           []string             `json:"roles"`
	RoleAssignments []authRoleAssignment `json:"roleAssignments"`
	CachePartition  *string              `json:"cachePartition"`
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
				Roles:           []string{},
				RoleAssignments: []authRoleAssignment{},
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
			Roles:           session.User.Roles,
			RoleAssignments: roleAssignmentsForOrganization(session.User.AvailableOrganizations, session.User.SelectedOrgID),
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
			Roles:                  session.User.Roles,
			RoleAssignments:        session.User.RoleAssignments,
			AvailableOrganizations: session.User.AvailableOrganizations,
		},
		Session: &sessionInfo{CreatedAt: session.CreatedAt, ExpiresAt: session.ExpiresAt},
	}
}

func extractRoles(claims map[string]any) []string {
	roles := map[string]struct{}{}
	for key, value := range claims {
		if key != "urn:zitadel:iam:org:project:roles" && (!strings.HasPrefix(key, "urn:zitadel:iam:org:project:") || !strings.HasSuffix(key, ":roles")) {
			continue
		}
		roleMap, ok := value.(map[string]any)
		if !ok {
			continue
		}
		for role := range roleMap {
			roles[role] = struct{}{}
		}
	}
	return sortedKeys(roles)
}

func extractRoleAssignmentsForProject(claims map[string]any, projectID string) []authRoleAssignment {
	assignments := extractRoleAssignments(claims)
	filtered := make([]authRoleAssignment, 0, len(assignments))
	for _, assignment := range assignments {
		if assignment.ProjectID != nil && *assignment.ProjectID == projectID {
			filtered = append(filtered, assignment)
		}
	}
	return filtered
}

func extractRoleAssignments(claims map[string]any) []authRoleAssignment {
	assignments := []authRoleAssignment{}
	for key, value := range claims {
		var projectID *string
		switch {
		case key == "urn:zitadel:iam:org:project:roles":
			projectID = nil
		case strings.HasPrefix(key, "urn:zitadel:iam:org:project:") && strings.HasSuffix(key, ":roles"):
			extracted := strings.TrimSuffix(strings.TrimPrefix(key, "urn:zitadel:iam:org:project:"), ":roles")
			projectID = &extracted
		default:
			continue
		}
		roleMap, ok := value.(map[string]any)
		if !ok {
			continue
		}
		for role, organizations := range roleMap {
			orgMap, ok := organizations.(map[string]any)
			if !ok {
				continue
			}
			for orgID := range orgMap {
				assignments = append(assignments, authRoleAssignment{ProjectID: projectID, OrgID: orgID, Role: role})
			}
		}
	}
	sort.Slice(assignments, func(i, j int) bool {
		leftProject := stringValue(assignments[i].ProjectID)
		rightProject := stringValue(assignments[j].ProjectID)
		if leftProject != rightProject {
			return leftProject < rightProject
		}
		if assignments[i].OrgID != assignments[j].OrgID {
			return assignments[i].OrgID < assignments[j].OrgID
		}
		return assignments[i].Role < assignments[j].Role
	})
	return assignments
}

func buildOrganizationContexts(assignments []authRoleAssignment) []authOrganizationContext {
	type contextParts struct {
		roles       map[string]struct{}
		assignments []authRoleAssignment
	}
	contexts := map[string]*contextParts{}
	for _, assignment := range assignments {
		parts := contexts[assignment.OrgID]
		if parts == nil {
			parts = &contextParts{roles: map[string]struct{}{}}
			contexts[assignment.OrgID] = parts
		}
		parts.roles[assignment.Role] = struct{}{}
		parts.assignments = append(parts.assignments, assignment)
	}
	orgIDs := make([]string, 0, len(contexts))
	for orgID := range contexts {
		orgIDs = append(orgIDs, orgID)
	}
	sort.Strings(orgIDs)
	result := make([]authOrganizationContext, 0, len(orgIDs))
	for _, orgID := range orgIDs {
		parts := contexts[orgID]
		sort.Slice(parts.assignments, func(i, j int) bool {
			leftProject := stringValue(parts.assignments[i].ProjectID)
			rightProject := stringValue(parts.assignments[j].ProjectID)
			if leftProject != rightProject {
				return leftProject < rightProject
			}
			return parts.assignments[i].Role < parts.assignments[j].Role
		})
		result = append(result, authOrganizationContext{OrgID: orgID, Roles: sortedKeys(parts.roles), RoleAssignments: parts.assignments})
	}
	return result
}

func rolesForOrganization(contexts []authOrganizationContext, selectedOrgID *string) []string {
	if selectedOrgID == nil {
		return []string{}
	}
	for _, context := range contexts {
		if context.OrgID == *selectedOrgID {
			return context.Roles
		}
	}
	return []string{}
}

func roleAssignmentsForOrganization(contexts []authOrganizationContext, selectedOrgID *string) []authRoleAssignment {
	if selectedOrgID == nil {
		return []authRoleAssignment{}
	}
	for _, context := range contexts {
		if context.OrgID == *selectedOrgID {
			return context.RoleAssignments
		}
	}
	return []authRoleAssignment{}
}

func selectInitialOrganizationID(contexts []authOrganizationContext, homeOrgID, previousSelectedOrgID *string) *string {
	if previousSelectedOrgID != nil {
		for _, context := range contexts {
			if context.OrgID == *previousSelectedOrgID {
				return previousSelectedOrgID
			}
		}
	}
	if homeOrgID != nil {
		for _, context := range contexts {
			if context.OrgID == *homeOrgID {
				return homeOrgID
			}
		}
	}
	if len(contexts) > 0 {
		selected := contexts[0].OrgID
		return &selected
	}
	return homeOrgID
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

func sortedKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
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
