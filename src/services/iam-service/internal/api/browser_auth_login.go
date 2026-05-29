package api

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/oauth2"

	"github.com/verself/iam-service/internal/identity"
	identitystore "github.com/verself/iam-service/internal/store"
	"github.com/verself/service-runtime/requestmeta"
)

type passwordLoginRequest struct {
	Email           string `json:"email"`
	Password        string `json:"password"`
	AuthRequestID   string `json:"authRequestId"`
	RedirectTo      string `json:"redirectTo"`
	Purpose         string `json:"purpose"`
	LoginHint       string `json:"loginHint"`
	RequiredSubject string `json:"requiredSubject"`
	RequiredEmail   string `json:"requiredEmail"`
	RequiredOrgID   string `json:"requiredOrgId"`
	Prompt          string `json:"prompt"`
}

type passwordLoginResponse struct {
	CallbackURL string `json:"callbackUrl"`
}

type passwordResetStartRequest struct {
	Email string `json:"email"`
}

type passwordResetCompleteRequest struct {
	UserID           string `json:"userId"`
	VerificationCode string `json:"verificationCode"`
	Password         string `json:"password"`
}

type passwordResetResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

type deviceLoginLookupRequest struct {
	UserCode string `json:"userCode"`
}

type deviceAuthorizationResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int64  `json:"expires_in"`
	Interval                int64  `json:"interval,omitempty"`
}

type deviceLoginResponse struct {
	DeviceLoginHandle string   `json:"deviceLoginHandle"`
	UserCodeSuffix    string   `json:"userCodeSuffix"`
	ClientID          string   `json:"clientId"`
	AppName           string   `json:"appName"`
	ProjectName       string   `json:"projectName"`
	Scopes            []string `json:"scopes"`
	State             string   `json:"state"`
	ApprovedAt        string   `json:"approvedAt"`
	DeniedAt          string   `json:"deniedAt"`
	ExpiresAt         string   `json:"expiresAt"`
	UpdatedAt         string   `json:"updatedAt"`
}

type deviceLoginRecord struct {
	DeviceLoginHandle             string
	UserCodeSuffix                string
	ProviderDeviceAuthorizationID string
	ClientID                      string
	AppName                       string
	ProjectName                   string
	ScopesJSON                    string
	State                         string
	ApprovedAt                    time.Time
	DeniedAt                      time.Time
	ExpiresAt                     time.Time
	UpdatedAt                     time.Time
}

func (a *BrowserAuth) handleDeviceAuthorization(w http.ResponseWriter, r *http.Request) {
	attribution := requestmeta.FromHTTP(r)
	ctx := requestmeta.WithAttribution(r.Context(), attribution)
	ctx = context.WithValue(ctx, browserAuthRequestInfoKey{}, browserAuthRequestInfo{
		Attribution: attribution,
		UserAgent:   strings.TrimSpace(r.Header.Get("User-Agent")),
	})
	requestmeta.AnnotateSpan(ctx, attribution)
	r = r.WithContext(ctx)
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		a.writeProblem(w, r, http.StatusMethodNotAllowed, problemRequestMethodNotAllowed, "method not allowed")
		return
	}
	if !a.allowBrowserAuthRequest(w, r, "oauth-device-authorization-start", rateLimitSignup) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, browserAuthJSONBodyMax)
	if err := r.ParseForm(); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			a.writeProblem(w, r, http.StatusRequestEntityTooLarge, problemRequestBodyTooLarge, "request body is too large")
			return
		}
		a.writeProblem(w, r, http.StatusBadRequest, problemRequestValidationFailed, "invalid device authorization payload")
		return
	}
	clientID := strings.TrimSpace(r.PostForm.Get("client_id"))
	scopes := compactAuthScopes(strings.Fields(r.PostForm.Get("scope")))
	device, err := a.providerLogin.StartDeviceAuthorization(r.Context(), identity.StartDeviceAuthorizationInput{
		ClientID: clientID,
		Scopes:   scopes,
	})
	if err != nil {
		if identity.IsInvalid(err) {
			a.writeProblem(w, r, http.StatusBadRequest, problemRequestValidationFailed, "invalid device authorization request")
			return
		}
		a.serverError(w, r, "start provider device authorization", err)
		return
	}
	if _, err := a.upsertDeviceLoginIntent(r.Context(), device.UserCode, device.Request, time.Now().UTC().Add(device.ExpiresIn)); err != nil {
		a.serverError(w, r, "persist device authorization", err)
		return
	}
	verificationURI, verificationURIComplete := a.deviceVerificationURLs(device.UserCode)
	a.writeJSON(w, http.StatusOK, deviceAuthorizationResponse{
		DeviceCode:              device.DeviceCode,
		UserCode:                device.UserCode,
		VerificationURI:         verificationURI,
		VerificationURIComplete: verificationURIComplete,
		ExpiresIn:               int64(device.ExpiresIn / time.Second),
		Interval:                int64(device.Interval / time.Second),
	})
}

