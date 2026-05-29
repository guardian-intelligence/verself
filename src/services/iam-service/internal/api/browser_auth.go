package api

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
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
	"os"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/oauth2"

	"github.com/verself/iam-service/internal/authz"
	"github.com/verself/iam-service/internal/identity"
	identitystore "github.com/verself/iam-service/internal/store"
	runtimeiam "github.com/verself/service-runtime/iam"
	"github.com/verself/service-runtime/requestmeta"
)

const (
	browserAuthCookieName      = "verself_client"
	browserAuthLoginCookieName = "verself_login"
	browserAuthSessionTTL      = 30 * 24 * time.Hour
	browserAuthLoginTTL        = 5 * time.Minute
	browserAuthRefreshLeeway   = 60 * time.Second
	browserAuthClientPrefix    = "bc_"
	browserAuthAccountPrefix   = "ba_"
	browserAuthCallbackPath    = "/api/v1/auth/callback"
	browserAuthDefaultRedirect = "/"
	browserAuthJSONBodyMax     = 4096
	zitadelResourceOwnerID     = "urn:zitadel:iam:user:resourceowner:id"
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
	ProviderSession ProviderSessionRevoker
	ProviderLogin   ProviderLoginClient
	PasswordReset   PasswordResetNotifier
	// GithubLoginIDPIDPath is the credstore path holding the Zitadel
	// identity-provider id for "Sign in with GitHub". The file is written by
	// auth-control-plane-apply, which may run after this service has started, so
	// the value is resolved lazily rather than read once at boot. Empty path
	// disables the GitHub login routes.
	GithubLoginIDPIDPath string
}

type BrowserAuth struct {
	q                *identitystore.Queries
	store            identity.SQLStore
	logger           *slog.Logger
	provider         *oidc.Provider
	verifier         *oidc.IDTokenVerifier
	oauth            oauth2.Config
	httpClient       *http.Client
	authz            *authz.Service
	providerSessions ProviderSessionRevoker
	providerLogin    ProviderLoginClient
	passwordReset    PasswordResetNotifier
	productAudience  string
	publicBaseURL    *url.URL
	postLogoutURL    string
	tokenVault       browserTokenVault
	// githubLoginIDPIDPath is read lazily; githubLoginIDPID caches the resolved
	// value once present so login does not require a restart after the IdP is
	// provisioned out of band.
	githubLoginIDPIDPath string
	githubLoginIDPMu     sync.Mutex
	githubLoginIDPID     string
}

type browserAuthRequestInfoKey struct{}

type ProviderLoginClient interface {
	CreatePasswordSession(context.Context, identity.LoginSessionInput) (identity.LoginSession, error)
	StartIDPIntent(context.Context, string, string, string) (identity.IDPIntentStart, error)
	RetrieveIDPIntent(context.Context, string, string) (identity.IDPIntentResult, error)
	CreateSessionFromIDPIntent(context.Context, string, string, string, identity.LoginSessionInput) (identity.LoginSession, error)
	GetOIDCAuthRequest(context.Context, string) (identity.OIDCAuthRequest, error)
	FinalizeOIDCAuthRequest(context.Context, string, identity.LoginSession) (string, error)
	StartDeviceAuthorization(context.Context, identity.StartDeviceAuthorizationInput) (identity.DeviceAuthorization, error)
	GetDeviceAuthorizationRequest(context.Context, string) (identity.DeviceAuthorizationRequest, error)
	ApproveDeviceAuthorization(context.Context, string, identity.LoginSession) error
	DenyDeviceAuthorization(context.Context, string) error
	FindPasswordResetUser(context.Context, string) (identity.PasswordResetUser, bool, error)
	StartPasswordReset(context.Context, string) (string, error)
	CompletePasswordReset(context.Context, string, string, string) error
}

type browserAuthRequestInfo struct {
	Attribution requestmeta.Attribution
	UserAgent   string
}

type browserAuthObservationMode uint8

const (
	browserAuthObserveRequest browserAuthObservationMode = iota
	browserAuthObserveNone
)

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
	if cfg.ProviderSession == nil {
		return nil, errors.New("identity browser auth provider session revoker is required")
	}
	if cfg.ProviderLogin == nil {
		return nil, errors.New("identity browser auth provider login client is required")
	}
	if cfg.PasswordReset == nil {
		return nil, errors.New("identity browser auth password reset notifier is required")
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	provider, err := oidc.NewProvider(oidc.ClientContext(ctx, httpClient), cfg.IssuerURL)
	if err != nil {
		return nil, fmt.Errorf("identity browser auth oidc discovery: %w", err)
	}
	tokenVault, err := newBrowserTokenVault(cfg.ClientSecret)
	if err != nil {
		return nil, fmt.Errorf("identity browser auth token vault: %w", err)
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
		httpClient:           httpClient,
		authz:                cfg.Authz,
		providerSessions:     cfg.ProviderSession,
		providerLogin:        cfg.ProviderLogin,
		passwordReset:        cfg.PasswordReset,
		productAudience:      productAudience,
		publicBaseURL:        publicBaseURL,
		postLogoutURL:        postLogoutURL,
		tokenVault:           tokenVault,
		githubLoginIDPIDPath: strings.TrimSpace(cfg.GithubLoginIDPIDPath),
	}, nil
}

// githubLoginIDP resolves the Zitadel GitHub IdP id from credstore on demand.
// auth-control-plane-apply writes this file when it provisions the IdP, possibly
// after this service started, so it is read lazily and cached once present.
func (a *BrowserAuth) githubLoginIDP() string {
	a.githubLoginIDPMu.Lock()
	defer a.githubLoginIDPMu.Unlock()
	if a.githubLoginIDPID != "" {
		return a.githubLoginIDPID
	}
	if a.githubLoginIDPIDPath == "" {
		return ""
	}
	data, err := os.ReadFile(a.githubLoginIDPIDPath)
	if err != nil {
		return ""
	}
	a.githubLoginIDPID = strings.TrimSpace(string(data))
	return a.githubLoginIDPID
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
	mux.HandleFunc("/oauth/v2/device_authorization", auth.handleDeviceAuthorization)
	mux.Handle("/api/v1/auth/", http.StripPrefix("/api/v1/auth", auth))
}

func (a *BrowserAuth) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	attribution := requestmeta.FromHTTP(r)
	ctx := requestmeta.WithAttribution(r.Context(), attribution)
	ctx = context.WithValue(ctx, browserAuthRequestInfoKey{}, browserAuthRequestInfo{
		Attribution: attribution,
		UserAgent:   strings.TrimSpace(r.Header.Get("User-Agent")),
	})
	requestmeta.AnnotateSpan(ctx, attribution)
	r = r.WithContext(ctx)

	switch r.URL.Path {
	case "/password-login":
		a.route(w, r, http.MethodPost, "browser-password-login", rateLimitSignup, browserAuthJSONBodyMax, a.handlePasswordLogin)
	case "/password-reset":
		a.route(w, r, http.MethodPost, "browser-password-reset-start", rateLimitSignup, browserAuthJSONBodyMax, a.handlePasswordResetStart)
	case "/password-reset/complete":
		a.route(w, r, http.MethodPost, "browser-password-reset-complete", rateLimitSignup, browserAuthJSONBodyMax, a.handlePasswordResetComplete)
	case "/idp/github/start":
		a.route(w, r, http.MethodPost, "browser-idp-github-start", rateLimitSignup, browserAuthJSONBodyMax, a.handleGithubLoginStart)
	case "/idp/callback":
		a.route(w, r, http.MethodGet, "browser-idp-callback", rateLimitSignup, 0, a.handleGithubLoginCallback)
	case "/callback":
		a.route(w, r, http.MethodGet, "browser-callback", rateLimitSignup, 0, a.handleCallback)
	case "/session":
		a.route(w, r, http.MethodGet, "browser-session-read", rateLimitRead, 0, a.handleSession)
	case "/sync-session":
		a.route(w, r, http.MethodGet, "browser-sync-session-read", rateLimitRead, 0, a.handleSyncSession)
	case "/organization":
		a.route(w, r, http.MethodPost, "browser-organization-select", rateLimitIAMMutation, browserAuthJSONBodyMax, a.handleOrganization)
	case "/resource-token":
		a.route(w, r, http.MethodPost, "browser-resource-token-create", rateLimitIAMMutation, 0, a.handleResourceToken)
	case "/logout":
		a.route(w, r, http.MethodGet, "browser-logout", rateLimitIAMMutation, 0, a.handleLogout)
	case "/accounts":
		switch r.Method {
		case http.MethodGet:
			a.route(w, r, http.MethodGet, "browser-accounts-list", rateLimitRead, 0, a.handleAccounts)
		case http.MethodPost:
			a.route(w, r, http.MethodPost, "browser-account-select", rateLimitIAMMutation, browserAuthJSONBodyMax, a.handleAccountSelect)
		default:
			w.Header().Set("Allow", "GET, POST")
			a.writeProblem(w, r, http.StatusMethodNotAllowed, problemRequestMethodNotAllowed, "method not allowed")
		}
	case "/sessions":
		a.route(w, r, http.MethodGet, "browser-sessions-list", rateLimitRead, 0, a.handleSessions)
	default:
		if r.URL.Path == "/device-logins/lookup" {
			a.route(w, r, http.MethodPost, "browser-device-login-lookup", rateLimitRead, browserAuthJSONBodyMax, a.handleDeviceLoginLookup)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/device-logins/") {
			switch {
			case strings.HasSuffix(r.URL.Path, "/approval"):
				a.route(w, r, http.MethodPost, "browser-device-login-approve", rateLimitIAMMutation, browserAuthJSONBodyMax, a.handleDeviceLoginApprove)
			case strings.HasSuffix(r.URL.Path, "/denial"):
				a.route(w, r, http.MethodPost, "browser-device-login-deny", rateLimitIAMMutation, browserAuthJSONBodyMax, a.handleDeviceLoginDeny)
			default:
				a.writeProblem(w, r, http.StatusNotFound, problemResourceNotFound, "auth route not found")
			}
			return
		}
		if strings.HasPrefix(r.URL.Path, "/accounts/") {
			a.route(w, r, http.MethodDelete, "browser-account-remove", rateLimitIAMMutation, 0, a.handleAccountRemove)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/sessions/") {
			a.route(w, r, http.MethodDelete, "browser-device-revoke", rateLimitIAMMutation, 0, a.handleSessionRevoke)
			return
		}
		a.writeProblem(w, r, http.StatusNotFound, problemResourceNotFound, "auth route not found")
	}
}

func (a *BrowserAuth) route(w http.ResponseWriter, r *http.Request, method string, operation string, class runtimeiam.RateLimitClass, maxBodyBytes int64, next http.HandlerFunc) {
	if r.Method != method {
		w.Header().Set("Allow", method)
		a.writeProblem(w, r, http.StatusMethodNotAllowed, problemRequestMethodNotAllowed, "method not allowed")
		return
	}
	if !a.allowBrowserAuthRequest(w, r, operation, class) {
		return
	}
	if maxBodyBytes > 0 {
		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	}
	next(w, r)
}