func (a *BrowserAuth) handlePasswordLogin(w http.ResponseWriter, r *http.Request) {
	if !a.sameOrigin(r) {
		a.writeProblem(w, r, http.StatusForbidden, problemAuthOriginNotAllowed, "origin not allowed")
		return
	}
	var input passwordLoginRequest
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			a.writeProblem(w, r, http.StatusRequestEntityTooLarge, problemRequestBodyTooLarge, "request body is too large")
			return
		}
		a.writeProblem(w, r, http.StatusBadRequest, problemRequestValidationFailed, "invalid login payload")
		return
	}
	email := normalizeEmailHint(firstNonEmpty(input.Email, input.LoginHint))
	if email == "" || input.Password == "" {
		a.writeProblem(w, r, http.StatusBadRequest, problemRequestValidationFailed, "email and password are required")
		return
	}
	incomingAuthRequestID := strings.TrimSpace(input.AuthRequestID)
	session, err := a.providerLogin.CreatePasswordSession(r.Context(), identity.LoginSessionInput{
		LoginName: email,
		Password:  input.Password,
		UserAgent: strings.TrimSpace(r.Header.Get("User-Agent")),
		IP:        requestmetaIP(r.Context()),
		Lifetime:  browserAuthSessionTTL,
	})
	if err != nil {
		if errors.Is(err, identity.ErrInvalidCredentials) {
			a.writeProblem(w, r, http.StatusUnauthorized, problemAuthInvalidCredentials, "Email or password is incorrect.")
			return
		}
		a.serverError(w, r, "create provider password session", err)
		return
	}
	// A successful password authentication is the linking authority for a pending
	// step-up GitHub link (the email match only selected the candidate account).
	if err := a.linkGithubFromChallenge(r, w, email); err != nil {
		_ = a.providerSessions.DeleteSession(context.WithoutCancel(r.Context()), session.SessionID)
		a.serverError(w, r, "link github from challenge", err)
		return
	}
	if incomingAuthRequestID != "" {
		if _, err := a.providerLogin.GetOIDCAuthRequest(r.Context(), incomingAuthRequestID); err != nil {
			_ = a.providerSessions.DeleteSession(context.WithoutCancel(r.Context()), session.SessionID)
			a.serverError(w, r, "load incoming provider auth request", err)
			return
		}
		callbackURL, err := a.providerLogin.FinalizeOIDCAuthRequest(r.Context(), incomingAuthRequestID, session)
		if err != nil {
			_ = a.providerSessions.DeleteSession(context.WithoutCancel(r.Context()), session.SessionID)
			a.serverError(w, r, "finalize incoming provider auth request", err)
			return
		}
		a.writeJSON(w, http.StatusOK, passwordLoginResponse{CallbackURL: callbackURL})
		return
	}
	client, clientSecret, err := a.ensureBrowserClient(w, r)
	if err != nil {
		_ = a.providerSessions.DeleteSession(context.WithoutCancel(r.Context()), session.SessionID)
		a.serverError(w, r, "ensure browser client", err)
		return
	}
	state, nonce, verifier, err := newOIDCLoginSecrets()
	if err != nil {
		_ = a.providerSessions.DeleteSession(context.WithoutCancel(r.Context()), session.SessionID)
		a.serverError(w, r, "generate oidc login state", err)
		return
	}
	authRequestID, err := a.startProviderOIDCAuthRequest(r.Context(), state, nonce, verifier, passwordLoginOIDCOptions(input, email))
	if err != nil {
		_ = a.providerSessions.DeleteSession(context.WithoutCancel(r.Context()), session.SessionID)
		a.serverError(w, r, "start provider auth request", err)
		return
	}
	stateHash := hashToken(state)
	providerSessionToken, err := a.tokenVault.seal(session.SessionToken, stateHash, "provider_session")
	if err != nil {
		_ = a.providerSessions.DeleteSession(context.WithoutCancel(r.Context()), session.SessionID)
		a.serverError(w, r, "seal provider session token", err)
		return
	}
	redirectTo := a.sanitizeRedirectTarget(input.RedirectTo)
	if err := a.q.InsertBrowserLoginTransaction(r.Context(), identitystore.InsertBrowserLoginTransactionParams{
		StateHash:                      stateHash,
		ClientHash:                     client.ClientHash,
		Nonce:                          nonce,
		CodeVerifier:                   verifier,
		RedirectTo:                     redirectTo,
		Purpose:                        browserLoginPurpose(input.Purpose),
		LoginHint:                      nullableText(email),
		RequiredSubject:                nullableText(strings.TrimSpace(input.RequiredSubject)),
		RequiredEmail:                  nullableText(normalizeEmailHint(input.RequiredEmail)),
		RequiredOrgID:                  nullableText(strings.TrimSpace(input.RequiredOrgID)),
		ProviderSessionID:              session.SessionID,
		ProviderSessionTokenCiphertext: providerSessionToken,
		ExpiresAt:                      timestamptz(time.Now().UTC().Add(browserAuthLoginTTL)),
	}); err != nil {
		_ = a.providerSessions.DeleteSession(context.WithoutCancel(r.Context()), session.SessionID)
		a.serverError(w, r, "persist login transaction", err)
		return
	}
	callbackURL, err := a.providerLogin.FinalizeOIDCAuthRequest(r.Context(), authRequestID, session)
	if err != nil {
		_, _ = a.q.DeleteBrowserLoginTransaction(context.WithoutCancel(r.Context()), identitystore.DeleteBrowserLoginTransactionParams{StateHash: stateHash})
		_ = a.providerSessions.DeleteSession(context.WithoutCancel(r.Context()), session.SessionID)
		a.serverError(w, r, "finalize provider auth request", err)
		return
	}
	a.setLoginCookie(w, stateHash)
	a.setClientCookie(w, clientSecret)
	a.writeJSON(w, http.StatusOK, passwordLoginResponse{CallbackURL: callbackURL})
}

func (a *BrowserAuth) handlePasswordResetStart(w http.ResponseWriter, r *http.Request) {
	if !a.sameOrigin(r) {
		a.writeProblem(w, r, http.StatusForbidden, problemAuthOriginNotAllowed, "origin not allowed")
		return
	}
	var input passwordResetStartRequest
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			a.writeProblem(w, r, http.StatusRequestEntityTooLarge, problemRequestBodyTooLarge, "request body is too large")
			return
		}
		a.writeProblem(w, r, http.StatusBadRequest, problemRequestValidationFailed, "invalid password reset payload")
		return
	}
	email := normalizeEmailHint(input.Email)
	if email == "" {
		a.writeProblem(w, r, http.StatusBadRequest, problemRequestValidationFailed, "email is required")
		return
	}
	user, found, err := a.providerLogin.FindPasswordResetUser(r.Context(), email)
	if err != nil {
		a.serverError(w, r, "find password reset user", err)
		return
	}
	if !found {
		a.writeJSON(w, http.StatusOK, passwordResetStartedResponse())
		return
	}
	// Notifications are org-scoped; attribute the account-level reset to the
	// user's organization. Every user belongs to at least one org by
	// construction; absence is an invariant violation we surface loudly rather
	// than leak via the response (kept generic to avoid email enumeration).
	orgIDs, _, err := a.authz.LookupOrganizations(r.Context(), identity.AuthorizationSubject{
		Kind: identity.AuthorizationSubjectKindUser,
		ID:   user.UserID,
	}, "read", "")
	if err != nil {
		a.serverError(w, r, "lookup password reset organization", err)
		return
	}
	orgIDs = compactUniqueStrings(orgIDs)
	if len(orgIDs) == 0 {
		trace.SpanFromContext(r.Context()).AddEvent("iam.password_reset.no_organization", trace.WithAttributes(
			attribute.String("auth.user_handle", hashToken(user.UserID)),
		))
		a.writeJSON(w, http.StatusOK, passwordResetStartedResponse())
		return
	}
	code, err := a.providerLogin.StartPasswordReset(r.Context(), user.UserID)
	if err != nil {
		a.serverError(w, r, "start provider password reset", err)
		return
	}
	if err := a.passwordReset.SendPasswordReset(r.Context(), PasswordResetNotification{
		Email:        user.Email,
		ActionURL:    a.passwordResetURL(user.UserID, code),
		ResourceName: "urn:verself:auth:password-resets/" + hashToken(user.UserID),
		OrgID:        orgIDs[0],
	}); err != nil {
		a.serverError(w, r, "send password reset", err)
		return
	}
	a.writeJSON(w, http.StatusOK, passwordResetStartedResponse())
}