func browserLoginPrompt(value string) string {
	switch strings.TrimSpace(value) {
	case "login":
		return "login"
	case "select_account":
		return "select_account"
	}
	return ""
}

func browserLoginPurpose(value string) string {
	switch strings.TrimSpace(value) {
	case "signup", "invite", "reauth":
		return strings.TrimSpace(value)
	default:
		return "login"
	}
}

func (a *BrowserAuth) enforceLoginConstraints(ctx context.Context, pending identitystore.DeleteBrowserLoginTransactionRow, user browserUser) error {
	if pending.RequiredSubject.Valid && pending.RequiredSubject.String != "" && user.Sub != pending.RequiredSubject.String {
		return errors.New("required subject mismatch")
	}
	if pending.RequiredEmail.Valid && pending.RequiredEmail.String != "" {
		if !sameEmailIdentity(pending.RequiredEmail.String, stringValue(user.Email)) {
			return errors.New("required email mismatch")
		}
	}
	if pending.RequiredOrgID.Valid && pending.RequiredOrgID.String != "" {
		if _, ok := user.organization(pending.RequiredOrgID.String); !ok {
			return errors.New("required organization is unavailable to account")
		}
	}
	return nil
}

func (a *BrowserAuth) recordLoginConstraintFailure(ctx context.Context, pending identitystore.DeleteBrowserLoginTransactionRow, user browserUser, cause error) {
	trace.SpanFromContext(ctx).AddEvent("iam.browser_login.constraint_failed", trace.WithAttributes(
		attribute.String("auth.client_handle", browserClientHandle(pending.ClientHash)),
		attribute.String("auth.login_purpose", pending.Purpose),
		attribute.Bool("auth.required_subject", pending.RequiredSubject.Valid && pending.RequiredSubject.String != ""),
		attribute.Bool("auth.required_email", pending.RequiredEmail.Valid && pending.RequiredEmail.String != ""),
		attribute.Bool("auth.required_org", pending.RequiredOrgID.Valid && pending.RequiredOrgID.String != ""),
		attribute.String("enduser.id", user.Sub),
		attribute.String("error.type", cause.Error()),
	))
	if a.logger != nil {
		a.logger.WarnContext(ctx, "browser auth login constraint failed", "subject", user.Sub, "purpose", pending.Purpose, "error", cause)
	}
}

func (a *BrowserAuth) handleCallback(w http.ResponseWriter, r *http.Request) {
	a.clearLoginCookie(w)
	if oauthErr := r.URL.Query().Get("error"); oauthErr != "" {
		description := r.URL.Query().Get("error_description")
		if description != "" {
			a.writeProblem(w, r, http.StatusBadRequest, problemOIDCCallbackFailed, oauthErr+": "+description)
			return
		}
		a.writeProblem(w, r, http.StatusBadRequest, problemOIDCCallbackFailed, oauthErr)
		return
	}
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	state := strings.TrimSpace(r.URL.Query().Get("state"))
	if code == "" || state == "" {
		a.writeProblem(w, r, http.StatusBadRequest, problemOIDCCallbackFailed, "OIDC callback is missing code or state")
		return
	}
	stateHash := hashToken(state)
	loginStateHash, ok := loginStateHashFromRequest(r)
	if !ok || subtle.ConstantTimeCompare([]byte(loginStateHash), []byte(stateHash)) != 1 {
		a.writeProblem(w, r, http.StatusBadRequest, problemOIDCCallbackFailed, "OIDC callback state did not originate from this browser")
		return
	}
	pending, err := a.q.DeleteBrowserLoginTransaction(r.Context(), identitystore.DeleteBrowserLoginTransactionParams{
		StateHash: stateHash,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		a.writeProblem(w, r, http.StatusBadRequest, problemOIDCCallbackFailed, "OIDC callback state is missing or expired")
		return
	}
	if err != nil {
		a.serverError(w, r, "load login transaction", err)
		return
	}
	clientSecret, ok := browserClientSecretFromRequest(r)
	if !ok || subtle.ConstantTimeCompare([]byte(hashToken(clientSecret)), []byte(pending.ClientHash)) != 1 {
		a.writeProblem(w, r, http.StatusBadRequest, problemOIDCCallbackFailed, "OIDC callback client did not originate from this browser")
		return
	}
	tokens, err := a.exchangeToken(r.Context(), url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {a.oauth.RedirectURL},
		"code_verifier": {pending.CodeVerifier},
	})
	if err != nil {
		a.serverError(w, r, "exchange authorization code", err)
		return
	}
	if strings.TrimSpace(tokens.IDToken) == "" {
		a.writeProblem(w, r, http.StatusBadGateway, problemOIDCCallbackFailed, "OIDC callback returned no id_token")
		return
	}
	verified, err := a.verifier.Verify(a.oidcContext(r.Context()), tokens.IDToken)
	if err != nil {
		a.writeProblem(w, r, http.StatusBadGateway, problemOIDCCallbackFailed, "OIDC callback returned an invalid id_token")
		return
	}
	var idClaims map[string]any
	if err := verified.Claims(&idClaims); err != nil {
		a.serverError(w, r, "decode id_token claims", err)
		return
	}
	if nonce, _ := idClaims["nonce"].(string); nonce != pending.Nonce {
		a.writeProblem(w, r, http.StatusBadRequest, problemOIDCCallbackFailed, "OIDC callback nonce mismatch")
		return
	}
	user, err := a.userSnapshot(r.Context(), tokens, idClaims, nil)
	if err != nil {
		a.serverError(w, r, "build browser auth session", err)
		return
	}
	if err := a.enforceLoginConstraints(r.Context(), pending, user); err != nil {
		a.recordLoginConstraintFailure(r.Context(), pending, user, err)
		a.writeProblem(w, r, http.StatusForbidden, problemAuthConstraintFailed, "login did not match the required account")
		return
	}
	providerSession, err := a.providerSessionFromLoginTransaction(pending)
	if err != nil {
		a.serverError(w, r, "open provider login session", err)
		return
	}
	cachePartition, err := randomToken(24)
	if err != nil {
		a.serverError(w, r, "generate browser cache partition", err)
		return
	}
	accountHandle := browserAccountHandle(pending.ClientHash, user.Sub)
	if err := a.writeAccount(r.Context(), pending.ClientHash, accountHandle, tokens, user, providerSession); err != nil {
		a.serverError(w, r, "persist browser account", err)
		return
	}
	if err := a.q.SetBrowserClientActiveAccount(r.Context(), identitystore.SetBrowserClientActiveAccountParams{
		AccountHandle:        nullableText(accountHandle),
		ClientCachePartition: cachePartition,
		ClientHash:           pending.ClientHash,
	}); err != nil {
		a.serverError(w, r, "select browser account", err)
		return
	}
	trace.SpanFromContext(r.Context()).AddEvent("iam.browser_account.created", trace.WithAttributes(
		attribute.String("auth.account_handle", accountHandle),
		attribute.String("auth.client_handle", browserClientHandle(pending.ClientHash)),
		attribute.String("enduser.id", user.Sub),
	))
	sendBrowserAuthActivity(r.Context(), user, accountHandle, "browserAuth.createAccount", "iam.browser_account.created", "create", "iam.browser_account.create", r.Method, browserAuthExternalRoute(r), http.StatusSeeOther)
	a.setClientCookie(w, clientSecret)
	http.Redirect(w, r, a.absoluteRedirectTarget(pending.RedirectTo), http.StatusSeeOther)
}

func (a *BrowserAuth) handleSession(w http.ResponseWriter, r *http.Request) {
	session, err := a.accountSessionFromRequest(w, r)
	if err != nil {
		a.serverError(w, r, "load browser account", err)
		return
	}
	if session != nil {
		sendBrowserAuthActivity(r.Context(), session.User, session.AccountHandle, "browserAuth.readAccount", "iam.browser_account.read", "read", "iam.browser_account.read", r.Method, browserAuthExternalRoute(r), http.StatusOK)
	}
	a.writeJSON(w, http.StatusOK, snapshotForSession(session))
}