func (a *BrowserAuth) handlePasswordResetComplete(w http.ResponseWriter, r *http.Request) {
	if !a.sameOrigin(r) {
		a.writeProblem(w, r, http.StatusForbidden, problemAuthOriginNotAllowed, "origin not allowed")
		return
	}
	var input passwordResetCompleteRequest
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			a.writeProblem(w, r, http.StatusRequestEntityTooLarge, problemRequestBodyTooLarge, "request body is too large")
			return
		}
		a.writeProblem(w, r, http.StatusBadRequest, problemRequestValidationFailed, "invalid password reset payload")
		return
	}
	userID := strings.TrimSpace(input.UserID)
	verificationCode := strings.TrimSpace(input.VerificationCode)
	if userID == "" || verificationCode == "" {
		a.writeProblem(w, r, http.StatusBadRequest, problemRequestValidationFailed, "reset token is required")
		return
	}
	if err := identity.ValidatePasswordPolicy(input.Password); err != nil {
		if a.writePasswordPolicyProblem(w, r, err) {
			return
		}
		a.serverError(w, r, "validate reset password", err)
		return
	}
	if err := a.providerLogin.CompletePasswordReset(r.Context(), userID, verificationCode, input.Password); err != nil {
		if identity.IsInvalid(err) {
			a.writeProblem(w, r, http.StatusBadRequest, problemIAMPasswordResetInvalid, "Reset link is invalid or expired.")
			return
		}
		a.serverError(w, r, "complete provider password reset", err)
		return
	}
	a.writeJSON(w, http.StatusOK, passwordResetResponse{
		Status:  "completed",
		Message: "Password reset complete.",
	})
}

func passwordResetStartedResponse() passwordResetResponse {
	return passwordResetResponse{
		Status:  "accepted",
		Message: "If that email has an account, we sent reset instructions.",
	}
}

func (a *BrowserAuth) passwordResetURL(userID, verificationCode string) string {
	reset := *a.publicBaseURL
	reset.Path = "/reset-password"
	query := reset.Query()
	query.Set("user_id", strings.TrimSpace(userID))
	query.Set("verification_code", strings.TrimSpace(verificationCode))
	reset.RawQuery = query.Encode()
	reset.Fragment = ""
	return reset.String()
}

func (a *BrowserAuth) writePasswordPolicyProblem(w http.ResponseWriter, r *http.Request, err error) bool {
	switch {
	case errors.Is(err, identity.ErrPasswordTooShort):
		a.writeProblem(w, r, http.StatusBadRequest, problemIAMPasswordTooShort, fmt.Sprintf("Password must be at least %d characters.", identity.PasswordMinRunes))
	case errors.Is(err, identity.ErrPasswordTooLong):
		a.writeProblem(w, r, http.StatusBadRequest, problemIAMPasswordTooLong, "Password is too long.")
	case errors.Is(err, identity.ErrPasswordRejected):
		a.writeProblem(w, r, http.StatusBadRequest, problemIAMPasswordRejected, "Password does not meet the current password policy.")
	default:
		return false
	}
	return true
}

func newOIDCLoginSecrets() (state string, nonce string, verifier string, err error) {
	state, err = randomToken(32)
	if err != nil {
		return "", "", "", err
	}
	nonce, err = randomToken(32)
	if err != nil {
		return "", "", "", err
	}
	verifier, err = randomToken(48)
	if err != nil {
		return "", "", "", err
	}
	return state, nonce, verifier, nil
}

func passwordLoginOIDCOptions(input passwordLoginRequest, email string) []oauth2.AuthCodeOption {
	options := []oauth2.AuthCodeOption{}
	if prompt := browserLoginPrompt(input.Prompt); prompt != "" {
		options = append(options, oauth2.SetAuthURLParam("prompt", prompt))
	}
	if email != "" {
		options = append(options, oauth2.SetAuthURLParam("login_hint", email))
	}
	return options
}

func (a *BrowserAuth) startProviderOIDCAuthRequest(ctx context.Context, state, nonce, verifier string, extra []oauth2.AuthCodeOption) (string, error) {
	options := []oauth2.AuthCodeOption{
		oauth2.SetAuthURLParam("nonce", nonce),
		oauth2.S256ChallengeOption(verifier),
	}
	options = append(options, extra...)
	authURL := a.oauth.AuthCodeURL(state, options...)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, authURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	client := *a.httpClient
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 300 || resp.StatusCode > 399 {
		return "", fmt.Errorf("provider auth request returned HTTP %d", resp.StatusCode)
	}
	location := strings.TrimSpace(resp.Header.Get("Location"))
	if location == "" {
		return "", errors.New("provider auth request did not return a redirect")
	}
	redirectURL, err := url.Parse(location)
	if err != nil {
		return "", err
	}
	if !redirectURL.IsAbs() {
		base, parseErr := url.Parse(authURL)
		if parseErr != nil {
			return "", parseErr
		}
		redirectURL = base.ResolveReference(redirectURL)
	}
	authRequestID := firstNonEmpty(
		redirectURL.Query().Get("authRequest"),
		redirectURL.Query().Get("auth_request"),
		redirectURL.Query().Get("authRequestId"),
		redirectURL.Query().Get("auth_request_id"),
	)
	if authRequestID == "" {
		return "", fmt.Errorf("provider auth redirect is missing auth request id: %s", redirectURL.Path)
	}
	if _, err := a.providerLogin.GetOIDCAuthRequest(ctx, authRequestID); err != nil {
		return "", err
	}
	return authRequestID, nil
}

func requestmetaIP(ctx context.Context) string {
	info, ok := ctx.Value(browserAuthRequestInfoKey{}).(browserAuthRequestInfo)
	if !ok {
		return ""
	}
	return info.Attribution.ClientIP
}

func (a *BrowserAuth) handleDeviceLoginLookup(w http.ResponseWriter, r *http.Request) {
	var input deviceLoginLookupRequest
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			a.writeProblem(w, r, http.StatusRequestEntityTooLarge, problemRequestBodyTooLarge, "request body is too large")
			return
		}
		a.writeProblem(w, r, http.StatusBadRequest, problemRequestValidationFailed, "invalid device login payload")
		return
	}
	userCode := normalizeDeviceUserCode(input.UserCode)
	if userCode == "" {
		a.writeProblem(w, r, http.StatusBadRequest, problemRequestValidationFailed, "device code is required")
		return
	}
	intent, err := a.q.GetDeviceLoginIntentByUserCodeHash(r.Context(), identitystore.GetDeviceLoginIntentByUserCodeHashParams{UserCodeHash: deviceLoginUserCodeHash(userCode)})
	if errors.Is(err, pgx.ErrNoRows) {
		a.writeProblem(w, r, http.StatusNotFound, problemIAMDeviceLoginNotFound, "device login request was not found")
		return
	}
	if err != nil {
		a.serverError(w, r, "load device login request", err)
		return
	}
	record := deviceLoginRecordFromUserCodeHash(intent)
	if record.State == "pending" && !record.ExpiresAt.After(time.Now().UTC()) {
		if expired, expireErr := a.q.MarkExpiredDeviceLoginIntent(r.Context(), identitystore.MarkExpiredDeviceLoginIntentParams{DeviceLoginHandle: record.DeviceLoginHandle}); expireErr == nil {
			record = deviceLoginRecordFromExpired(expired)
		}
	}
	if record.State == "expired" {
		a.writeProblem(w, r, http.StatusGone, problemIAMDeviceLoginExpired, "device login request has expired")
		return
	}
	a.writeJSON(w, http.StatusOK, deviceLoginResponseFromRecord(record))
}

func (a *BrowserAuth) handleDeviceLoginApprove(w http.ResponseWriter, r *http.Request) {
	a.handleDeviceLoginDecision(w, r, true)
}