func (a *BrowserAuth) handleSyncSession(w http.ResponseWriter, r *http.Request) {
	client, err := a.browserClientFromRequestMode(w, r, browserAuthObserveNone)
	if err != nil {
		a.serverError(w, r, "validate browser client", err)
		return
	}
	if client == nil {
		a.writeProblem(w, r, http.StatusUnauthorized, problemAuthUnauthenticated, "authentication required")
		return
	}
	if _, err := a.accountSessionForClient(r.Context(), client.ClientHash, browserAuthObserveNone); err != nil {
		a.serverError(w, r, "validate browser account", err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

func (a *BrowserAuth) handleOrganization(w http.ResponseWriter, r *http.Request) {
	if !a.sameOrigin(r) {
		a.writeProblem(w, r, http.StatusForbidden, problemAuthOriginNotAllowed, "origin not allowed")
		return
	}
	var input struct {
		OrgID string `json:"orgID"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			a.writeProblem(w, r, http.StatusRequestEntityTooLarge, problemRequestBodyTooLarge, "request body is too large")
			return
		}
		a.writeProblem(w, r, http.StatusBadRequest, problemRequestValidationFailed, "invalid organization payload")
		return
	}
	orgID := strings.TrimSpace(input.OrgID)
	if orgID == "" {
		a.writeProblem(w, r, http.StatusBadRequest, problemRequestValidationFailed, "orgID is required")
		return
	}
	session, err := a.requireAccount(w, r)
	if err != nil {
		return
	}
	if _, ok := session.User.organization(orgID); !ok {
		organizations, refreshErr := a.publicOrganizationContexts(r.Context(), session.User.Sub)
		if refreshErr != nil {
			a.serverError(w, r, "refresh browser organization contexts", refreshErr)
			return
		}
		session.User.AvailableOrganizations = organizations
		if _, ok := session.User.organization(orgID); !ok {
			a.writeProblem(w, r, http.StatusForbidden, problemAuthOrganizationMissing, "selected organization is not available to this account")
			return
		}
	}
	cachePartition, err := randomToken(24)
	if err != nil {
		a.serverError(w, r, "generate browser cache partition", err)
		return
	}
	if err := a.q.UpdateBrowserAccountOrganization(r.Context(), identitystore.UpdateBrowserAccountOrganizationParams{
		SelectedOrgID: textValue(orgID),
		AccountHandle: session.AccountHandle,
		ClientHash:    session.ClientHash,
	}); err != nil {
		a.serverError(w, r, "update selected organization", err)
		return
	}
	if err := a.q.DeleteBrowserResourceTokens(r.Context(), identitystore.DeleteBrowserResourceTokensParams{AccountHandle: session.AccountHandle}); err != nil {
		a.serverError(w, r, "delete browser resource tokens", err)
		return
	}
	if err := a.q.SetBrowserClientActiveAccount(r.Context(), identitystore.SetBrowserClientActiveAccountParams{
		AccountHandle:        nullableText(session.AccountHandle),
		ClientCachePartition: cachePartition,
		ClientHash:           session.ClientHash,
	}); err != nil {
		a.serverError(w, r, "rotate browser cache partition", err)
		return
	}
	session.User.SelectedOrgID = &orgID
	session.User.OrgID = &orgID
	if err := a.writeAccount(r.Context(), session.ClientHash, session.AccountHandle, tokenResponse{
		AccessToken:  session.AccessToken,
		RefreshToken: session.RefreshToken,
		IDToken:      session.IDToken,
		Scope:        stringValue(session.TokenScope),
		ExpiresAt:    session.ExpiresAt,
	}, session.User, session.providerLoginSession()); err != nil {
		a.serverError(w, r, "persist selected organization context", err)
		return
	}
	next, err := a.readActiveAccount(r.Context(), session.ClientHash)
	if err != nil {
		a.serverError(w, r, "reload browser account", err)
		return
	}
	sendBrowserAuthActivity(r.Context(), next.User, next.AccountHandle, "browserAuth.selectOrganization", "iam.browser_org.selected", "update", "iam.browser_org.select", r.Method, browserAuthExternalRoute(r), http.StatusOK)
	a.writeJSON(w, http.StatusOK, snapshotForSession(next))
}

func (a *BrowserAuth) handleResourceToken(w http.ResponseWriter, r *http.Request) {
	if !a.sameOrigin(r) {
		a.writeProblem(w, r, http.StatusForbidden, problemAuthOriginNotAllowed, "origin not allowed")
		return
	}
	session, err := a.requireAccount(w, r)
	if err != nil {
		return
	}
	token, err := a.resourceToken(r.Context(), session)
	if err != nil {
		a.serverError(w, r, "exchange browser resource token", err)
		return
	}
	trace.SpanFromContext(r.Context()).AddEvent("iam.browser_resource_token.created", trace.WithAttributes(
		attribute.String("auth.account_handle", session.AccountHandle),
		attribute.String("auth.client_handle", session.ClientHandle),
		attribute.String("enduser.id", session.User.Sub),
	))
	sendBrowserAuthActivity(
		r.Context(),
		session.User,
		session.AccountHandle,
		"browserAuth.createResourceToken",
		"iam.browser_resource_token.created",
		"create",
		"iam.browser_resource_token.create",
		r.Method,
		browserAuthExternalRoute(r),
		http.StatusOK,
	)
	a.writeJSON(w, http.StatusOK, map[string]string{"accessToken": token})
}

func (a *BrowserAuth) handleSessions(w http.ResponseWriter, r *http.Request) {
	session, err := a.requireAccount(w, r)
	if err != nil {
		return
	}
	rows, err := a.q.ListBrowserAccountsForClient(r.Context(), identitystore.ListBrowserAccountsForClientParams{ClientHash: session.ClientHash})
	if err != nil {
		a.serverError(w, r, "list browser accounts", err)
		return
	}
	sessions := make([]browserSessionSummary, 0, len(rows))
	for _, row := range rows {
		sessions = append(sessions, browserSessionSummary{
			SessionHandle:          row.AccountHandle,
			IsCurrent:              row.AccountHandle == session.AccountHandle,
			CreatedAt:              requiredTime(row.CreatedAt),
			LastSeenAt:             requiredTime(row.LastSeenAt),
			ExpiresAt:              requiredTime(row.ExpiresAt),
			CreatedClientIP:        row.CreatedClientIp,
			CreatedClientIPTrusted: row.CreatedClientIpTrusted,
			CreatedClientIPSource:  row.CreatedClientIpSource,
			CreatedEdgePeerIP:      row.CreatedEdgePeerIp,
			CreatedUserAgent:       row.CreatedUserAgent,
			CreatedDevice: browserDevice{
				Label:       row.CreatedDeviceLabel,
				Kind:        row.CreatedDeviceKind,
				BrowserName: row.CreatedBrowserName,
				OSName:      row.CreatedOsName,
			},
			CreatedLocation: browserLocation{
				CountryCode: row.CreatedGeoCountryCode,
				Region:      row.CreatedGeoRegion,
				City:        row.CreatedGeoCity,
			},
			ClientIP:        row.LastSeenClientIp,
			ClientIPTrusted: row.LastSeenClientIpTrusted,
			ClientIPSource:  row.LastSeenClientIpSource,
			EdgePeerIP:      row.LastSeenEdgePeerIp,
			UserAgent:       row.LastSeenUserAgent,
			Device: browserDevice{
				Label:       row.LastSeenDeviceLabel,
				Kind:        row.LastSeenDeviceKind,
				BrowserName: row.LastSeenBrowserName,
				OSName:      row.LastSeenOsName,
			},
			Location: browserLocation{
				CountryCode: row.LastSeenGeoCountryCode,
				Region:      row.LastSeenGeoRegion,
				City:        row.LastSeenGeoCity,
			},
		})
	}
	sendBrowserAuthActivity(r.Context(), session.User, session.AccountHandle, "browserAuth.listAccountsAsSessions", "iam.browser_accounts.listed", "read", "iam.browser_account.read", r.Method, browserAuthExternalRoute(r), http.StatusOK)
	a.writeJSON(w, http.StatusOK, browserSessionsResponse{Sessions: sessions})
}

func (a *BrowserAuth) handleAccounts(w http.ResponseWriter, r *http.Request) {
	client, err := a.requireBrowserClient(w, r)
	if err != nil {
		return
	}
	currentAccountHandle := stringValue(stringFromText(client.ActiveAccountHandle))
	accounts, err := a.browserAccountSummaries(r.Context(), client.ClientHash, currentAccountHandle)
	if err != nil {
		a.serverError(w, r, "list browser accounts", err)
		return
	}
	if currentAccountHandle != "" {
		if session, err := a.readActiveAccount(r.Context(), client.ClientHash); err == nil {
			sendBrowserAuthActivity(r.Context(), session.User, session.AccountHandle, "browserAuth.listAccounts", "iam.browser_accounts.listed", "read", "iam.browser_account.read", r.Method, browserAuthExternalRoute(r), http.StatusOK)
		}
	}
	a.writeJSON(w, http.StatusOK, browserAccountsResponse{Accounts: accounts})
}

func (a *BrowserAuth) handleAccountSelect(w http.ResponseWriter, r *http.Request) {
	if !a.sameOrigin(r) {
		a.writeProblem(w, r, http.StatusForbidden, problemAuthOriginNotAllowed, "origin not allowed")
		return
	}
	client, err := a.requireBrowserClient(w, r)
	if err != nil {
		return
	}
	var input struct {
		AccountHandle string `json:"accountHandle"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			a.writeProblem(w, r, http.StatusRequestEntityTooLarge, problemRequestBodyTooLarge, "request body is too large")
			return
		}
		a.writeProblem(w, r, http.StatusBadRequest, problemRequestValidationFailed, "invalid account payload")
		return
	}
	accountHandle := strings.TrimSpace(input.AccountHandle)
	if accountHandle == "" {
		a.writeProblem(w, r, http.StatusBadRequest, problemRequestValidationFailed, "accountHandle is required")
		return
	}
	account, err := a.readAccount(r.Context(), client.ClientHash, accountHandle)
	if errors.Is(err, pgx.ErrNoRows) {
		a.writeProblem(w, r, http.StatusForbidden, problemAuthAccountMissing, "account is not available to this browser")
		return
	}
	if err != nil {
		a.serverError(w, r, "load browser account", err)
		return
	}
	cachePartition, err := randomToken(24)
	if err != nil {
		a.serverError(w, r, "generate browser cache partition", err)
		return
	}
	if err := a.q.SetBrowserClientActiveAccount(r.Context(), identitystore.SetBrowserClientActiveAccountParams{
		AccountHandle:        nullableText(account.AccountHandle),
		ClientCachePartition: cachePartition,
		ClientHash:           client.ClientHash,
	}); err != nil {
		a.serverError(w, r, "select browser account", err)
		return
	}
	if err := a.q.DeleteBrowserResourceTokens(r.Context(), identitystore.DeleteBrowserResourceTokensParams{AccountHandle: account.AccountHandle}); err != nil {
		a.serverError(w, r, "delete browser resource tokens", err)
		return
	}
	next, err := a.readActiveAccount(r.Context(), client.ClientHash)
	if err != nil {
		a.serverError(w, r, "reload browser account", err)
		return
	}
	trace.SpanFromContext(r.Context()).AddEvent("iam.browser_account.selected", trace.WithAttributes(
		attribute.String("auth.account_handle", account.AccountHandle),
		attribute.String("auth.client_handle", client.ClientHandle),
		attribute.String("enduser.id", account.User.Sub),
	))
	sendBrowserAuthActivity(r.Context(), next.User, next.AccountHandle, "browserAuth.selectAccount", "iam.browser_account.selected", "update", "iam.browser_account.select", r.Method, browserAuthExternalRoute(r), http.StatusOK)
	a.writeJSON(w, http.StatusOK, snapshotForSession(next))
}

func (a *BrowserAuth) handleAccountRemove(w http.ResponseWriter, r *http.Request) {
	if !a.sameOrigin(r) {
		a.writeProblem(w, r, http.StatusForbidden, problemAuthOriginNotAllowed, "origin not allowed")
		return
	}
	client, err := a.requireBrowserClient(w, r)
	if err != nil {
		return
	}
	accountHandle := strings.Trim(strings.TrimPrefix(r.URL.Path, "/accounts/"), "/")
	if accountHandle == "" {
		a.writeProblem(w, r, http.StatusBadRequest, problemRequestValidationFailed, "account handle is required")
		return
	}
	removed, err := a.readAccount(r.Context(), client.ClientHash, accountHandle)
	if errors.Is(err, pgx.ErrNoRows) {
		a.writeProblem(w, r, http.StatusForbidden, problemAuthAccountMissing, "account is not available to this browser")
		return
	}
	if err != nil {
		a.serverError(w, r, "load browser account", err)
		return
	}
	if err := a.revokeProviderBrowserSessions(r.Context(), []*browserSession{removed}); err != nil {
		a.serverError(w, r, "revoke browser account provider session", err)
		return
	}
	if err := a.q.DeleteBrowserResourceTokens(r.Context(), identitystore.DeleteBrowserResourceTokensParams{AccountHandle: removed.AccountHandle}); err != nil {
		a.serverError(w, r, "delete browser account resource tokens", err)
		return
	}
	if err := a.q.DeleteBrowserAccountByHandle(r.Context(), identitystore.DeleteBrowserAccountByHandleParams{ClientHash: client.ClientHash, AccountHandle: accountHandle}); err != nil {
		a.serverError(w, r, "remove browser account", err)
		return
	}
	activeAccountHandle := stringValue(stringFromText(client.ActiveAccountHandle))
	if activeAccountHandle == accountHandle {
		if err := a.selectNextBrowserAccountAfterRemoval(r.Context(), client.ClientHash); err != nil {
			a.serverError(w, r, "select next browser account", err)
			return
		}
	}
	trace.SpanFromContext(r.Context()).AddEvent("iam.browser_account.removed", trace.WithAttributes(
		attribute.String("auth.account_handle", accountHandle),
		attribute.String("auth.client_handle", client.ClientHandle),
	))
	sendBrowserAuthActivity(r.Context(), removed.User, accountHandle, "browserAuth.removeAccount", "iam.browser_account.removed", "delete", "iam.browser_account.delete", r.Method, browserAuthExternalRoute(r), http.StatusNoContent)
	w.WriteHeader(http.StatusNoContent)
}

func (a *BrowserAuth) handleSessionRevoke(w http.ResponseWriter, r *http.Request) {
	if !a.sameOrigin(r) {
		a.writeProblem(w, r, http.StatusForbidden, problemAuthOriginNotAllowed, "origin not allowed")
		return
	}
	client, err := a.requireBrowserClient(w, r)
	if err != nil {
		return
	}
	sessionHandle := strings.Trim(strings.TrimPrefix(r.URL.Path, "/sessions/"), "/")
	if sessionHandle == "" {
		a.writeProblem(w, r, http.StatusBadRequest, problemRequestValidationFailed, "session handle is required")
		return
	}
	removed, err := a.readAccount(r.Context(), client.ClientHash, sessionHandle)
	if errors.Is(err, pgx.ErrNoRows) {
		a.writeProblem(w, r, http.StatusForbidden, problemAuthSessionMissing, "session is not available to this browser")
		return
	}
	if err != nil {
		a.serverError(w, r, "load browser account", err)
		return
	}
	sessions, err := a.revokeBrowserClient(r.Context(), client.ClientHash, client.ClientHandle, client.ClientCachePartition)
	if err != nil {
		a.serverError(w, r, "revoke browser device", err)
		return
	}
	a.clearClientCookie(w)
	a.clearLoginCookie(w)
	trace.SpanFromContext(r.Context()).AddEvent("iam.browser_client.revoked", trace.WithAttributes(
		attribute.String("auth.account_handle", sessionHandle),
		attribute.String("auth.client_handle", client.ClientHandle),
		attribute.Int("auth.account_count", len(sessions)),
	))
	sendBrowserAuthActivity(r.Context(), removed.User, sessionHandle, "browserAuth.revokeDevice", "iam.browser_client.revoked", "delete", "iam.browser_client.delete", r.Method, browserAuthExternalRoute(r), http.StatusNoContent)
	w.WriteHeader(http.StatusNoContent)
}

func (a *BrowserAuth) handleLogout(w http.ResponseWriter, r *http.Request) {
	clientSecret, ok := browserClientSecretFromRequest(r)
	var logoutSessions []*browserSession
	if ok {
		clientHash := hashToken(clientSecret)
		clientHandle := browserClientHandle(clientHash)
		cachePartition := ""
		if client, err := a.q.GetBrowserClient(r.Context(), identitystore.GetBrowserClientParams{ClientHash: clientHash}); err == nil {
			clientHandle = client.ClientHandle
			cachePartition = client.ClientCachePartition
		} else if !errors.Is(err, pgx.ErrNoRows) {
			a.serverError(w, r, "load browser client", err)
			return
		}
		sessions, err := a.revokeBrowserClient(r.Context(), clientHash, clientHandle, cachePartition)
		if err != nil {
			a.serverError(w, r, "revoke browser device", err)
			return
		}
		logoutSessions = sessions
	}
	a.clearClientCookie(w)
	a.clearLoginCookie(w)
	for _, session := range logoutSessions {
		sendBrowserAuthActivity(r.Context(), session.User, session.AccountHandle, "browserAuth.logout", "iam.browser_client.logged_out", "delete", "iam.browser_client.delete", r.Method, browserAuthExternalRoute(r), http.StatusSeeOther)
	}
	http.Redirect(w, r, a.postLogoutURL, http.StatusSeeOther)
}

func (a *BrowserAuth) ensureBrowserClient(w http.ResponseWriter, r *http.Request) (identitystore.IamBrowserClient, string, error) {
	if clientSecret, ok := browserClientSecretFromRequest(r); ok {
		clientHash := hashToken(clientSecret)
		client, err := a.q.GetBrowserClient(r.Context(), identitystore.GetBrowserClientParams{ClientHash: clientHash})
		if err == nil {
			if err := a.touchBrowserClientSeen(r.Context(), clientHash); err != nil {
				return identitystore.IamBrowserClient{}, "", err
			}
			a.setClientCookie(w, clientSecret)
			return client, clientSecret, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return identitystore.IamBrowserClient{}, "", err
		}
	}

	clientSecret, err := randomToken(32)
	if err != nil {
		return identitystore.IamBrowserClient{}, "", err
	}
	clientHash := hashToken(clientSecret)
	cachePartition, err := randomToken(24)
	if err != nil {
		return identitystore.IamBrowserClient{}, "", err
	}
	observation := browserSessionObservationFromContext(r.Context())
	if err := a.q.UpsertBrowserClient(r.Context(), identitystore.UpsertBrowserClientParams{
		ClientHash:           clientHash,
		ClientHandle:         browserClientHandle(clientHash),
		ClientCachePartition: cachePartition,
		ExpiresAt:            timestamptz(time.Now().UTC().Add(browserAuthSessionTTL)),
		ClientIp:             observation.ClientIP,
		ClientIpTrusted:      observation.ClientIPTrusted,
		ClientIpSource:       observation.ClientIPSource,
		EdgePeerIp:           observation.EdgePeerIP,
		UserAgent:            observation.UserAgent,
		DeviceLabel:          observation.Device.Label,
		DeviceKind:           observation.Device.Kind,
		BrowserName:          observation.Device.BrowserName,
		OsName:               observation.Device.OSName,
		GeoCountryCode:       observation.Location.CountryCode,
		GeoRegion:            observation.Location.Region,
		GeoCity:              observation.Location.City,
	}); err != nil {
		return identitystore.IamBrowserClient{}, "", err
	}
	client, err := a.q.GetBrowserClient(r.Context(), identitystore.GetBrowserClientParams{ClientHash: clientHash})
	if err != nil {
		return identitystore.IamBrowserClient{}, "", err
	}
	trace.SpanFromContext(r.Context()).AddEvent("iam.browser_client.created", trace.WithAttributes(
		attribute.String("auth.client_handle", client.ClientHandle),
		attribute.String("verself.request.client_ip_source", observation.ClientIPSource),
		attribute.String("auth.device_label", observation.Device.Label),
	))
	return client, clientSecret, nil
}

func (a *BrowserAuth) touchBrowserClientSeen(ctx context.Context, clientHash string) error {
	observation := browserSessionObservationFromContext(ctx)
	return a.q.TouchBrowserClientSeen(ctx, identitystore.TouchBrowserClientSeenParams{
		ClientHash:      clientHash,
		ClientIp:        observation.ClientIP,
		ClientIpTrusted: observation.ClientIPTrusted,
		ClientIpSource:  observation.ClientIPSource,
		EdgePeerIp:      observation.EdgePeerIP,
		UserAgent:       observation.UserAgent,
		DeviceLabel:     observation.Device.Label,
		DeviceKind:      observation.Device.Kind,
		BrowserName:     observation.Device.BrowserName,
		OsName:          observation.Device.OSName,
		GeoCountryCode:  observation.Location.CountryCode,
		GeoRegion:       observation.Location.Region,
		GeoCity:         observation.Location.City,
	})
}

func (a *BrowserAuth) browserClientFromRequest(w http.ResponseWriter, r *http.Request) (*identitystore.IamBrowserClient, error) {
	return a.browserClientFromRequestMode(w, r, browserAuthObserveRequest)
}

func (a *BrowserAuth) browserClientFromRequestMode(w http.ResponseWriter, r *http.Request, mode browserAuthObservationMode) (*identitystore.IamBrowserClient, error) {
	clientSecret, ok := browserClientSecretFromRequest(r)
	if !ok {
		return nil, nil
	}
	clientHash := hashToken(clientSecret)
	if mode == browserAuthObserveRequest {
		if err := a.touchBrowserClientSeen(r.Context(), clientHash); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
	}
	client, err := a.q.GetBrowserClient(r.Context(), identitystore.GetBrowserClientParams{ClientHash: clientHash})
	if errors.Is(err, pgx.ErrNoRows) {
		a.clearClientCookie(w)
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &client, nil
}

func (a *BrowserAuth) requireBrowserClient(w http.ResponseWriter, r *http.Request) (*identitystore.IamBrowserClient, error) {
	client, err := a.browserClientFromRequest(w, r)
	if err != nil {
		a.serverError(w, r, "load browser client", err)
		return nil, err
	}
	if client == nil {
		a.writeProblem(w, r, http.StatusUnauthorized, problemAuthUnauthenticated, "authentication required")
		return nil, errors.New("authentication required")
	}
	return client, nil
}

func (a *BrowserAuth) revokeBrowserClient(ctx context.Context, clientHash, clientHandle, cachePartition string) ([]*browserSession, error) {
	sessions, err := a.browserAccountSessionsForClient(ctx, clientHash, clientHandle, cachePartition)
	if err != nil {
		return nil, err
	}
	if err := a.revokeProviderBrowserSessions(ctx, sessions); err != nil {
		return nil, err
	}
	if err := a.q.DeleteBrowserClient(ctx, identitystore.DeleteBrowserClientParams{ClientHash: clientHash}); err != nil {
		return nil, err
	}
	return sessions, nil
}

func (a *BrowserAuth) selectNextBrowserAccountAfterRemoval(ctx context.Context, clientHash string) error {
	cachePartition, err := randomToken(24)
	if err != nil {
		return err
	}
	rows, err := a.q.ListBrowserAccountsForClient(ctx, identitystore.ListBrowserAccountsForClientParams{ClientHash: clientHash})
	if err != nil {
		return err
	}
	accountHandle := pgtype.Text{}
	if len(rows) > 0 {
		accountHandle = textValue(rows[0].AccountHandle)
	}
	return a.q.SetBrowserClientActiveAccount(ctx, identitystore.SetBrowserClientActiveAccountParams{
		AccountHandle:        accountHandle,
		ClientCachePartition: cachePartition,
		ClientHash:           clientHash,
	})
}

func (a *BrowserAuth) browserAccountSessionsForClient(ctx context.Context, clientHash, clientHandle, cachePartition string) ([]*browserSession, error) {
	rows, err := a.q.ListBrowserAccountsForClient(ctx, identitystore.ListBrowserAccountsForClientParams{ClientHash: clientHash})
	if err != nil {
		return nil, err
	}
	if clientHandle == "" {
		clientHandle = browserClientHandle(clientHash)
	}
	sessions := make([]*browserSession, 0, len(rows))
	for _, row := range rows {
		session, err := a.readAccountForClient(ctx, clientHash, clientHandle, cachePartition, row.AccountHandle)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}
	return sessions, nil
}

func (a *BrowserAuth) revokeProviderBrowserSessions(ctx context.Context, sessions []*browserSession) error {
	if len(sessions) == 0 {
		return nil
	}
	if a.providerSessions == nil {
		return errors.New("provider session revoker is required")
	}
	seen := map[string]struct{}{}
	for _, session := range sessions {
		sessionID, err := browserProviderSessionID(session)
		if err != nil {
			return err
		}
		if _, ok := seen[sessionID]; ok {
			continue
		}
		seen[sessionID] = struct{}{}
		if err := a.providerSessions.DeleteSession(ctx, sessionID); err != nil {
			return err
		}
		trace.SpanFromContext(ctx).AddEvent("iam.provider_session.revoked", trace.WithAttributes(
			attribute.String("auth.provider_session_hash", hashToken(sessionID)),
			attribute.String("auth.client_handle", session.ClientHandle),
			attribute.String("auth.account_handle", session.AccountHandle),
			attribute.String("enduser.id", session.User.Sub),
		))
	}
	return nil
}

func browserProviderSessionID(session *browserSession) (string, error) {
	if session == nil {
		return "", errors.New("browser account session is required")
	}
	if sessionID := strings.TrimSpace(session.ProviderSessionID); sessionID != "" {
		return sessionID, nil
	}
	if sessionID := providerSessionIDFromClaims(session.User.Claims); sessionID != "" {
		return sessionID, nil
	}
	if strings.TrimSpace(session.IDToken) != "" {
		claims, err := decodeJWTPayload(session.IDToken)
		if err != nil {
			return "", fmt.Errorf("decode browser id_token provider session id: %w", err)
		}
		if sessionID := providerSessionIDFromClaims(claims); sessionID != "" {
			return sessionID, nil
		}
	}
	return "", errors.New("browser account is missing provider session id")
}

func (a *BrowserAuth) accountSessionFromRequest(w http.ResponseWriter, r *http.Request) (*browserSession, error) {
	client, err := a.browserClientFromRequest(w, r)
	if err != nil {
		return nil, err
	}
	if client == nil {
		return nil, nil
	}
	return a.accountSessionForClient(r.Context(), client.ClientHash, browserAuthObserveRequest)
}

func (a *BrowserAuth) accountSessionForClient(ctx context.Context, clientHash string, mode browserAuthObservationMode) (*browserSession, error) {
	session, err := a.readActiveAccount(ctx, clientHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if time.Until(session.ExpiresAt) > browserAuthRefreshLeeway {
		if mode == browserAuthObserveRequest {
			if err := a.touchAccountSeen(ctx, session); err != nil {
				return nil, err
			}
		}
		return session, nil
	}
	refreshed, err := a.refreshSession(ctx, session)
	if err != nil {
		if a.logger != nil {
			a.logger.WarnContext(ctx, "browser auth token refresh failed", "error", err, "subject", session.User.Sub)
		}
		if err := a.revokeProviderBrowserSessions(ctx, []*browserSession{session}); err != nil {
			return nil, err
		}
		if err := a.q.DeleteBrowserAccountByHandle(ctx, identitystore.DeleteBrowserAccountByHandleParams{ClientHash: session.ClientHash, AccountHandle: session.AccountHandle}); err != nil {
			return nil, err
		}
		return nil, nil
	}
	return refreshed, nil
}

func (a *BrowserAuth) requireAccount(w http.ResponseWriter, r *http.Request) (*browserSession, error) {
	session, err := a.accountSessionFromRequest(w, r)
	if err != nil {
		a.serverError(w, r, "load browser account", err)
		return nil, err
	}
	if session == nil {
		a.writeProblem(w, r, http.StatusUnauthorized, problemAuthUnauthenticated, "authentication required")
		return nil, errors.New("authentication required")
	}
	return session, nil
}

func (a *BrowserAuth) refreshSession(ctx context.Context, session *browserSession) (*browserSession, error) {
	if session.RefreshToken == "" {
		return nil, errors.New("browser account has no refresh token")
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
		cachePartition, err = randomToken(24)
		if err != nil {
			return nil, err
		}
		if err := a.q.DeleteBrowserResourceTokens(ctx, identitystore.DeleteBrowserResourceTokensParams{AccountHandle: session.AccountHandle}); err != nil {
			return nil, err
		}
	}
	if err := a.writeAccount(ctx, session.ClientHash, session.AccountHandle, tokens, user, session.providerLoginSession()); err != nil {
		return nil, err
	}
	if err := a.q.SetBrowserClientActiveAccount(ctx, identitystore.SetBrowserClientActiveAccountParams{
		AccountHandle:        nullableText(session.AccountHandle),
		ClientCachePartition: cachePartition,
		ClientHash:           session.ClientHash,
	}); err != nil {
		return nil, err
	}
	trace.SpanFromContext(ctx).AddEvent("iam.browser_account.refreshed", trace.WithAttributes(
		attribute.String("auth.account_handle", session.AccountHandle),
		attribute.String("auth.client_handle", session.ClientHandle),
		attribute.String("enduser.id", user.Sub),
	))
	return a.readActiveAccount(ctx, session.ClientHash)
}

func (a *BrowserAuth) resourceToken(ctx context.Context, session *browserSession) (string, error) {
	selectedOrgID := stringValue(session.User.SelectedOrgID)
	audience := strings.TrimSpace(a.productAudience)
	if audience == "" {
		return "", errors.New("product audience is required for resource token exchange")
	}
	if selectedOrgID == "" {
		expiresAt, _, err := a.verifyAccessTokenClaims(ctx, session.AccessToken, audience, "")
		if err != nil {
			return "", fmt.Errorf("verify no-organization resource token: %w", err)
		}
		if time.Until(expiresAt) <= browserAuthRefreshLeeway {
			return "", errors.New("no-organization resource token expires too soon")
		}
		trace.SpanFromContext(ctx).AddEvent("identity.browser_auth.resource_token.no_organization", trace.WithAttributes(
			attribute.String("auth.audience", audience),
			attribute.String("enduser.id", session.User.Sub),
		))
		return session.AccessToken, nil
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
		AccountHandle: session.AccountHandle,
		Audience:      audience,
		SelectedOrgID: selectedOrgID,
		ScopeHash:     scopeHash,
	})
	if err == nil && time.Until(requiredTime(cached.ExpiresAt)) > browserAuthRefreshLeeway {
		cachedAccessToken, decryptErr := a.tokenVault.open(cached.AccessTokenCiphertext, session.AccountHandle, "resource_access")
		if decryptErr == nil && a.verifyAccessToken(ctx, cachedAccessToken, audience, providerOrgID) == nil {
			span.SetAttributes(attribute.Bool("auth.cache_hit", true))
			span.SetStatus(codes.Ok, "")
			return cachedAccessToken, nil
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
	resourceAccessToken, err := a.tokenVault.seal(tokens.AccessToken, session.AccountHandle, "resource_access")
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return "", err
	}
	if err := a.q.UpsertBrowserResourceToken(ctx, identitystore.UpsertBrowserResourceTokenParams{
		AccountHandle:         session.AccountHandle,
		Audience:              audience,
		SelectedOrgID:         selectedOrgID,
		ScopeHash:             scopeHash,
		AccessTokenCiphertext: resourceAccessToken,
		TokenScope:            nullableText(tokens.Scope),
		ExpiresAt:             timestamptz(expiresAt),
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
	if strings.TrimSpace(selectedOrgID) == "" {
		return nil
	}
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
	organizations, err := a.publicOrganizationContexts(ctx, sub)
	if err != nil {
		return browserUser{}, err
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

func (a *BrowserAuth) readActiveAccount(ctx context.Context, clientHash string) (*browserSession, error) {
	row, err := a.q.GetActiveBrowserAccount(ctx, identitystore.GetActiveBrowserAccountParams{ClientHash: clientHash})
	if err != nil {
		return nil, err
	}
	return a.browserSessionFromAccountRecord(browserAccountRecordFromActive(row))
}

func (a *BrowserAuth) readAccount(ctx context.Context, clientHash, accountHandle string) (*browserSession, error) {
	client, err := a.q.GetBrowserClient(ctx, identitystore.GetBrowserClientParams{ClientHash: clientHash})
	if err != nil {
		return nil, err
	}
	return a.readAccountForClient(ctx, clientHash, client.ClientHandle, client.ClientCachePartition, accountHandle)
}

func (a *BrowserAuth) readAccountForClient(ctx context.Context, clientHash, clientHandle, cachePartition, accountHandle string) (*browserSession, error) {
	row, err := a.q.GetBrowserAccount(ctx, identitystore.GetBrowserAccountParams{ClientHash: clientHash, AccountHandle: accountHandle})
	if err != nil {
		return nil, err
	}
	return a.browserSessionFromAccountRecord(browserAccountRecordFromAccount(clientHandle, cachePartition, row))
}

func (a *BrowserAuth) browserSessionFromAccountRecord(row browserAccountRecord) (*browserSession, error) {
	var organizations []authOrganizationContext
	if err := json.Unmarshal([]byte(row.AvailableOrgContextsJson), &organizations); err != nil {
		return nil, err
	}
	var claims map[string]any
	if err := json.Unmarshal([]byte(row.UserClaimsJson), &claims); err != nil {
		return nil, err
	}
	accessToken, err := a.tokenVault.open(row.AccessTokenCiphertext, row.AccountHandle, "access")
	if err != nil {
		return nil, fmt.Errorf("open browser account access token: %w", err)
	}
	refreshToken, err := a.tokenVault.openNullable(row.RefreshTokenCiphertext, row.AccountHandle, "refresh")
	if err != nil {
		return nil, fmt.Errorf("open browser account refresh token: %w", err)
	}
	idToken, err := a.tokenVault.openNullable(row.IDTokenCiphertext, row.AccountHandle, "id")
	if err != nil {
		return nil, fmt.Errorf("open browser account id token: %w", err)
	}
	providerSessionToken, err := a.tokenVault.openNullable(textValue(row.ProviderSessionTokenCiphertext), row.AccountHandle, "provider_session")
	if err != nil {
		return nil, fmt.Errorf("open browser account provider session token: %w", err)
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
		ClientHash:             row.ClientHash,
		ClientHandle:           row.ClientHandle,
		AccountHandle:          row.AccountHandle,
		ClientCachePartition:   row.ClientCachePartition,
		AccessToken:            accessToken,
		RefreshToken:           refreshToken,
		IDToken:                idToken,
		ProviderSessionID:      row.ProviderSessionID,
		ProviderSessionToken:   providerSessionToken,
		TokenScope:             stringFromText(row.TokenScope),
		ExpiresAt:              requiredTime(row.ExpiresAt),
		CreatedClientIP:        row.CreatedClientIp,
		CreatedIPTrusted:       row.CreatedClientIpTrusted,
		CreatedClientIPSource:  row.CreatedClientIpSource,
		CreatedEdgePeerIP:      row.CreatedEdgePeerIp,
		CreatedUserAgent:       row.CreatedUserAgent,
		CreatedDevice:          browserDevice{Label: row.CreatedDeviceLabel, Kind: row.CreatedDeviceKind, BrowserName: row.CreatedBrowserName, OSName: row.CreatedOsName},
		CreatedLocation:        browserLocation{CountryCode: row.CreatedGeoCountryCode, Region: row.CreatedGeoRegion, City: row.CreatedGeoCity},
		LastSeenClientIP:       row.LastSeenClientIp,
		LastSeenIPTrusted:      row.LastSeenClientIpTrusted,
		LastSeenClientIPSource: row.LastSeenClientIpSource,
		LastSeenEdgePeerIP:     row.LastSeenEdgePeerIp,
		LastSeenUserAgent:      row.LastSeenUserAgent,
		LastSeenDevice:         browserDevice{Label: row.LastSeenDeviceLabel, Kind: row.LastSeenDeviceKind, BrowserName: row.LastSeenBrowserName, OSName: row.LastSeenOsName},
		LastSeenLocation:       browserLocation{CountryCode: row.LastSeenGeoCountryCode, Region: row.LastSeenGeoRegion, City: row.LastSeenGeoCity},
		LastSeenAt:             requiredTime(row.LastSeenAt),
		CreatedAt:              requiredTime(row.CreatedAt),
		UpdatedAt:              requiredTime(row.UpdatedAt),
		User:                   user,
	}, nil
}

func (a *BrowserAuth) providerSessionFromLoginTransaction(pending identitystore.DeleteBrowserLoginTransactionRow) (identity.LoginSession, error) {
	sessionID := strings.TrimSpace(pending.ProviderSessionID)
	ciphertext := strings.TrimSpace(pending.ProviderSessionTokenCiphertext)
	if sessionID == "" && ciphertext == "" {
		return identity.LoginSession{}, nil
	}
	if sessionID == "" || ciphertext == "" {
		return identity.LoginSession{}, errors.New("provider login session is incomplete")
	}
	token, err := a.tokenVault.open(ciphertext, pending.StateHash, "provider_session")
	if err != nil {
		return identity.LoginSession{}, err
	}
	return identity.LoginSession{SessionID: sessionID, SessionToken: token}, nil
}

func (a *BrowserAuth) writeAccount(ctx context.Context, clientHash, accountHandle string, tokens tokenResponse, user browserUser, providerSession identity.LoginSession) error {
	organizations, err := json.Marshal(user.AvailableOrganizations)
	if err != nil {
		return err
	}
	claims, err := json.Marshal(user.Claims)
	if err != nil {
		return err
	}
	accessToken, err := a.tokenVault.seal(tokens.AccessToken, accountHandle, "access")
	if err != nil {
		return err
	}
	idToken, err := a.tokenVault.sealOptional(tokens.IDToken, accountHandle, "id")
	if err != nil {
		return err
	}
	refreshToken, err := a.tokenVault.sealOptional(tokens.RefreshToken, accountHandle, "refresh")
	if err != nil {
		return err
	}
	providerSessionToken, err := a.tokenVault.sealOptional(providerSession.SessionToken, accountHandle, "provider_session")
	if err != nil {
		return err
	}
	observation := browserSessionObservationFromContext(ctx)
	if err := a.q.UpsertBrowserAccount(ctx, identitystore.UpsertBrowserAccountParams{
		AccountHandle:                  accountHandle,
		ClientHash:                     clientHash,
		State:                          "active",
		Subject:                        user.Sub,
		Email:                          nullableTextPtr(user.Email),
		DisplayName:                    nullableTextPtr(user.Name),
		PreferredUsername:              nullableTextPtr(user.PreferredUsername),
		OrgID:                          nullableTextPtr(user.OrgID),
		HomeOrgID:                      nullableTextPtr(user.HomeOrgID),
		SelectedOrgID:                  nullableTextPtr(user.SelectedOrgID),
		AvailableOrgContextsJson:       organizations,
		UserClaimsJson:                 claims,
		IDTokenCiphertext:              nullableText(idToken),
		AccessTokenCiphertext:          accessToken,
		RefreshTokenCiphertext:         nullableText(refreshToken),
		ProviderSessionID:              strings.TrimSpace(providerSession.SessionID),
		ProviderSessionTokenCiphertext: providerSessionToken,
		TokenScope:                     nullableText(tokens.Scope),
		ExpiresAt:                      timestamptz(tokens.ExpiresAt),
		ClientIp:                       observation.ClientIP,
		ClientIpTrusted:                observation.ClientIPTrusted,
		ClientIpSource:                 observation.ClientIPSource,
		EdgePeerIp:                     observation.EdgePeerIP,
		UserAgent:                      observation.UserAgent,
		DeviceLabel:                    observation.Device.Label,
		DeviceKind:                     observation.Device.Kind,
		BrowserName:                    observation.Device.BrowserName,
		OsName:                         observation.Device.OSName,
		GeoCountryCode:                 observation.Location.CountryCode,
		GeoRegion:                      observation.Location.Region,
		GeoCity:                        observation.Location.City,
	}); err != nil {
		return err
	}
	return a.insertAccountObservation(ctx, clientHash, accountHandle, user.Sub, observation)
}

func (a *BrowserAuth) insertAccountObservation(ctx context.Context, clientHash, accountHandle, subject string, observation browserSessionObservation) error {
	return a.q.InsertBrowserAccountObservation(ctx, identitystore.InsertBrowserAccountObservationParams{
		ClientHash:      clientHash,
		AccountHandle:   accountHandle,
		Subject:         subject,
		ClientIp:        observation.ClientIP,
		ClientIpTrusted: observation.ClientIPTrusted,
		ClientIpSource:  observation.ClientIPSource,
		EdgePeerIp:      observation.EdgePeerIP,
		UserAgent:       observation.UserAgent,
		DeviceLabel:     observation.Device.Label,
		DeviceKind:      observation.Device.Kind,
		BrowserName:     observation.Device.BrowserName,
		OsName:          observation.Device.OSName,
		GeoCountryCode:  observation.Location.CountryCode,
		GeoRegion:       observation.Location.Region,
		GeoCity:         observation.Location.City,
	})
}

func (a *BrowserAuth) touchAccountSeen(ctx context.Context, session *browserSession) error {
	if session == nil {
		return nil
	}
	observation := browserSessionObservationFromContext(ctx)
	seenAt := time.Now().UTC()
	if err := a.q.UpdateBrowserAccountSeen(ctx, identitystore.UpdateBrowserAccountSeenParams{
		AccountHandle:   session.AccountHandle,
		ClientHash:      session.ClientHash,
		ClientIp:        observation.ClientIP,
		ClientIpTrusted: observation.ClientIPTrusted,
		ClientIpSource:  observation.ClientIPSource,
		EdgePeerIp:      observation.EdgePeerIP,
		UserAgent:       observation.UserAgent,
		DeviceLabel:     observation.Device.Label,
		DeviceKind:      observation.Device.Kind,
		BrowserName:     observation.Device.BrowserName,
		OsName:          observation.Device.OSName,
		GeoCountryCode:  observation.Location.CountryCode,
		GeoRegion:       observation.Location.Region,
		GeoCity:         observation.Location.City,
	}); err != nil {
		return err
	}
	if err := a.insertAccountObservation(ctx, session.ClientHash, session.AccountHandle, session.User.Sub, observation); err != nil {
		return err
	}
	session.LastSeenClientIP = observation.ClientIP
	session.LastSeenIPTrusted = observation.ClientIPTrusted
	session.LastSeenClientIPSource = observation.ClientIPSource
	session.LastSeenEdgePeerIP = observation.EdgePeerIP
	session.LastSeenUserAgent = observation.UserAgent
	session.LastSeenDevice = observation.Device
	session.LastSeenLocation = observation.Location
	session.LastSeenAt = seenAt
	trace.SpanFromContext(ctx).AddEvent("iam.browser_account.seen", trace.WithAttributes(
		attribute.String("auth.account_handle", session.AccountHandle),
		attribute.String("auth.client_handle", session.ClientHandle),
		attribute.String("enduser.id", session.User.Sub),
		attribute.String("verself.request.client_ip_source", observation.ClientIPSource),
		attribute.String("auth.device_label", observation.Device.Label),
	))
	return nil
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

func (a *BrowserAuth) allowBrowserAuthRequest(w http.ResponseWriter, r *http.Request, operation string, class runtimeiam.RateLimitClass) bool {
	key := browserAuthRateLimitKey(r, operation, class)
	decision := apiOperationRateLimiter.Allow(class, key, time.Now())
	if decision.Allowed {
		return true
	}
	if decision.RetryAfter > 0 {
		w.Header().Set("Retry-After", fmt.Sprintf("%.0f", decision.RetryAfter.Seconds()))
	}
	a.writeProblem(w, r, http.StatusTooManyRequests, problemQuotaRateLimited, "rate limit exceeded")
	return false
}

func browserAuthRateLimitKey(r *http.Request, operation string, class runtimeiam.RateLimitClass) string {
	principal := ""
	if clientSecret, ok := browserClientSecretFromRequest(r); ok {
		principal = "client:" + hashToken(clientSecret)
	} else if info := browserRequestInfoFromContext(r.Context()); strings.TrimSpace(info.Attribution.ClientIP) != "" {
		principal = "ip:" + info.Attribution.ClientIP
	} else {
		principal = "ip:" + r.RemoteAddr
	}
	return strings.Join([]string{"browser-auth", string(class), operation, principal}, ":")
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

func (a *BrowserAuth) serverError(w http.ResponseWriter, r *http.Request, operation string, err error) {
	if a.logger != nil {
		a.logger.Error("browser auth failed", "operation", operation, "error", err)
	}
	a.writeCatalogProblem(w, r, problemServiceUnavailable, withAuthProblemCause(err))
}

func (a *BrowserAuth) setClientCookie(w http.ResponseWriter, clientSecret string) {
	http.SetCookie(w, &http.Cookie{
		Name:     browserAuthCookieName,
		Value:    clientSecret,
		Path:     "/",
		Expires:  time.Now().UTC().Add(browserAuthSessionTTL),
		MaxAge:   int(browserAuthSessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
}

func (a *BrowserAuth) clearClientCookie(w http.ResponseWriter) {
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

func browserClientSecretFromRequest(r *http.Request) (string, bool) {
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
	ClientHash             string
	ClientHandle           string
	AccountHandle          string
	ClientCachePartition   string
	AccessToken            string
	RefreshToken           string
	IDToken                string
	ProviderSessionID      string
	ProviderSessionToken   string
	TokenScope             *string
	ExpiresAt              time.Time
	CreatedClientIP        string
	CreatedIPTrusted       bool
	CreatedClientIPSource  string
	CreatedEdgePeerIP      string
	CreatedUserAgent       string
	CreatedDevice          browserDevice
	CreatedLocation        browserLocation
	LastSeenClientIP       string
	LastSeenIPTrusted      bool
	LastSeenClientIPSource string
	LastSeenEdgePeerIP     string
	LastSeenUserAgent      string
	LastSeenDevice         browserDevice
	LastSeenLocation       browserLocation
	LastSeenAt             time.Time
	CreatedAt              time.Time
	UpdatedAt              time.Time
	User                   browserUser
}

func (s *browserSession) providerLoginSession() identity.LoginSession {
	if s == nil {
		return identity.LoginSession{}
	}
	return identity.LoginSession{
		SessionID:    strings.TrimSpace(s.ProviderSessionID),
		SessionToken: strings.TrimSpace(s.ProviderSessionToken),
	}
}

type browserAccountRecord struct {
	ClientHandle                   string
	ClientCachePartition           string
	AccountHandle                  string
	ClientHash                     string
	State                          string
	Subject                        string
	Email                          pgtype.Text
	DisplayName                    pgtype.Text
	PreferredUsername              pgtype.Text
	OrgID                          pgtype.Text
	HomeOrgID                      pgtype.Text
	SelectedOrgID                  pgtype.Text
	AvailableOrgContextsJson       string
	UserClaimsJson                 string
	IDTokenCiphertext              pgtype.Text
	AccessTokenCiphertext          string
	RefreshTokenCiphertext         pgtype.Text
	ProviderSessionID              string
	ProviderSessionTokenCiphertext string
	TokenScope                     pgtype.Text
	ExpiresAt                      pgtype.Timestamptz
	CreatedClientIp                string
	CreatedClientIpTrusted         bool
	CreatedClientIpSource          string
	CreatedEdgePeerIp              string
	CreatedUserAgent               string
	CreatedDeviceLabel             string
	CreatedDeviceKind              string
	CreatedBrowserName             string
	CreatedOsName                  string
	CreatedGeoCountryCode          string
	CreatedGeoRegion               string
	CreatedGeoCity                 string
	LastSeenClientIp               string
	LastSeenClientIpTrusted        bool
	LastSeenClientIpSource         string
	LastSeenEdgePeerIp             string
	LastSeenUserAgent              string
	LastSeenDeviceLabel            string
	LastSeenDeviceKind             string
	LastSeenBrowserName            string
	LastSeenOsName                 string
	LastSeenGeoCountryCode         string
	LastSeenGeoRegion              string
	LastSeenGeoCity                string
	LastSeenAt                     pgtype.Timestamptz
	CreatedAt                      pgtype.Timestamptz
	UpdatedAt                      pgtype.Timestamptz
}

func browserAccountRecordFromActive(row identitystore.GetActiveBrowserAccountRow) browserAccountRecord {
	return browserAccountRecord{
		ClientHandle:                   row.ClientHandle,
		ClientCachePartition:           row.ClientCachePartition,
		AccountHandle:                  row.AccountHandle,
		ClientHash:                     row.ClientHash,
		State:                          row.State,
		Subject:                        row.Subject,
		Email:                          row.Email,
		DisplayName:                    row.DisplayName,
		PreferredUsername:              row.PreferredUsername,
		OrgID:                          row.OrgID,
		HomeOrgID:                      row.HomeOrgID,
		SelectedOrgID:                  row.SelectedOrgID,
		AvailableOrgContextsJson:       row.AvailableOrgContextsJson,
		UserClaimsJson:                 row.UserClaimsJson,
		IDTokenCiphertext:              row.IDTokenCiphertext,
		AccessTokenCiphertext:          row.AccessTokenCiphertext,
		RefreshTokenCiphertext:         row.RefreshTokenCiphertext,
		ProviderSessionID:              row.ProviderSessionID,
		ProviderSessionTokenCiphertext: row.ProviderSessionTokenCiphertext,
		TokenScope:                     row.TokenScope,
		ExpiresAt:                      row.ExpiresAt,
		CreatedClientIp:                row.CreatedClientIp,
		CreatedClientIpTrusted:         row.CreatedClientIpTrusted,
		CreatedClientIpSource:          row.CreatedClientIpSource,
		CreatedEdgePeerIp:              row.CreatedEdgePeerIp,
		CreatedUserAgent:               row.CreatedUserAgent,
		CreatedDeviceLabel:             row.CreatedDeviceLabel,
		CreatedDeviceKind:              row.CreatedDeviceKind,
		CreatedBrowserName:             row.CreatedBrowserName,
		CreatedOsName:                  row.CreatedOsName,
		CreatedGeoCountryCode:          row.CreatedGeoCountryCode,
		CreatedGeoRegion:               row.CreatedGeoRegion,
		CreatedGeoCity:                 row.CreatedGeoCity,
		LastSeenClientIp:               row.LastSeenClientIp,
		LastSeenClientIpTrusted:        row.LastSeenClientIpTrusted,
		LastSeenClientIpSource:         row.LastSeenClientIpSource,
		LastSeenEdgePeerIp:             row.LastSeenEdgePeerIp,
		LastSeenUserAgent:              row.LastSeenUserAgent,
		LastSeenDeviceLabel:            row.LastSeenDeviceLabel,
		LastSeenDeviceKind:             row.LastSeenDeviceKind,
		LastSeenBrowserName:            row.LastSeenBrowserName,
		LastSeenOsName:                 row.LastSeenOsName,
		LastSeenGeoCountryCode:         row.LastSeenGeoCountryCode,
		LastSeenGeoRegion:              row.LastSeenGeoRegion,
		LastSeenGeoCity:                row.LastSeenGeoCity,
		LastSeenAt:                     row.LastSeenAt,
		CreatedAt:                      row.CreatedAt,
		UpdatedAt:                      row.UpdatedAt,
	}
}

func browserAccountRecordFromAccount(clientHandle, cachePartition string, row identitystore.GetBrowserAccountRow) browserAccountRecord {
	return browserAccountRecord{
		ClientHandle:                   clientHandle,
		ClientCachePartition:           cachePartition,
		AccountHandle:                  row.AccountHandle,
		ClientHash:                     row.ClientHash,
		State:                          row.State,
		Subject:                        row.Subject,
		Email:                          row.Email,
		DisplayName:                    row.DisplayName,
		PreferredUsername:              row.PreferredUsername,
		OrgID:                          row.OrgID,
		HomeOrgID:                      row.HomeOrgID,
		SelectedOrgID:                  row.SelectedOrgID,
		AvailableOrgContextsJson:       row.AvailableOrgContextsJson,
		UserClaimsJson:                 row.UserClaimsJson,
		IDTokenCiphertext:              row.IDTokenCiphertext,
		AccessTokenCiphertext:          row.AccessTokenCiphertext,
		RefreshTokenCiphertext:         row.RefreshTokenCiphertext,
		ProviderSessionID:              row.ProviderSessionID,
		ProviderSessionTokenCiphertext: row.ProviderSessionTokenCiphertext,
		TokenScope:                     row.TokenScope,
		ExpiresAt:                      row.ExpiresAt,
		CreatedClientIp:                row.CreatedClientIp,
		CreatedClientIpTrusted:         row.CreatedClientIpTrusted,
		CreatedClientIpSource:          row.CreatedClientIpSource,
		CreatedEdgePeerIp:              row.CreatedEdgePeerIp,
		CreatedUserAgent:               row.CreatedUserAgent,
		CreatedDeviceLabel:             row.CreatedDeviceLabel,
		CreatedDeviceKind:              row.CreatedDeviceKind,
		CreatedBrowserName:             row.CreatedBrowserName,
		CreatedOsName:                  row.CreatedOsName,
		CreatedGeoCountryCode:          row.CreatedGeoCountryCode,
		CreatedGeoRegion:               row.CreatedGeoRegion,
		CreatedGeoCity:                 row.CreatedGeoCity,
		LastSeenClientIp:               row.LastSeenClientIp,
		LastSeenClientIpTrusted:        row.LastSeenClientIpTrusted,
		LastSeenClientIpSource:         row.LastSeenClientIpSource,
		LastSeenEdgePeerIp:             row.LastSeenEdgePeerIp,
		LastSeenUserAgent:              row.LastSeenUserAgent,
		LastSeenDeviceLabel:            row.LastSeenDeviceLabel,
		LastSeenDeviceKind:             row.LastSeenDeviceKind,
		LastSeenBrowserName:            row.LastSeenBrowserName,
		LastSeenOsName:                 row.LastSeenOsName,
		LastSeenGeoCountryCode:         row.LastSeenGeoCountryCode,
		LastSeenGeoRegion:              row.LastSeenGeoRegion,
		LastSeenGeoCity:                row.LastSeenGeoCity,
		LastSeenAt:                     row.LastSeenAt,
		CreatedAt:                      row.CreatedAt,
		UpdatedAt:                      row.UpdatedAt,
	}
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
	ClientHandle    *string `json:"clientHandle"`
	AccountHandle   *string `json:"accountHandle"`
}

type sessionInfo struct {
	SessionHandle   string          `json:"sessionHandle"`
	ClientHandle    string          `json:"clientHandle"`
	AccountHandle   string          `json:"accountHandle"`
	CreatedAt       time.Time       `json:"createdAt"`
	LastSeenAt      time.Time       `json:"lastSeenAt"`
	ExpiresAt       time.Time       `json:"expiresAt"`
	AuthMethods     []string        `json:"authMethods"`
	ClientIP        string          `json:"clientIP"`
	ClientIPTrusted bool            `json:"clientIPTrusted"`
	ClientIPSource  string          `json:"clientIPSource"`
	EdgePeerIP      string          `json:"edgePeerIP"`
	UserAgent       string          `json:"userAgent"`
	Device          browserDevice   `json:"device"`
	Location        browserLocation `json:"location"`
}

type authSnapshot struct {
	IsSignedIn bool         `json:"isSignedIn"`
	Auth       authState    `json:"auth"`
	User       *browserUser `json:"user"`
	Session    *sessionInfo `json:"session"`
}

type browserSessionSummary struct {
	SessionHandle          string          `json:"sessionHandle"`
	IsCurrent              bool            `json:"isCurrent"`
	CreatedAt              time.Time       `json:"createdAt"`
	LastSeenAt             time.Time       `json:"lastSeenAt"`
	ExpiresAt              time.Time       `json:"expiresAt"`
	CreatedClientIP        string          `json:"createdClientIP"`
	CreatedClientIPTrusted bool            `json:"createdClientIPTrusted"`
	CreatedClientIPSource  string          `json:"createdClientIPSource"`
	CreatedEdgePeerIP      string          `json:"createdEdgePeerIP"`
	CreatedUserAgent       string          `json:"createdUserAgent"`
	CreatedDevice          browserDevice   `json:"createdDevice"`
	CreatedLocation        browserLocation `json:"createdLocation"`
	ClientIP               string          `json:"clientIP"`
	ClientIPTrusted        bool            `json:"clientIPTrusted"`
	ClientIPSource         string          `json:"clientIPSource"`
	EdgePeerIP             string          `json:"edgePeerIP"`
	UserAgent              string          `json:"userAgent"`
	Device                 browserDevice   `json:"device"`
	Location               browserLocation `json:"location"`
}

type browserSessionsResponse struct {
	Sessions []browserSessionSummary `json:"sessions"`
}

type browserAccountSummary struct {
	AccountHandle           string                    `json:"accountHandle"`
	IsCurrent               bool                      `json:"isCurrent"`
	Subject                 string                    `json:"subject"`
	Email                   *string                   `json:"email"`
	DisplayName             *string                   `json:"displayName"`
	PreferredUsername       *string                   `json:"preferredUsername"`
	HomeOrgID               *string                   `json:"homeOrgID"`
	SelectedOrgID           *string                   `json:"selectedOrgID"`
	AvailableOrganizations  []authOrganizationContext `json:"availableOrganizations"`
	CreatedAt               time.Time                 `json:"createdAt"`
	LastSeenAt              time.Time                 `json:"lastSeenAt"`
	ExpiresAt               time.Time                 `json:"expiresAt"`
	CreatedClientIP         string                    `json:"createdClientIP"`
	CreatedClientIPTrusted  bool                      `json:"createdClientIPTrusted"`
	CreatedClientIPSource   string                    `json:"createdClientIPSource"`
	CreatedEdgePeerIP       string                    `json:"createdEdgePeerIP"`
	CreatedUserAgent        string                    `json:"createdUserAgent"`
	CreatedDevice           browserDevice             `json:"createdDevice"`
	CreatedLocation         browserLocation           `json:"createdLocation"`
	LastSeenClientIP        string                    `json:"lastSeenClientIP"`
	LastSeenClientIPTrusted bool                      `json:"lastSeenClientIPTrusted"`
	LastSeenClientIPSource  string                    `json:"lastSeenClientIPSource"`
	LastSeenEdgePeerIP      string                    `json:"lastSeenEdgePeerIP"`
	LastSeenUserAgent       string                    `json:"lastSeenUserAgent"`
	LastSeenDevice          browserDevice             `json:"lastSeenDevice"`
	LastSeenLocation        browserLocation           `json:"lastSeenLocation"`
}

type browserAccountsResponse struct {
	Accounts []browserAccountSummary `json:"accounts"`
}

func snapshotForSession(session *browserSession) authSnapshot {
	if session == nil {
		return authSnapshot{
			IsSignedIn: false,
			Auth: authState{
				IsAuthenticated: false,
				ClientHandle:    nil,
				AccountHandle:   nil,
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
			ClientHandle:    &session.ClientHandle,
			AccountHandle:   &session.AccountHandle,
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
		Session: &sessionInfo{
			SessionHandle:   session.AccountHandle,
			ClientHandle:    session.ClientHandle,
			AccountHandle:   session.AccountHandle,
			CreatedAt:       session.CreatedAt,
			LastSeenAt:      session.LastSeenAt,
			ExpiresAt:       session.ExpiresAt,
			AuthMethods:     authMethodsFromClaims(session.User.Claims),
			ClientIP:        session.LastSeenClientIP,
			ClientIPTrusted: session.LastSeenIPTrusted,
			ClientIPSource:  session.LastSeenClientIPSource,
			EdgePeerIP:      session.LastSeenEdgePeerIP,
			UserAgent:       session.LastSeenUserAgent,
			Device:          session.LastSeenDevice,
			Location:        session.LastSeenLocation,
		},
	}
}

func (a *BrowserAuth) browserAccountSummaries(ctx context.Context, clientHash, currentAccountHandle string) ([]browserAccountSummary, error) {
	rows, err := a.q.ListBrowserAccountsForClient(ctx, identitystore.ListBrowserAccountsForClientParams{ClientHash: clientHash})
	if err != nil {
		return nil, err
	}
	accounts := make([]browserAccountSummary, 0, len(rows))
	for _, row := range rows {
		var organizations []authOrganizationContext
		if err := json.Unmarshal([]byte(row.AvailableOrgContextsJson), &organizations); err != nil {
			return nil, err
		}
		accounts = append(accounts, browserAccountSummary{
			AccountHandle:           row.AccountHandle,
			IsCurrent:               row.AccountHandle == currentAccountHandle,
			Subject:                 row.Subject,
			Email:                   stringFromText(row.Email),
			DisplayName:             stringFromText(row.DisplayName),
			PreferredUsername:       stringFromText(row.PreferredUsername),
			HomeOrgID:               stringFromText(row.HomeOrgID),
			SelectedOrgID:           stringFromText(row.SelectedOrgID),
			AvailableOrganizations:  organizations,
			CreatedAt:               requiredTime(row.CreatedAt),
			LastSeenAt:              requiredTime(row.LastSeenAt),
			ExpiresAt:               requiredTime(row.ExpiresAt),
			CreatedClientIP:         row.CreatedClientIp,
			CreatedClientIPTrusted:  row.CreatedClientIpTrusted,
			CreatedClientIPSource:   row.CreatedClientIpSource,
			CreatedEdgePeerIP:       row.CreatedEdgePeerIp,
			CreatedUserAgent:        row.CreatedUserAgent,
			CreatedDevice:           browserDevice{Label: row.CreatedDeviceLabel, Kind: row.CreatedDeviceKind, BrowserName: row.CreatedBrowserName, OSName: row.CreatedOsName},
			CreatedLocation:         browserLocation{CountryCode: row.CreatedGeoCountryCode, Region: row.CreatedGeoRegion, City: row.CreatedGeoCity},
			LastSeenClientIP:        row.LastSeenClientIp,
			LastSeenClientIPTrusted: row.LastSeenClientIpTrusted,
			LastSeenClientIPSource:  row.LastSeenClientIpSource,
			LastSeenEdgePeerIP:      row.LastSeenEdgePeerIp,
			LastSeenUserAgent:       row.LastSeenUserAgent,
			LastSeenDevice:          browserDevice{Label: row.LastSeenDeviceLabel, Kind: row.LastSeenDeviceKind, BrowserName: row.LastSeenBrowserName, OSName: row.LastSeenOsName},
			LastSeenLocation:        browserLocation{CountryCode: row.LastSeenGeoCountryCode, Region: row.LastSeenGeoRegion, City: row.LastSeenGeoCity},
		})
	}
	return accounts, nil
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
	out, missing := browserOrganizationContextsFromMetadata(orgIDs, rows)
	if len(missing) > 0 {
		trace.SpanFromContext(ctx).AddEvent("iam.browser_auth.organization_metadata_missing", trace.WithAttributes(
			attribute.Int("iam.organization.missing_count", len(missing)),
		))
		if a.logger != nil {
			a.logger.WarnContext(ctx, "browser auth organization metadata missing", "missing_org_ids", missing)
		}
	}
	return out, nil
}

func browserOrganizationContextsFromMetadata(orgIDs []string, rows []identitystore.ListOrganizationMetadataByOrgIDsRow) ([]authOrganizationContext, []string) {
	seen := map[string]struct{}{}
	out := make([]authOrganizationContext, 0, len(rows))
	for _, row := range rows {
		seen[row.OrgID] = struct{}{}
		out = append(out, authOrganizationContext{OrgID: row.OrgID, IdentityProviderOrgID: row.IdentityProviderOrgID})
	}
	return out, missingOrganizationMetadataIDs(orgIDs, seen)
}

func missingOrganizationMetadataIDs(orgIDs []string, seen map[string]struct{}) []string {
	missing := []string{}
	for _, orgID := range orgIDs {
		if _, ok := seen[orgID]; !ok {
			missing = append(missing, orgID)
		}
	}
	return missing
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

func hashToken(value string) string {
	sum := sha256.Sum256([]byte(value))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func browserClientHandle(clientHash string) string {
	return browserHandle(browserAuthClientPrefix, clientHash)
}

func browserAccountHandle(clientHash, subject string) string {
	return browserHandle(browserAuthAccountPrefix, clientHash, subject)
}

func browserHandle(prefix string, parts ...string) string {
	hash := sha256.New()
	_, _ = hash.Write([]byte("verself browser handle v1"))
	for _, part := range parts {
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(part))
	}
	return prefix + base64.RawURLEncoding.EncodeToString(hash.Sum(nil))[:32]
}

func normalizeEmailHint(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func sameEmailIdentity(required, actual string) bool {
	requiredEmail, err := identity.ParseEmailAddress(required)
	if err != nil {
		return false
	}
	actualEmail, err := identity.ParseEmailAddress(actual)
	if err != nil {
		return false
	}
	if len(requiredEmail.IdentityKey) != len(actualEmail.IdentityKey) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(requiredEmail.IdentityKey), []byte(actualEmail.IdentityKey)) == 1
}

type browserTokenVault struct {
	aead cipher.AEAD
}

func newBrowserTokenVault(secret string) (browserTokenVault, error) {
	key := sha256.Sum256([]byte("verself browser token vault v1\x00" + secret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return browserTokenVault{}, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return browserTokenVault{}, err
	}
	return browserTokenVault{aead: aead}, nil
}

func (v browserTokenVault) sealOptional(plaintext, accountHandle, kind string) (string, error) {
	if strings.TrimSpace(plaintext) == "" {
		return "", nil
	}
	return v.seal(plaintext, accountHandle, kind)
}

func (v browserTokenVault) seal(plaintext, accountHandle, kind string) (string, error) {
	if strings.TrimSpace(plaintext) == "" {
		return "", errors.New("browser token vault cannot seal an empty token")
	}
	nonce := make([]byte, v.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	ciphertext := v.aead.Seal(nil, nonce, []byte(plaintext), browserTokenVaultAAD(accountHandle, kind))
	payload := make([]byte, 0, len(nonce)+len(ciphertext))
	payload = append(payload, nonce...)
	payload = append(payload, ciphertext...)
	return "v1." + base64.RawURLEncoding.EncodeToString(payload), nil
}

func (v browserTokenVault) openNullable(value pgtype.Text, accountHandle, kind string) (string, error) {
	if !value.Valid || strings.TrimSpace(value.String) == "" {
		return "", nil
	}
	return v.open(value.String, accountHandle, kind)
}

func (v browserTokenVault) open(encoded, accountHandle, kind string) (string, error) {
	payload, ok := strings.CutPrefix(encoded, "v1.")
	if !ok {
		return "", errors.New("unsupported browser token ciphertext version")
	}
	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return "", err
	}
	nonceSize := v.aead.NonceSize()
	if len(raw) <= nonceSize {
		return "", errors.New("browser token ciphertext is too short")
	}
	plaintext, err := v.aead.Open(nil, raw[:nonceSize], raw[nonceSize:], browserTokenVaultAAD(accountHandle, kind))
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

func browserTokenVaultAAD(accountHandle, kind string) []byte {
	return []byte("verself browser token vault aad v1\x00" + accountHandle + "\x00" + kind)
}

func browserRequestInfoFromContext(ctx context.Context) browserAuthRequestInfo {
	info, _ := ctx.Value(browserAuthRequestInfoKey{}).(browserAuthRequestInfo)
	return info
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