func (a *BrowserAuth) handleDeviceLoginDeny(w http.ResponseWriter, r *http.Request) {
	a.handleDeviceLoginDecision(w, r, false)
}

func (a *BrowserAuth) handleDeviceLoginDecision(w http.ResponseWriter, r *http.Request, approve bool) {
	if !a.sameOrigin(r) {
		a.writeProblem(w, r, http.StatusForbidden, problemAuthOriginNotAllowed, "origin not allowed")
		return
	}
	session, err := a.requireAccount(w, r)
	if err != nil {
		return
	}
	handle := deviceLoginHandleFromPath(r.URL.Path, approve)
	if handle == "" {
		a.writeProblem(w, r, http.StatusBadRequest, problemRequestValidationFailed, "device login handle is required")
		return
	}
	intent, err := a.q.GetDeviceLoginIntent(r.Context(), identitystore.GetDeviceLoginIntentParams{DeviceLoginHandle: handle})
	if errors.Is(err, pgx.ErrNoRows) {
		a.writeProblem(w, r, http.StatusNotFound, problemIAMDeviceLoginNotFound, "device login request was not found")
		return
	}
	if err != nil {
		a.serverError(w, r, "load device login request", err)
		return
	}
	record := deviceLoginRecordFromGet(intent)
	if record.State != "pending" || !record.ExpiresAt.After(time.Now().UTC()) {
		if record.State == "pending" {
			if expired, expireErr := a.q.MarkExpiredDeviceLoginIntent(r.Context(), identitystore.MarkExpiredDeviceLoginIntentParams{DeviceLoginHandle: handle}); expireErr == nil {
				record = deviceLoginRecordFromExpired(expired)
			}
		}
		if record.State == "expired" {
			a.writeProblem(w, r, http.StatusGone, problemIAMDeviceLoginExpired, "device login request has expired")
			return
		}
		a.writeProblem(w, r, http.StatusConflict, problemIAMDeviceLoginNotPending, "device login request is no longer pending")
		return
	}
	providerSession := session.providerLoginSession()
	if strings.TrimSpace(providerSession.SessionID) == "" || strings.TrimSpace(providerSession.SessionToken) == "" {
		a.writeProblem(w, r, http.StatusConflict, problemAuthProviderSession, "Sign in again before approving this device.")
		return
	}
	if approve {
		if err := a.providerLogin.ApproveDeviceAuthorization(r.Context(), record.ProviderDeviceAuthorizationID, providerSession); err != nil {
			a.serverError(w, r, "approve provider device login", err)
			return
		}
		row, err := a.q.ApproveDeviceLoginIntent(r.Context(), identitystore.ApproveDeviceLoginIntentParams{
			ApprovedByAccountHandle: session.AccountHandle,
			ApprovedBySubject:       session.User.Sub,
			ApprovedByEmail:         stringValue(session.User.Email),
			DeviceLoginHandle:       handle,
		})
		if err != nil {
			a.serverError(w, r, "persist device login approval", err)
			return
		}
		a.writeJSON(w, http.StatusOK, deviceLoginResponseFromRecord(deviceLoginRecordFromApprove(row)))
		return
	}
	if err := a.providerLogin.DenyDeviceAuthorization(r.Context(), record.ProviderDeviceAuthorizationID); err != nil {
		a.serverError(w, r, "deny provider device login", err)
		return
	}
	row, err := a.q.DenyDeviceLoginIntent(r.Context(), identitystore.DenyDeviceLoginIntentParams{
		DeniedByAccountHandle: session.AccountHandle,
		DeniedBySubject:       session.User.Sub,
		DeniedByEmail:         stringValue(session.User.Email),
		DeviceLoginHandle:     handle,
	})
	if err != nil {
		a.serverError(w, r, "persist device login denial", err)
		return
	}
	a.writeJSON(w, http.StatusOK, deviceLoginResponseFromRecord(deviceLoginRecordFromDeny(row)))
}

func normalizeDeviceUserCode(value string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	value = strings.NewReplacer(" ", "", "\t", "", "\n", "", "\r", "").Replace(value)
	return value
}

func deviceLoginHandle(userCode string) string {
	sum := sha256.Sum256([]byte("verself device login handle v1\x00" + normalizeDeviceUserCode(userCode)))
	return "dlr_" + base64.RawURLEncoding.EncodeToString(sum[:])[:32]
}

func deviceLoginUserCodeHash(userCode string) []byte {
	sum := sha256.Sum256([]byte("verself device login user code v1\x00" + normalizeDeviceUserCode(userCode)))
	return sum[:]
}

func deviceLoginUserCodeSuffix(userCode string) string {
	normalized := normalizeDeviceUserCode(userCode)
	if len(normalized) <= 4 {
		return normalized
	}
	return normalized[len(normalized)-4:]
}

func deviceLoginHandleFromPath(path string, approve bool) string {
	suffix := "/denial"
	if approve {
		suffix = "/approval"
	}
	value := strings.TrimSuffix(strings.TrimPrefix(path, "/device-logins/"), suffix)
	return strings.Trim(value, "/")
}

func (a *BrowserAuth) upsertDeviceLoginIntent(ctx context.Context, userCode string, request identity.DeviceAuthorizationRequest, expiresAt time.Time) (deviceLoginRecord, error) {
	scopes, err := json.Marshal(request.Scopes)
	if err != nil {
		return deviceLoginRecord{}, err
	}
	row, err := a.q.UpsertDeviceLoginIntent(ctx, identitystore.UpsertDeviceLoginIntentParams{
		DeviceLoginHandle:             deviceLoginHandle(userCode),
		UserCodeHash:                  deviceLoginUserCodeHash(userCode),
		UserCodeSuffix:                deviceLoginUserCodeSuffix(userCode),
		ProviderDeviceAuthorizationID: request.ID,
		ClientID:                      request.ClientID,
		AppName:                       request.AppName,
		ProjectName:                   request.ProjectName,
		ScopesJson:                    scopes,
		ExpiresAt:                     timestamptz(expiresAt.UTC()),
	})
	if err != nil {
		return deviceLoginRecord{}, err
	}
	return deviceLoginRecordFromUpsert(row), nil
}

func (a *BrowserAuth) deviceVerificationURLs(userCode string) (string, string) {
	verification := *a.publicBaseURL
	verification.Path = "/device"
	verification.RawQuery = ""
	verification.Fragment = ""
	complete := verification
	query := complete.Query()
	query.Set("user_code", normalizeDeviceUserCode(userCode))
	complete.RawQuery = query.Encode()
	return verification.String(), complete.String()
}

func compactAuthScopes(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := values[:0]
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func deviceLoginResponseFromRecord(record deviceLoginRecord) deviceLoginResponse {
	scopes := []string{}
	_ = json.Unmarshal([]byte(record.ScopesJSON), &scopes)
	return deviceLoginResponse{
		DeviceLoginHandle: record.DeviceLoginHandle,
		UserCodeSuffix:    record.UserCodeSuffix,
		ClientID:          record.ClientID,
		AppName:           record.AppName,
		ProjectName:       record.ProjectName,
		Scopes:            scopes,
		State:             record.State,
		ApprovedAt:        formatOptionalTime(record.ApprovedAt),
		DeniedAt:          formatOptionalTime(record.DeniedAt),
		ExpiresAt:         record.ExpiresAt.Format(time.RFC3339),
		UpdatedAt:         record.UpdatedAt.Format(time.RFC3339),
	}
}

func formatOptionalTime(value time.Time) string {
	if value.IsZero() || value.Equal(time.Unix(0, 0).UTC()) {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func deviceLoginRecordFromUpsert(row identitystore.UpsertDeviceLoginIntentRow) deviceLoginRecord {
	return deviceLoginRecord{
		DeviceLoginHandle:             row.DeviceLoginHandle,
		UserCodeSuffix:                row.UserCodeSuffix,
		ProviderDeviceAuthorizationID: row.ProviderDeviceAuthorizationID,
		ClientID:                      row.ClientID,
		AppName:                       row.AppName,
		ProjectName:                   row.ProjectName,
		ScopesJSON:                    row.ScopesJson,
		State:                         row.State,
		ApprovedAt:                    requiredTime(row.ApprovedAt),
		DeniedAt:                      requiredTime(row.DeniedAt),
		ExpiresAt:                     requiredTime(row.ExpiresAt),
		UpdatedAt:                     requiredTime(row.UpdatedAt),
	}
}

func deviceLoginRecordFromGet(row identitystore.GetDeviceLoginIntentRow) deviceLoginRecord {
	return deviceLoginRecord{
		DeviceLoginHandle:             row.DeviceLoginHandle,
		UserCodeSuffix:                row.UserCodeSuffix,
		ProviderDeviceAuthorizationID: row.ProviderDeviceAuthorizationID,
		ClientID:                      row.ClientID,
		AppName:                       row.AppName,
		ProjectName:                   row.ProjectName,
		ScopesJSON:                    row.ScopesJson,
		State:                         row.State,
		ApprovedAt:                    requiredTime(row.ApprovedAt),
		DeniedAt:                      requiredTime(row.DeniedAt),
		ExpiresAt:                     requiredTime(row.ExpiresAt),
		UpdatedAt:                     requiredTime(row.UpdatedAt),
	}
}

func deviceLoginRecordFromUserCodeHash(row identitystore.GetDeviceLoginIntentByUserCodeHashRow) deviceLoginRecord {
	return deviceLoginRecord{
		DeviceLoginHandle:             row.DeviceLoginHandle,
		UserCodeSuffix:                row.UserCodeSuffix,
		ProviderDeviceAuthorizationID: row.ProviderDeviceAuthorizationID,
		ClientID:                      row.ClientID,
		AppName:                       row.AppName,
		ProjectName:                   row.ProjectName,
		ScopesJSON:                    row.ScopesJson,
		State:                         row.State,
		ApprovedAt:                    requiredTime(row.ApprovedAt),
		DeniedAt:                      requiredTime(row.DeniedAt),
		ExpiresAt:                     requiredTime(row.ExpiresAt),
		UpdatedAt:                     requiredTime(row.UpdatedAt),
	}
}

func deviceLoginRecordFromExpired(row identitystore.MarkExpiredDeviceLoginIntentRow) deviceLoginRecord {
	return deviceLoginRecord{
		DeviceLoginHandle:             row.DeviceLoginHandle,
		UserCodeSuffix:                row.UserCodeSuffix,
		ProviderDeviceAuthorizationID: row.ProviderDeviceAuthorizationID,
		ClientID:                      row.ClientID,
		AppName:                       row.AppName,
		ProjectName:                   row.ProjectName,
		ScopesJSON:                    row.ScopesJson,
		State:                         row.State,
		ApprovedAt:                    requiredTime(row.ApprovedAt),
		DeniedAt:                      requiredTime(row.DeniedAt),
		ExpiresAt:                     requiredTime(row.ExpiresAt),
		UpdatedAt:                     requiredTime(row.UpdatedAt),
	}
}

func deviceLoginRecordFromApprove(row identitystore.ApproveDeviceLoginIntentRow) deviceLoginRecord {
	return deviceLoginRecord{
		DeviceLoginHandle:             row.DeviceLoginHandle,
		UserCodeSuffix:                row.UserCodeSuffix,
		ProviderDeviceAuthorizationID: row.ProviderDeviceAuthorizationID,
		ClientID:                      row.ClientID,
		AppName:                       row.AppName,
		ProjectName:                   row.ProjectName,
		ScopesJSON:                    row.ScopesJson,
		State:                         row.State,
		ApprovedAt:                    requiredTime(row.ApprovedAt),
		DeniedAt:                      requiredTime(row.DeniedAt),
		ExpiresAt:                     requiredTime(row.ExpiresAt),
		UpdatedAt:                     requiredTime(row.UpdatedAt),
	}
}

func deviceLoginRecordFromDeny(row identitystore.DenyDeviceLoginIntentRow) deviceLoginRecord {
	return deviceLoginRecord{
		DeviceLoginHandle:             row.DeviceLoginHandle,
		UserCodeSuffix:                row.UserCodeSuffix,
		ProviderDeviceAuthorizationID: row.ProviderDeviceAuthorizationID,
		ClientID:                      row.ClientID,
		AppName:                       row.AppName,
		ProjectName:                   row.ProjectName,
		ScopesJSON:                    row.ScopesJson,
		State:                         row.State,
		ApprovedAt:                    requiredTime(row.ApprovedAt),
		DeniedAt:                      requiredTime(row.DeniedAt),
		ExpiresAt:                     requiredTime(row.ExpiresAt),
		UpdatedAt:                     requiredTime(row.UpdatedAt),
	}
}
