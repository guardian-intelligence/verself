package zitadel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/verself/iam-service/internal/identity"
)

const (
	defaultTimeout = 5 * time.Second
	userPageLimit  = 200
)

var zitadelMaxKeyExpiration = time.Date(9999, time.December, 31, 23, 59, 59, 0, time.UTC)

type Config struct {
	BaseURL    string
	HostHeader string
	AdminToken string
	HTTPClient *http.Client
}

type ProductPublicIdentifiersConfig struct {
	ProjectID          string
	BrowserAppName     string
	GitHubLoginIDPName string
}

type ProductPublicIdentifiers struct {
	BrowserOIDCClientID string
	GitHubLoginIDPID    string
}

type Client struct {
	baseURL    *url.URL
	hostHeader string
	adminToken string
	httpClient *http.Client
}

func New(cfg Config) (*Client, error) {
	baseURL, err := url.Parse(strings.TrimSpace(cfg.BaseURL))
	if err != nil {
		return nil, fmt.Errorf("zitadel: parse base url: %w", err)
	}
	if baseURL.Scheme == "" || baseURL.Host == "" {
		return nil, errors.New("zitadel: base url must be absolute")
	}
	if strings.TrimSpace(cfg.AdminToken) == "" {
		return nil, errors.New("zitadel: admin token is required")
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultTimeout}
	}
	return &Client{
		baseURL:    baseURL,
		hostHeader: strings.TrimSpace(cfg.HostHeader),
		adminToken: strings.TrimSpace(cfg.AdminToken),
		httpClient: httpClient,
	}, nil
}

func (c *Client) ProductPublicIdentifiers(ctx context.Context, cfg ProductPublicIdentifiersConfig) (ProductPublicIdentifiers, error) {
	projectID := strings.TrimSpace(cfg.ProjectID)
	browserAppName := strings.TrimSpace(cfg.BrowserAppName)
	if projectID == "" || browserAppName == "" {
		return ProductPublicIdentifiers{}, fmt.Errorf("%w: project_id and browser_app_name are required", identity.ErrInvalidInput)
	}
	browserClientID, err := c.OIDCClientIDByAppName(ctx, projectID, browserAppName)
	if err != nil {
		return ProductPublicIdentifiers{}, err
	}
	githubIDPID := ""
	if name := strings.TrimSpace(cfg.GitHubLoginIDPName); name != "" {
		var found bool
		githubIDPID, found, err = c.IDPIDByName(ctx, name)
		if err != nil {
			return ProductPublicIdentifiers{}, err
		}
		if !found {
			return ProductPublicIdentifiers{}, fmt.Errorf("%w: IdP %s is missing", identity.ErrZitadelUnavailable, name)
		}
	}
	return ProductPublicIdentifiers{
		BrowserOIDCClientID: browserClientID,
		GitHubLoginIDPID:    githubIDPID,
	}, nil
}

func (c *Client) OIDCClientIDByAppName(ctx context.Context, projectID, name string) (string, error) {
	projectID = strings.TrimSpace(projectID)
	name = strings.TrimSpace(name)
	if projectID == "" || name == "" {
		return "", fmt.Errorf("%w: project_id and app name are required", identity.ErrInvalidInput)
	}
	var out struct {
		Result []struct {
			ID         string `json:"id"`
			Name       string `json:"name"`
			OIDCConfig struct {
				ClientID string `json:"clientId"`
			} `json:"oidcConfig"`
		} `json:"result"`
	}
	body := map[string]any{"queries": []map[string]any{{"nameQuery": map[string]string{"name": name, "method": "TEXT_QUERY_METHOD_EQUALS"}}}}
	path := "/management/v1/projects/" + url.PathEscape(projectID) + "/apps/_search"
	if err := c.doJSON(ctx, http.MethodPost, path, body, &out); err != nil {
		return "", fmt.Errorf("%w: search OIDC application %s: %v", identity.ErrZitadelUnavailable, name, err)
	}
	for _, item := range out.Result {
		if !strings.EqualFold(strings.TrimSpace(item.Name), name) {
			continue
		}
		clientID := strings.TrimSpace(item.OIDCConfig.ClientID)
		if clientID == "" {
			break
		}
		return clientID, nil
	}
	return "", fmt.Errorf("%w: OIDC application %s is missing", identity.ErrZitadelUnavailable, name)
}

func (c *Client) IDPIDByName(ctx context.Context, name string) (string, bool, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", false, fmt.Errorf("%w: idp name is required", identity.ErrInvalidInput)
	}
	var out struct {
		Result []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"result"`
	}
	body := map[string]any{"queries": []map[string]any{{"idpNameQuery": map[string]string{"name": name, "method": "TEXT_QUERY_METHOD_EQUALS"}}}}
	if err := c.doJSON(ctx, http.MethodPost, "/admin/v1/idps/_search", body, &out); err != nil {
		return "", false, fmt.Errorf("%w: search IdP %s: %v", identity.ErrZitadelUnavailable, name, err)
	}
	for _, item := range out.Result {
		if strings.EqualFold(strings.TrimSpace(item.Name), name) && strings.TrimSpace(item.ID) != "" {
			return strings.TrimSpace(item.ID), true, nil
		}
	}
	return "", false, nil
}

func (c *Client) ListMembers(ctx context.Context, orgID string) ([]identity.Member, error) {
	orgID = strings.TrimSpace(orgID)
	if orgID == "" {
		return nil, fmt.Errorf("%w: org_id is required", identity.ErrInvalidInput)
	}
	users, err := c.listUsersByOrganization(ctx, orgID)
	if err != nil {
		return nil, err
	}
	members := make([]identity.Member, 0, len(users))
	for userID, user := range users {
		member := identity.Member{
			UserID:      userID,
			Type:        user.Type,
			Email:       user.Email,
			LoginName:   user.LoginName,
			DisplayName: firstNonEmpty(user.DisplayName, user.LoginName, user.Email),
			State:       user.State,
		}
		members = append(members, member)
	}
	sort.Slice(members, func(i, j int) bool {
		return members[i].DisplayName < members[j].DisplayName
	})
	return members, nil
}

func (c *Client) CreateOrganization(ctx context.Context, input identity.DirectoryCreateOrganizationRequest) (identity.DirectoryCreateOrganizationResult, error) {
	name := strings.TrimSpace(input.Name)
	adminUserID := strings.TrimSpace(input.AdminUserID)
	if name == "" {
		return identity.DirectoryCreateOrganizationResult{}, fmt.Errorf("%w: organization name is required", identity.ErrInvalidInput)
	}
	body := map[string]any{
		"name": name,
	}
	if adminUserID != "" {
		body["admins"] = []map[string]any{{"userId": adminUserID}}
	}
	var out struct {
		OrganizationID string `json:"organizationId"`
		OrgID          string `json:"orgId"`
		ID             string `json:"id"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/v2/organizations", body, &out); err != nil {
		return identity.DirectoryCreateOrganizationResult{}, fmt.Errorf("%w: create organization: %v", identity.ErrZitadelUnavailable, err)
	}
	orgID := firstNonEmpty(out.OrganizationID, out.OrgID, out.ID)
	if orgID == "" {
		return identity.DirectoryCreateOrganizationResult{}, fmt.Errorf("%w: create organization returned no organization id", identity.ErrZitadelUnavailable)
	}
	return identity.DirectoryCreateOrganizationResult{OrganizationID: orgID}, nil
}

func (c *Client) CreateSignupUser(ctx context.Context, input identity.DirectoryCreateSignupUserRequest) (identity.DirectoryCreateSignupUserResult, error) {
	orgID := strings.TrimSpace(input.OrgID)
	if orgID == "" || strings.TrimSpace(input.Email) == "" || input.Password == "" {
		return identity.DirectoryCreateSignupUserResult{}, fmt.Errorf("%w: org_id, email, and password are required", identity.ErrInvalidInput)
	}
	created, err := c.createHumanUser(ctx, orgID, identity.InviteMemberRequest{
		Email:      strings.TrimSpace(input.Email),
		GivenName:  strings.TrimSpace(input.GivenName),
		FamilyName: strings.TrimSpace(input.FamilyName),
		Password:   input.Password,
	}, identity.ErrSignupAccountExists)
	if err != nil {
		return identity.DirectoryCreateSignupUserResult{}, err
	}
	if err := c.verifyEmail(ctx, created.UserID, created.EmailVerificationCode); err != nil {
		return identity.DirectoryCreateSignupUserResult{}, err
	}
	return identity.DirectoryCreateSignupUserResult{UserID: created.UserID}, nil
}

// CreateHumanWithIDPLink creates a human in orgID with a pre-verified email and
// an external IdP link already attached, for just-in-time provisioning from a
// completed IdP intent. The email is marked verified directly (provider truth),
// so no verification code round-trip is needed, and no password is set.
func (c *Client) CreateHumanWithIDPLink(ctx context.Context, input identity.DirectoryCreateHumanWithIDPLinkRequest) (identity.DirectoryCreateSignupUserResult, error) {
	orgID := strings.TrimSpace(input.OrgID)
	email := strings.TrimSpace(input.Email)
	idpID := strings.TrimSpace(input.IDPID)
	externalUserID := strings.TrimSpace(input.ExternalUserID)
	if orgID == "" || email == "" || idpID == "" || externalUserID == "" {
		return identity.DirectoryCreateSignupUserResult{}, fmt.Errorf("%w: org_id, email, idp_id, and external_user_id are required", identity.ErrInvalidInput)
	}
	body := map[string]any{
		"organizationId": orgID,
		"username":       email,
		"human": map[string]any{
			"profile": map[string]any{
				"givenName":  firstNonEmpty(strings.TrimSpace(input.GivenName), email),
				"familyName": firstNonEmpty(strings.TrimSpace(input.FamilyName), "Member"),
			},
			"email": map[string]any{
				"email":      email,
				"isVerified": true,
			},
			"idpLinks": []map[string]any{
				{
					"idpId":    idpID,
					"userId":   externalUserID,
					"userName": firstNonEmpty(strings.TrimSpace(input.ExternalUserName), externalUserID),
				},
			},
		},
	}
	var out createUserResponse
	if err := c.doJSON(ctx, http.MethodPost, "/v2/users/new", body, &out); err != nil {
		if zitadelUserAlreadyExists(err) {
			return identity.DirectoryCreateSignupUserResult{}, fmt.Errorf("%w: create idp human: %v", identity.ErrSignupAccountExists, err)
		}
		if zitadelRequestInvalid(err) {
			return identity.DirectoryCreateSignupUserResult{}, fmt.Errorf("%w: create idp human: %v", identity.ErrInvalidInput, err)
		}
		return identity.DirectoryCreateSignupUserResult{}, fmt.Errorf("%w: create idp human: %v", identity.ErrZitadelUnavailable, err)
	}
	userID := firstNonEmpty(out.ID, out.UserID)
	if userID == "" {
		return identity.DirectoryCreateSignupUserResult{}, fmt.Errorf("%w: create idp human returned no user id", identity.ErrZitadelUnavailable)
	}
	return identity.DirectoryCreateSignupUserResult{UserID: userID}, nil
}

func (c *Client) FindPasswordResetUser(ctx context.Context, email string) (identity.PasswordResetUser, bool, error) {
	email = strings.TrimSpace(email)
	if email == "" {
		return identity.PasswordResetUser{}, false, fmt.Errorf("%w: email is required", identity.ErrInvalidInput)
	}
	var out usersResponse
	body := map[string]any{
		"query": map[string]any{"limit": 2},
		"queries": []map[string]any{
			{"inUserEmailsQuery": map[string]any{"userEmails": []string{email}}},
		},
	}
	if err := c.doJSON(ctx, http.MethodPost, "/v2/users", body, &out); err != nil {
		return identity.PasswordResetUser{}, false, fmt.Errorf("%w: find password reset user: %v", identity.ErrZitadelUnavailable, err)
	}
	for _, item := range out.Result {
		if item.Human == nil {
			continue
		}
		if !sameEmailIdentity(item.Human.Email.Email, email) {
			continue
		}
		loginName := item.PreferredLoginName
		if loginName == "" && len(item.LoginNames) > 0 {
			loginName = item.LoginNames[0]
		}
		if loginName == "" {
			loginName = item.Username
		}
		return identity.PasswordResetUser{
			UserID:    strings.TrimSpace(item.UserID),
			Email:     strings.TrimSpace(item.Human.Email.Email),
			LoginName: strings.TrimSpace(loginName),
		}, true, nil
	}
	return identity.PasswordResetUser{}, false, nil
}

func (c *Client) StartPasswordReset(ctx context.Context, userID string) (string, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return "", fmt.Errorf("%w: user_id is required", identity.ErrInvalidInput)
	}
	var out struct {
		VerificationCode string `json:"verificationCode"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/v2/users/"+url.PathEscape(userID)+"/password_reset", map[string]any{"returnCode": map[string]any{}}, &out); err != nil {
		return "", fmt.Errorf("%w: start password reset: %v", identity.ErrZitadelUnavailable, err)
	}
	if strings.TrimSpace(out.VerificationCode) == "" {
		return "", fmt.Errorf("%w: password reset returned no verification code", identity.ErrZitadelUnavailable)
	}
	return strings.TrimSpace(out.VerificationCode), nil
}

func (c *Client) CompletePasswordReset(ctx context.Context, userID, verificationCode, password string) error {
	userID = strings.TrimSpace(userID)
	verificationCode = strings.TrimSpace(verificationCode)
	if userID == "" || verificationCode == "" || password == "" {
		return fmt.Errorf("%w: user_id, verification_code, and password are required", identity.ErrInvalidInput)
	}
	body := map[string]any{
		"verificationCode": verificationCode,
		"newPassword": map[string]any{
			"password":       password,
			"changeRequired": false,
		},
	}
	if err := c.doJSON(ctx, http.MethodPost, "/v2/users/"+url.PathEscape(userID)+"/password", body, nil); err != nil {
		if zitadelRequestInvalid(err) {
			return fmt.Errorf("%w: complete password reset", identity.ErrInvalidInput)
		}
		return fmt.Errorf("%w: complete password reset: %v", identity.ErrZitadelUnavailable, err)
	}
	return nil
}

func (c *Client) InviteMember(ctx context.Context, orgID string, input identity.InviteMemberRequest) (identity.DirectoryInviteMemberResult, error) {
	created, err := c.createHumanUser(ctx, orgID, input, identity.ErrInvalidInput)
	if err != nil {
		return identity.DirectoryInviteMemberResult{}, err
	}
	return identity.DirectoryInviteMemberResult{
		UserID:                created.UserID,
		Email:                 input.Email,
		Status:                "invited",
		EmailVerificationCode: created.EmailVerificationCode,
	}, nil
}

func (c *Client) CompleteMemberInvite(ctx context.Context, input identity.DirectoryCompleteMemberInviteRequest) error {
	userID := strings.TrimSpace(input.UserID)
	emailVerificationCode := strings.TrimSpace(input.EmailVerificationCode)
	if userID == "" || emailVerificationCode == "" {
		return fmt.Errorf("%w: user_id and email verification code are required", identity.ErrInvalidInput)
	}
	if err := c.verifyEmail(ctx, userID, emailVerificationCode); err != nil {
		return err
	}
	return nil
}

func (c *Client) DeleteSession(ctx context.Context, sessionID string) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return fmt.Errorf("%w: session_id is required", identity.ErrInvalidInput)
	}
	path := "/v2/sessions/" + url.PathEscape(sessionID)
	if err := c.doJSON(ctx, http.MethodDelete, path, map[string]any{}, nil); err != nil {
		if zitadelSessionAlreadyGone(err) {
			return nil
		}
		return fmt.Errorf("%w: delete session: %v", identity.ErrZitadelUnavailable, err)
	}
	return nil
}

func (c *Client) CreatePasswordSession(ctx context.Context, input identity.LoginSessionInput) (identity.LoginSession, error) {
	loginName := strings.TrimSpace(input.LoginName)
	password := input.Password
	if loginName == "" || password == "" {
		return identity.LoginSession{}, fmt.Errorf("%w: login_name and password are required", identity.ErrInvalidInput)
	}
	createBody := map[string]any{
		"checks": map[string]any{
			"user": map[string]any{"loginName": loginName},
		},
	}
	if input.Lifetime > 0 {
		createBody["lifetime"] = protobufDuration(input.Lifetime)
	}
	if userAgent := loginSessionUserAgent(input); len(userAgent) > 0 {
		createBody["userAgent"] = userAgent
	}
	var created sessionResponse
	if err := c.doJSON(ctx, http.MethodPost, "/v2/sessions", createBody, &created); err != nil {
		return identity.LoginSession{}, mapLoginSessionError("create session", err)
	}
	sessionID := strings.TrimSpace(created.SessionID)
	if sessionID == "" || strings.TrimSpace(created.SessionToken) == "" {
		return identity.LoginSession{}, fmt.Errorf("%w: create session returned no session token", identity.ErrZitadelUnavailable)
	}
	var checked sessionResponse
	if err := c.doJSON(ctx, http.MethodPatch, "/v2/sessions/"+url.PathEscape(sessionID), map[string]any{
		"checks": map[string]any{
			"password": map[string]any{"password": password},
		},
	}, &checked); err != nil {
		_ = c.DeleteSession(context.WithoutCancel(ctx), sessionID)
		return identity.LoginSession{}, mapLoginSessionError("check password", err)
	}
	sessionToken := strings.TrimSpace(checked.SessionToken)
	if sessionToken == "" {
		return identity.LoginSession{}, fmt.Errorf("%w: password check returned no session token", identity.ErrZitadelUnavailable)
	}
	expiresAt := time.Time{}
	if input.Lifetime > 0 {
		expiresAt = time.Now().UTC().Add(input.Lifetime)
	}
	return identity.LoginSession{SessionID: sessionID, SessionToken: sessionToken, ExpiresAt: expiresAt}, nil
}

func (c *Client) GetOIDCAuthRequest(ctx context.Context, authRequestID string) (identity.OIDCAuthRequest, error) {
	authRequestID = strings.TrimSpace(authRequestID)
	if authRequestID == "" {
		return identity.OIDCAuthRequest{}, fmt.Errorf("%w: auth_request_id is required", identity.ErrInvalidInput)
	}
	var out struct {
		AuthRequest struct {
			ID          string   `json:"id"`
			ClientID    string   `json:"clientId"`
			Scope       []string `json:"scope"`
			RedirectURI string   `json:"redirectUri"`
			LoginHint   string   `json:"loginHint"`
		} `json:"authRequest"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/v2/oidc/auth_requests/"+url.PathEscape(authRequestID), nil, &out); err != nil {
		return identity.OIDCAuthRequest{}, fmt.Errorf("%w: get OIDC auth request: %v", identity.ErrZitadelUnavailable, err)
	}
	return identity.OIDCAuthRequest{
		ID:          strings.TrimSpace(firstNonEmpty(out.AuthRequest.ID, authRequestID)),
		ClientID:    strings.TrimSpace(out.AuthRequest.ClientID),
		Scopes:      compactStrings(out.AuthRequest.Scope),
		RedirectURI: strings.TrimSpace(out.AuthRequest.RedirectURI),
		LoginHint:   strings.TrimSpace(out.AuthRequest.LoginHint),
	}, nil
}

func (c *Client) FinalizeOIDCAuthRequest(ctx context.Context, authRequestID string, session identity.LoginSession) (string, error) {
	authRequestID = strings.TrimSpace(authRequestID)
	session.SessionID = strings.TrimSpace(session.SessionID)
	session.SessionToken = strings.TrimSpace(session.SessionToken)
	if authRequestID == "" || session.SessionID == "" || session.SessionToken == "" {
		return "", fmt.Errorf("%w: auth_request_id, session_id, and session_token are required", identity.ErrInvalidInput)
	}
	var out struct {
		CallbackURL string `json:"callbackUrl"`
	}
	body := map[string]any{
		"session": map[string]any{
			"sessionId":    session.SessionID,
			"sessionToken": session.SessionToken,
		},
	}
	if err := c.doJSON(ctx, http.MethodPost, "/v2/oidc/auth_requests/"+url.PathEscape(authRequestID), body, &out); err != nil {
		return "", fmt.Errorf("%w: finalize OIDC auth request: %v", identity.ErrZitadelUnavailable, err)
	}
	if strings.TrimSpace(out.CallbackURL) == "" {
		return "", fmt.Errorf("%w: finalize OIDC auth request returned no callback URL", identity.ErrZitadelUnavailable)
	}
	return strings.TrimSpace(out.CallbackURL), nil
}

// StartIDPIntent begins an external identity provider login intent and returns
// the provider authorization URL. Zitadel appends `id` and `token` query
// parameters to successURL after the provider redirects back through
// /idps/callback.
func (c *Client) StartIDPIntent(ctx context.Context, idpID, successURL, failureURL string) (identity.IDPIntentStart, error) {
	idpID = strings.TrimSpace(idpID)
	successURL = strings.TrimSpace(successURL)
	failureURL = strings.TrimSpace(failureURL)
	if idpID == "" || successURL == "" || failureURL == "" {
		return identity.IDPIntentStart{}, fmt.Errorf("%w: idp_id, success_url, and failure_url are required", identity.ErrInvalidInput)
	}
	body := map[string]any{
		"idpId": idpID,
		"urls": map[string]any{
			"successUrl": successURL,
			"failureUrl": failureURL,
		},
	}
	var out struct {
		AuthURL string `json:"authUrl"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/v2/idp_intents", body, &out); err != nil {
		if zitadelRequestInvalid(err) {
			return identity.IDPIntentStart{}, fmt.Errorf("%w: start idp intent: %v", identity.ErrInvalidInput, err)
		}
		return identity.IDPIntentStart{}, fmt.Errorf("%w: start idp intent: %v", identity.ErrZitadelUnavailable, err)
	}
	authURL := strings.TrimSpace(out.AuthURL)
	if authURL == "" {
		return identity.IDPIntentStart{}, fmt.Errorf("%w: start idp intent returned no auth URL", identity.ErrZitadelUnavailable)
	}
	return identity.IDPIntentStart{AuthURL: authURL}, nil
}

// RetrieveIDPIntent reads the external identity resolved for a completed intent.
// The top-level userId is the linked Zitadel user (empty when unlinked).
func (c *Client) RetrieveIDPIntent(ctx context.Context, intentID, intentToken string) (identity.IDPIntentResult, error) {
	intentID = strings.TrimSpace(intentID)
	intentToken = strings.TrimSpace(intentToken)
	if intentID == "" || intentToken == "" {
		return identity.IDPIntentResult{}, fmt.Errorf("%w: idp_intent_id and idp_intent_token are required", identity.ErrInvalidInput)
	}
	var out struct {
		UserID         string `json:"userId"`
		IDPInformation struct {
			IDPID          string         `json:"idpId"`
			UserID         string         `json:"userId"`
			UserName       string         `json:"userName"`
			RawInformation map[string]any `json:"rawInformation"`
		} `json:"idpInformation"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/v2/idp_intents/"+url.PathEscape(intentID), map[string]any{"idpIntentToken": intentToken}, &out); err != nil {
		if zitadelRequestInvalid(err) {
			return identity.IDPIntentResult{}, fmt.Errorf("%w: retrieve idp intent: %v", identity.ErrInvalidInput, err)
		}
		return identity.IDPIntentResult{}, fmt.Errorf("%w: retrieve idp intent: %v", identity.ErrZitadelUnavailable, err)
	}
	email, emailVerified := githubEmailFromRawInformation(out.IDPInformation.RawInformation)
	return identity.IDPIntentResult{
		IDPID:           strings.TrimSpace(out.IDPInformation.IDPID),
		UserID:          strings.TrimSpace(out.UserID),
		ExternalSubject: strings.TrimSpace(out.IDPInformation.UserID),
		Username:        strings.TrimSpace(out.IDPInformation.UserName),
		Email:           email,
		EmailVerified:   emailVerified,
	}, nil
}

// FindHumanByVerifiedEmail returns the Zitadel user id of a human whose email
// matches and is verified. Used to auto-link a verified external identity to an
// existing account by verified email. Reuses the /v2/users email search.
func (c *Client) FindHumanByVerifiedEmail(ctx context.Context, email string) (string, bool, error) {
	email = strings.TrimSpace(email)
	if email == "" {
		return "", false, fmt.Errorf("%w: email is required", identity.ErrInvalidInput)
	}
	var out usersResponse
	body := map[string]any{
		"query": map[string]any{"limit": 2},
		"queries": []map[string]any{
			{"inUserEmailsQuery": map[string]any{"userEmails": []string{email}}},
		},
	}
	if err := c.doJSON(ctx, http.MethodPost, "/v2/users", body, &out); err != nil {
		return "", false, fmt.Errorf("%w: find human by email: %v", identity.ErrZitadelUnavailable, err)
	}
	for _, item := range out.Result {
		if item.Human == nil || !item.Human.Email.IsVerified {
			continue
		}
		if !sameEmailIdentity(item.Human.Email.Email, email) {
			continue
		}
		if userID := strings.TrimSpace(item.UserID); userID != "" {
			return userID, true, nil
		}
	}
	return "", false, nil
}

// AddIDPLink binds an external identity to an existing Zitadel human. The
// headless Session API never auto-links, so this is the explicit link an
// auto-link-by-verified-email path must perform before creating the session.
func (c *Client) AddIDPLink(ctx context.Context, input identity.AddIDPLinkInput) error {
	userID := strings.TrimSpace(input.UserID)
	idpID := strings.TrimSpace(input.IDPID)
	externalUserID := strings.TrimSpace(input.ExternalUserID)
	if userID == "" || idpID == "" || externalUserID == "" {
		return fmt.Errorf("%w: user_id, idp_id, and external_user_id are required", identity.ErrInvalidInput)
	}
	body := map[string]any{
		"idpLink": map[string]any{
			"idpId":    idpID,
			"userId":   externalUserID,
			"userName": firstNonEmpty(strings.TrimSpace(input.ExternalUserName), externalUserID),
		},
	}
	if err := c.doJSON(ctx, http.MethodPost, "/v2/users/"+url.PathEscape(userID)+"/links", body, nil); err != nil {
		if zitadelRequestInvalid(err) {
			return fmt.Errorf("%w: add idp link: %v", identity.ErrInvalidInput, err)
		}
		return fmt.Errorf("%w: add idp link: %v", identity.ErrZitadelUnavailable, err)
	}
	return nil
}

// CreateSessionFromIDPIntent mints a Zitadel session from a completed IdP
// intent. The headless Session API only authenticates an external identity that
// is already linked to a Zitadel user (it does NOT auto-create or auto-link like
// the hosted login UI), so callers must resolve/link userID first; it is passed
// as the user check. Returns the session token like CreatePasswordSession.
func (c *Client) CreateSessionFromIDPIntent(ctx context.Context, intentID, intentToken, userID string, input identity.LoginSessionInput) (identity.LoginSession, error) {
	intentID = strings.TrimSpace(intentID)
	intentToken = strings.TrimSpace(intentToken)
	if intentID == "" || intentToken == "" {
		return identity.LoginSession{}, fmt.Errorf("%w: idp_intent_id and idp_intent_token are required", identity.ErrInvalidInput)
	}
	checks := map[string]any{
		"idpIntent": map[string]any{
			"idpIntentId":    intentID,
			"idpIntentToken": intentToken,
		},
	}
	if userID = strings.TrimSpace(userID); userID != "" {
		checks["user"] = map[string]any{"userId": userID}
	}
	createBody := map[string]any{"checks": checks}
	if input.Lifetime > 0 {
		createBody["lifetime"] = protobufDuration(input.Lifetime)
	}
	if userAgent := loginSessionUserAgent(input); len(userAgent) > 0 {
		createBody["userAgent"] = userAgent
	}
	var created sessionResponse
	if err := c.doJSON(ctx, http.MethodPost, "/v2/sessions", createBody, &created); err != nil {
		return identity.LoginSession{}, mapLoginSessionError("create idp session", err)
	}
	sessionID := strings.TrimSpace(created.SessionID)
	sessionToken := strings.TrimSpace(created.SessionToken)
	if sessionID == "" || sessionToken == "" {
		return identity.LoginSession{}, fmt.Errorf("%w: create idp session returned no session token", identity.ErrZitadelUnavailable)
	}
	expiresAt := time.Time{}
	if input.Lifetime > 0 {
		expiresAt = time.Now().UTC().Add(input.Lifetime)
	}
	return identity.LoginSession{SessionID: sessionID, SessionToken: sessionToken, ExpiresAt: expiresAt}, nil
}

// githubEmailFromRawInformation extracts the GitHub primary email from the
// provider's raw user payload. GitHub's /user response carries `email` (the
// public/primary email) without a verification flag, so treat a present email as
// verified provider truth; Zitadel's autoLinking compares against verified
// Zitadel emails.
func githubEmailFromRawInformation(raw map[string]any) (string, bool) {
	if raw == nil {
		return "", false
	}
	email, _ := raw["email"].(string)
	email = strings.TrimSpace(email)
	if email == "" {
		return "", false
	}
	return email, true
}

func (c *Client) StartDeviceAuthorization(ctx context.Context, input identity.StartDeviceAuthorizationInput) (identity.DeviceAuthorization, error) {
	clientID := strings.TrimSpace(input.ClientID)
	scopes := compactStrings(input.Scopes)
	if clientID == "" || len(scopes) == 0 {
		return identity.DeviceAuthorization{}, fmt.Errorf("%w: client_id and scope are required", identity.ErrInvalidInput)
	}
	values := url.Values{
		"client_id": {clientID},
		"scope":     {strings.Join(scopes, " ")},
	}
	var out struct {
		DeviceCode              string `json:"device_code"`
		UserCode                string `json:"user_code"`
		VerificationURI         string `json:"verification_uri"`
		VerificationURIComplete string `json:"verification_uri_complete"`
		ExpiresIn               int64  `json:"expires_in"`
		Interval                int64  `json:"interval"`
	}
	if err := c.doForm(ctx, "/oauth/v2/device_authorization", values, &out); err != nil {
		if zitadelRequestInvalid(err) {
			return identity.DeviceAuthorization{}, fmt.Errorf("%w: start device authorization: %v", identity.ErrInvalidInput, err)
		}
		return identity.DeviceAuthorization{}, fmt.Errorf("%w: start device authorization: %v", identity.ErrZitadelUnavailable, err)
	}
	out.DeviceCode = strings.TrimSpace(out.DeviceCode)
	out.UserCode = strings.TrimSpace(out.UserCode)
	if out.DeviceCode == "" || out.UserCode == "" || out.ExpiresIn <= 0 {
		return identity.DeviceAuthorization{}, fmt.Errorf("%w: device authorization returned incomplete response", identity.ErrZitadelUnavailable)
	}
	request, err := c.GetDeviceAuthorizationRequest(ctx, out.UserCode)
	if err != nil {
		return identity.DeviceAuthorization{}, err
	}
	return identity.DeviceAuthorization{
		DeviceCode:              out.DeviceCode,
		UserCode:                out.UserCode,
		VerificationURI:         strings.TrimSpace(out.VerificationURI),
		VerificationURIComplete: strings.TrimSpace(out.VerificationURIComplete),
		ExpiresIn:               time.Duration(out.ExpiresIn) * time.Second,
		Interval:                time.Duration(out.Interval) * time.Second,
		Request:                 request,
	}, nil
}

func (c *Client) GetDeviceAuthorizationRequest(ctx context.Context, userCode string) (identity.DeviceAuthorizationRequest, error) {
	userCode = strings.TrimSpace(userCode)
	if userCode == "" {
		return identity.DeviceAuthorizationRequest{}, fmt.Errorf("%w: user_code is required", identity.ErrInvalidInput)
	}
	var out struct {
		Request struct {
			ID          string   `json:"id"`
			ClientID    string   `json:"clientId"`
			Scope       []string `json:"scope"`
			AppName     string   `json:"appName"`
			ProjectName string   `json:"projectName"`
		} `json:"deviceAuthorizationRequest"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/v2/oidc/device_authorization/"+url.PathEscape(userCode), nil, &out); err != nil {
		return identity.DeviceAuthorizationRequest{}, fmt.Errorf("%w: get device authorization request: %v", identity.ErrZitadelUnavailable, err)
	}
	if strings.TrimSpace(out.Request.ID) == "" {
		return identity.DeviceAuthorizationRequest{}, fmt.Errorf("%w: get device authorization request returned no id", identity.ErrZitadelUnavailable)
	}
	return identity.DeviceAuthorizationRequest{
		ID:          strings.TrimSpace(out.Request.ID),
		ClientID:    strings.TrimSpace(out.Request.ClientID),
		Scopes:      compactStrings(out.Request.Scope),
		AppName:     strings.TrimSpace(out.Request.AppName),
		ProjectName: strings.TrimSpace(out.Request.ProjectName),
	}, nil
}

func (c *Client) ApproveDeviceAuthorization(ctx context.Context, deviceAuthorizationID string, session identity.LoginSession) error {
	deviceAuthorizationID = strings.TrimSpace(deviceAuthorizationID)
	session.SessionID = strings.TrimSpace(session.SessionID)
	session.SessionToken = strings.TrimSpace(session.SessionToken)
	if deviceAuthorizationID == "" || session.SessionID == "" || session.SessionToken == "" {
		return fmt.Errorf("%w: device_authorization_id, session_id, and session_token are required", identity.ErrInvalidInput)
	}
	body := map[string]any{
		"session": map[string]any{
			"sessionId":    session.SessionID,
			"sessionToken": session.SessionToken,
		},
	}
	if err := c.doJSON(ctx, http.MethodPost, "/v2/oidc/device_authorization/"+url.PathEscape(deviceAuthorizationID), body, nil); err != nil {
		return fmt.Errorf("%w: approve device authorization: %v", identity.ErrZitadelUnavailable, err)
	}
	return nil
}

func (c *Client) DenyDeviceAuthorization(ctx context.Context, deviceAuthorizationID string) error {
	deviceAuthorizationID = strings.TrimSpace(deviceAuthorizationID)
	if deviceAuthorizationID == "" {
		return fmt.Errorf("%w: device_authorization_id is required", identity.ErrInvalidInput)
	}
	if err := c.doJSON(ctx, http.MethodPost, "/v2/oidc/device_authorization/"+url.PathEscape(deviceAuthorizationID), map[string]any{"deny": map[string]any{}}, nil); err != nil {
		return fmt.Errorf("%w: deny device authorization: %v", identity.ErrZitadelUnavailable, err)
	}
	return nil
}

type sessionResponse struct {
	SessionID    string `json:"sessionId"`
	SessionToken string `json:"sessionToken"`
}

func loginSessionUserAgent(input identity.LoginSessionInput) map[string]any {
	userAgent := map[string]any{}
	if strings.TrimSpace(input.UserAgent) != "" {
		userAgent["description"] = strings.TrimSpace(input.UserAgent)
	}
	if strings.TrimSpace(input.IP) != "" {
		userAgent["ip"] = strings.TrimSpace(input.IP)
	}
	return userAgent
}

func protobufDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	seconds := d / time.Second
	if seconds <= 0 {
		seconds = 1
	}
	return strconv.FormatInt(int64(seconds), 10) + "s"
}

func mapLoginSessionError(action string, err error) error {
	if zitadelRequestInvalid(err) {
		return fmt.Errorf("%w: %s", identity.ErrInvalidCredentials, action)
	}
	return fmt.Errorf("%w: %s: %v", identity.ErrZitadelUnavailable, action, err)
}

type userSummary struct {
	Type        identity.MemberType
	Email       string
	LoginName   string
	GivenName   string
	FamilyName  string
	DisplayName string
	State       string
}

type humanBlock struct {
	Profile struct {
		GivenName   string `json:"givenName"`
		FamilyName  string `json:"familyName"`
		DisplayName string `json:"displayName"`
	} `json:"profile"`
	Email struct {
		Email      string `json:"email"`
		IsVerified bool   `json:"isVerified"`
	} `json:"email"`
}

type machineBlock struct {
	Name string `json:"name"`
}

type usersResponse struct {
	Details struct {
		TotalResult flexibleInt `json:"totalResult"`
	} `json:"details"`
	Result []struct {
		UserID             string        `json:"userId"`
		State              string        `json:"state"`
		Username           string        `json:"username"`
		PreferredLoginName string        `json:"preferredLoginName"`
		LoginNames         []string      `json:"loginNames"`
		Human              *humanBlock   `json:"human"`
		Machine            *machineBlock `json:"machine"`
	} `json:"result"`
}

func (c *Client) listUsersByOrganization(ctx context.Context, orgID string) (map[string]userSummary, error) {
	orgID = strings.TrimSpace(orgID)
	users := map[string]userSummary{}
	for {
		var out usersResponse
		body := map[string]any{
			"query": map[string]any{"limit": userPageLimit, "offset": len(users)},
			"queries": []map[string]any{
				{"organizationIdQuery": map[string]any{"organizationId": orgID}},
			},
		}
		if err := c.doJSON(ctx, http.MethodPost, "/v2/users", body, &out); err != nil {
			return nil, fmt.Errorf("%w: list organization users: %v", identity.ErrZitadelUnavailable, err)
		}
		page := userSummariesFromResponse(out)
		for userID, user := range page {
			users[userID] = user
		}
		if len(page) == 0 || len(page) < userPageLimit || (out.Details.TotalResult.Int() > 0 && out.Details.TotalResult.Int() <= len(users)) {
			return users, nil
		}
	}
}

func (c *Client) listUsers(ctx context.Context, userIDs []string) (map[string]userSummary, error) {
	userIDs = compactStrings(userIDs)
	if len(userIDs) == 0 {
		return map[string]userSummary{}, nil
	}
	var out usersResponse
	body := map[string]any{
		"query": map[string]any{"limit": 200},
		"queries": []map[string]any{
			{"inUserIdsQuery": map[string]any{"userIds": userIDs}},
		},
	}
	if err := c.doJSON(ctx, http.MethodPost, "/v2/users", body, &out); err != nil {
		return nil, fmt.Errorf("%w: list users: %v", identity.ErrZitadelUnavailable, err)
	}
	return userSummariesFromResponse(out), nil
}

func userSummariesFromResponse(out usersResponse) map[string]userSummary {
	users := make(map[string]userSummary, len(out.Result))
	for _, item := range out.Result {
		loginName := item.PreferredLoginName
		if loginName == "" && len(item.LoginNames) > 0 {
			loginName = item.LoginNames[0]
		}
		if loginName == "" {
			loginName = item.Username
		}
		summary := userSummary{State: item.State, LoginName: loginName}
		switch {
		case item.Human != nil:
			summary.Type = identity.MemberTypeHuman
			summary.Email = item.Human.Email.Email
			summary.GivenName = item.Human.Profile.GivenName
			summary.FamilyName = item.Human.Profile.FamilyName
			summary.DisplayName = firstNonEmpty(
				item.Human.Profile.DisplayName,
				strings.TrimSpace(item.Human.Profile.GivenName+" "+item.Human.Profile.FamilyName),
				loginName,
			)
		case item.Machine != nil:
			summary.Type = identity.MemberTypeMachine
			summary.DisplayName = firstNonEmpty(item.Machine.Name, loginName)
		default:
			summary.DisplayName = loginName
		}
		users[item.UserID] = summary
	}
	return users
}

type updateUserResponse struct {
	ChangeDate time.Time `json:"changeDate"`
}

type updateUserRequest struct {
	Human updateHumanUser `json:"human"`
}

type updateHumanUser struct {
	Profile setHumanProfile `json:"profile"`
}

type setHumanProfile struct {
	GivenName   string  `json:"givenName"`
	FamilyName  string  `json:"familyName"`
	DisplayName *string `json:"displayName,omitempty"`
}

func (c *Client) UpdateHumanProfile(ctx context.Context, subjectID string, input identity.HumanProfileUpdate) (identity.HumanProfile, error) {
	subjectID = strings.TrimSpace(subjectID)
	input.GivenName = strings.TrimSpace(input.GivenName)
	input.FamilyName = strings.TrimSpace(input.FamilyName)
	if subjectID == "" || input.GivenName == "" || input.FamilyName == "" {
		return identity.HumanProfile{}, fmt.Errorf("%w: subject_id, given_name, and family_name are required", identity.ErrInvalidInput)
	}
	if input.DisplayName != nil {
		displayName := strings.TrimSpace(*input.DisplayName)
		input.DisplayName = &displayName
	}
	body := updateUserRequest{
		Human: updateHumanUser{
			Profile: setHumanProfile{
				GivenName:   input.GivenName,
				FamilyName:  input.FamilyName,
				DisplayName: input.DisplayName,
			},
		},
	}
	var updated updateUserResponse
	if err := c.doJSON(ctx, http.MethodPatch, "/v2/users/"+url.PathEscape(subjectID), body, &updated); err != nil {
		if zitadelResourceAlreadyGone(err) {
			return identity.HumanProfile{}, fmt.Errorf("%w: human profile subject not found", identity.ErrMemberMissing)
		}
		return identity.HumanProfile{}, fmt.Errorf("%w: update human profile: %v", identity.ErrZitadelUnavailable, err)
	}
	if updated.ChangeDate.IsZero() {
		return identity.HumanProfile{}, fmt.Errorf("%w: update human profile returned no change date", identity.ErrZitadelUnavailable)
	}
	users, err := c.listUsers(ctx, []string{subjectID})
	if err != nil {
		return identity.HumanProfile{}, err
	}
	user, ok := users[subjectID]
	if !ok {
		return identity.HumanProfile{}, fmt.Errorf("%w: human profile subject not found", identity.ErrMemberMissing)
	}
	if user.Type != identity.MemberTypeHuman || strings.TrimSpace(user.Email) == "" {
		return identity.HumanProfile{}, fmt.Errorf("%w: subject is not a human user", identity.ErrInvalidInput)
	}
	return identity.HumanProfile{
		SubjectID:   subjectID,
		Email:       user.Email,
		GivenName:   user.GivenName,
		FamilyName:  user.FamilyName,
		DisplayName: user.DisplayName,
		SyncedAt:    updated.ChangeDate.UTC(),
	}, nil
}

type createUserResponse struct {
	ID        string `json:"id"`
	UserID    string `json:"userId"`
	EmailCode string `json:"emailCode"`
}

type createdHumanUser struct {
	UserID                string
	EmailVerificationCode string
}

func (c *Client) createHumanUser(ctx context.Context, orgID string, input identity.InviteMemberRequest, duplicateUserErr error) (createdHumanUser, error) {
	human := map[string]any{
		"profile": map[string]any{
			"givenName":  firstNonEmpty(input.GivenName, input.Email),
			"familyName": firstNonEmpty(input.FamilyName, "Member"),
		},
		"email": map[string]any{
			"email":      input.Email,
			"returnCode": map[string]any{},
		},
	}
	if input.Password != "" {
		human["password"] = map[string]any{
			"password":       input.Password,
			"changeRequired": false,
		}
	}
	body := map[string]any{
		"organizationId": orgID,
		"username":       input.Email,
		"human":          human,
	}
	var out createUserResponse
	if err := c.doJSON(ctx, http.MethodPost, "/v2/users/new", body, &out); err != nil {
		if zitadelUserAlreadyExists(err) {
			return createdHumanUser{}, fmt.Errorf("%w: create user: %v", duplicateUserErr, err)
		}
		if zitadelRequestInvalid(err) {
			return createdHumanUser{}, fmt.Errorf("%w: create user: %v", identity.ErrInvalidInput, err)
		}
		return createdHumanUser{}, fmt.Errorf("%w: create user: %v", identity.ErrZitadelUnavailable, err)
	}
	userID := firstNonEmpty(out.ID, out.UserID)
	if userID == "" {
		return createdHumanUser{}, fmt.Errorf("%w: create user returned no user id", identity.ErrZitadelUnavailable)
	}
	if strings.TrimSpace(out.EmailCode) == "" {
		return createdHumanUser{}, fmt.Errorf("%w: create user returned no email verification code", identity.ErrZitadelUnavailable)
	}
	return createdHumanUser{UserID: userID, EmailVerificationCode: strings.TrimSpace(out.EmailCode)}, nil
}

func (c *Client) verifyEmail(ctx context.Context, userID, verificationCode string) error {
	userID = strings.TrimSpace(userID)
	verificationCode = strings.TrimSpace(verificationCode)
	if userID == "" || verificationCode == "" {
		return fmt.Errorf("%w: user_id and verification_code are required", identity.ErrInvalidInput)
	}
	body := map[string]any{"verificationCode": verificationCode}
	if err := c.doJSON(ctx, http.MethodPost, "/v2/users/"+url.PathEscape(userID)+"/email/verify", body, nil); err != nil {
		if zitadelRequestInvalid(err) {
			return fmt.Errorf("%w: verify email: %v", identity.ErrInvalidInput, err)
		}
		return fmt.Errorf("%w: verify email: %v", identity.ErrZitadelUnavailable, err)
	}
	return nil
}

type flexibleInt int

func (n *flexibleInt) UnmarshalJSON(data []byte) error {
	data = bytes.Trim(data, `"`)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		*n = 0
		return nil
	}
	value, err := strconv.Atoi(string(data))
	if err != nil {
		return err
	}
	*n = flexibleInt(value)
	return nil
}

func (n flexibleInt) Int() int {
	return int(n)
}

func (c *Client) doJSON(ctx context.Context, method, path string, body any, out any) error {
	if c == nil || c.baseURL == nil {
		return errors.New("zitadel client is nil")
	}
	var reader io.Reader
	if body != nil {
		reqBody, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		reader = bytes.NewReader(reqBody)
	}
	reqURL := c.baseURL.ResolveReference(&url.URL{Path: path})
	req, err := http.NewRequestWithContext(ctx, method, reqURL.String(), reader)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.adminToken)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.hostHeader != "" {
		req.Host = c.hostHeader
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	respBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if readErr != nil {
		return fmt.Errorf("read response: %w", readErr)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return zitadelStatusError{StatusCode: resp.StatusCode, Body: strings.TrimSpace(string(respBody))}
	}
	if out == nil {
		return nil
	}
	if len(respBody) == 0 {
		return nil
	}
	if err := json.Unmarshal(respBody, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func (c *Client) doForm(ctx context.Context, path string, values url.Values, out any) error {
	if c == nil || c.baseURL == nil {
		return errors.New("zitadel client is nil")
	}
	reqURL := c.baseURL.ResolveReference(&url.URL{Path: path})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL.String(), strings.NewReader(values.Encode()))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if c.hostHeader != "" {
		req.Host = c.hostHeader
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	respBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if readErr != nil {
		return fmt.Errorf("read response: %w", readErr)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return zitadelStatusError{StatusCode: resp.StatusCode, Body: strings.TrimSpace(string(respBody))}
	}
	if out == nil || len(respBody) == 0 {
		return nil
	}
	if err := json.Unmarshal(respBody, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

type zitadelStatusError struct {
	StatusCode int
	Body       string
}

func (e zitadelStatusError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("status %d", e.StatusCode)
	}
	return fmt.Sprintf("status %d: %s", e.StatusCode, e.Body)
}

func zitadelRequestInvalid(err error) bool {
	var statusErr zitadelStatusError
	if !errors.As(err, &statusErr) {
		return false
	}
	switch statusErr.StatusCode {
	case http.StatusBadRequest, http.StatusNotFound, http.StatusConflict:
		return true
	default:
		return false
	}
}

func zitadelUserAlreadyExists(err error) bool {
	var statusErr zitadelStatusError
	if !errors.As(err, &statusErr) {
		return false
	}
	return statusErr.StatusCode == http.StatusConflict && strings.Contains(strings.ToLower(statusErr.Body), "user already exists")
}

func zitadelResourceAlreadyGone(err error) bool {
	if err == nil {
		return false
	}
	text := err.Error()
	return strings.Contains(text, "Errors.User.NotExisting") ||
		strings.Contains(text, "User could not be found") ||
		strings.Contains(text, "Errors.User.Key.NotExisting") ||
		strings.Contains(text, "Errors.User.Secret.NotExisting") ||
		strings.Contains(text, "Errors.Key.NotExisting")
}

func zitadelSessionAlreadyGone(err error) bool {
	if err == nil {
		return false
	}
	var statusErr zitadelStatusError
	if errors.As(err, &statusErr) && statusErr.StatusCode == http.StatusNotFound {
		return true
	}
	text := err.Error()
	return strings.Contains(text, "Errors.Session.NotExisting") ||
		strings.Contains(text, "Session could not be found") ||
		strings.Contains(text, "session.not_found")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
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
	return requiredEmail.IdentityKey == actualEmail.IdentityKey
}

func compactStrings(values []string) []string {
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
	sort.Strings(out)
	return out
}

type serviceAccountKeyResponse struct {
	KeyID      string `json:"keyId"`
	ID         string `json:"id"`
	KeyContent string `json:"keyContent"`
}

type serviceAccountSecretResponse struct {
	ClientSecret string `json:"clientSecret"`
}

func (c *Client) CreateServiceAccountCredential(ctx context.Context, orgID string, input identity.ServiceAccountCredentialInput) (string, identity.APICredentialIssuedMaterial, error) {
	orgID = strings.TrimSpace(orgID)
	input.ClientID = strings.TrimSpace(input.ClientID)
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	if orgID == "" || input.ClientID == "" || input.DisplayName == "" {
		return "", identity.APICredentialIssuedMaterial{}, fmt.Errorf("%w: org_id, client_id, and display_name are required", identity.ErrInvalidInput)
	}
	body := map[string]any{
		"organizationId": orgID,
		"username":       input.ClientID,
		"machine": map[string]any{
			"name":            input.DisplayName,
			"description":     "Verself API credential " + input.CredentialID,
			"accessTokenType": "ACCESS_TOKEN_TYPE_JWT",
		},
	}
	var out createUserResponse
	if err := c.doJSON(ctx, http.MethodPost, "/v2/users/new", body, &out); err != nil {
		return "", identity.APICredentialIssuedMaterial{}, fmt.Errorf("%w: create service account: %v", identity.ErrZitadelUnavailable, err)
	}
	subjectID := firstNonEmpty(out.ID, out.UserID)
	if subjectID == "" {
		return "", identity.APICredentialIssuedMaterial{}, fmt.Errorf("%w: create service account returned no user id", identity.ErrZitadelUnavailable)
	}
	material, err := c.AddServiceAccountCredential(ctx, identity.AddServiceAccountCredentialInput{
		SubjectID:  subjectID,
		ClientID:   input.ClientID,
		AuthMethod: input.AuthMethod,
		ExpiresAt:  input.ExpiresAt,
	})
	if err != nil {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
		defer cancel()
		return "", identity.APICredentialIssuedMaterial{}, errors.Join(err, c.DeactivateServiceAccount(cleanupCtx, subjectID))
	}
	return subjectID, material, nil
}

func (c *Client) AddServiceAccountCredential(ctx context.Context, input identity.AddServiceAccountCredentialInput) (identity.APICredentialIssuedMaterial, error) {
	input.SubjectID = strings.TrimSpace(input.SubjectID)
	input.ClientID = strings.TrimSpace(input.ClientID)
	if input.SubjectID == "" || input.ClientID == "" {
		return identity.APICredentialIssuedMaterial{}, fmt.Errorf("%w: subject_id and client_id are required", identity.ErrInvalidInput)
	}
	switch input.AuthMethod {
	case identity.APICredentialAuthMethodPrivateKeyJWT, "":
		return c.addServiceAccountKey(ctx, input)
	case identity.APICredentialAuthMethodClientSecret:
		return c.addServiceAccountSecret(ctx, input)
	default:
		return identity.APICredentialIssuedMaterial{}, fmt.Errorf("%w: unsupported auth_method %q", identity.ErrInvalidInput, input.AuthMethod)
	}
}

func (c *Client) RemoveServiceAccountCredential(ctx context.Context, subjectID string, secret identity.APICredentialSecret) error {
	subjectID = strings.TrimSpace(subjectID)
	if subjectID == "" || strings.TrimSpace(secret.ProviderKeyID) == "" {
		return fmt.Errorf("%w: subject_id and provider_key_id are required", identity.ErrInvalidInput)
	}
	switch secret.AuthMethod {
	case identity.APICredentialAuthMethodPrivateKeyJWT:
		path := "/v2/users/" + url.PathEscape(subjectID) + "/keys/" + url.PathEscape(secret.ProviderKeyID)
		if err := c.doJSON(ctx, http.MethodDelete, path, map[string]any{}, nil); err != nil {
			if zitadelResourceAlreadyGone(err) {
				return nil
			}
			return fmt.Errorf("%w: remove service account key: %v", identity.ErrZitadelUnavailable, err)
		}
	case identity.APICredentialAuthMethodClientSecret:
		path := "/v2/users/" + url.PathEscape(subjectID) + "/secret"
		if err := c.doJSON(ctx, http.MethodDelete, path, map[string]any{}, nil); err != nil {
			if zitadelResourceAlreadyGone(err) {
				return nil
			}
			return fmt.Errorf("%w: remove service account secret: %v", identity.ErrZitadelUnavailable, err)
		}
	default:
		return fmt.Errorf("%w: unsupported auth_method %q", identity.ErrInvalidInput, secret.AuthMethod)
	}
	return nil
}

func (c *Client) DeactivateServiceAccount(ctx context.Context, subjectID string) error {
	subjectID = strings.TrimSpace(subjectID)
	if subjectID == "" {
		return fmt.Errorf("%w: subject_id is required", identity.ErrInvalidInput)
	}
	if err := c.doJSON(ctx, http.MethodDelete, "/v2/users/"+url.PathEscape(subjectID), map[string]any{}, nil); err != nil {
		if zitadelResourceAlreadyGone(err) {
			return nil
		}
		return fmt.Errorf("%w: delete service account: %v", identity.ErrZitadelUnavailable, err)
	}
	return nil
}

func (c *Client) addServiceAccountKey(ctx context.Context, input identity.AddServiceAccountCredentialInput) (identity.APICredentialIssuedMaterial, error) {
	// ZITADEL v4.13.1 requires expirationDate on machine keys; product-level nil still means "no expiry".
	body := map[string]any{
		"expirationDate": effectiveKeyExpiration(input.ExpiresAt).Format(time.RFC3339Nano),
	}
	var out serviceAccountKeyResponse
	if err := c.doJSON(ctx, http.MethodPost, "/v2/users/"+url.PathEscape(input.SubjectID)+"/keys", body, &out); err != nil {
		return identity.APICredentialIssuedMaterial{}, fmt.Errorf("%w: add service account key: %v", identity.ErrZitadelUnavailable, err)
	}
	keyID := firstNonEmpty(out.KeyID, out.ID)
	if keyID == "" || strings.TrimSpace(out.KeyContent) == "" {
		return identity.APICredentialIssuedMaterial{}, fmt.Errorf("%w: add service account key returned incomplete material", identity.ErrZitadelUnavailable)
	}
	fingerprint, _ := identity.SecretHash(out.KeyContent)
	return identity.APICredentialIssuedMaterial{
		AuthMethod:  identity.APICredentialAuthMethodPrivateKeyJWT,
		ClientID:    input.ClientID,
		TokenURL:    c.tokenURL(),
		KeyID:       keyID,
		KeyContent:  out.KeyContent,
		Fingerprint: fingerprint,
	}, nil
}

func effectiveKeyExpiration(expiresAt *time.Time) time.Time {
	if expiresAt == nil {
		return zitadelMaxKeyExpiration
	}
	return expiresAt.UTC()
}

func (c *Client) addServiceAccountSecret(ctx context.Context, input identity.AddServiceAccountCredentialInput) (identity.APICredentialIssuedMaterial, error) {
	var out serviceAccountSecretResponse
	if err := c.doJSON(ctx, http.MethodPost, "/v2/users/"+url.PathEscape(input.SubjectID)+"/secret", map[string]any{}, &out); err != nil {
		return identity.APICredentialIssuedMaterial{}, fmt.Errorf("%w: add service account secret: %v", identity.ErrZitadelUnavailable, err)
	}
	if strings.TrimSpace(out.ClientSecret) == "" {
		return identity.APICredentialIssuedMaterial{}, fmt.Errorf("%w: add service account secret returned no secret", identity.ErrZitadelUnavailable)
	}
	fingerprint, _ := identity.SecretHash(out.ClientSecret)
	return identity.APICredentialIssuedMaterial{
		AuthMethod:   identity.APICredentialAuthMethodClientSecret,
		ClientID:     input.ClientID,
		TokenURL:     c.tokenURL(),
		ClientSecret: out.ClientSecret,
		Fingerprint:  fingerprint,
	}, nil
}

func (c *Client) tokenURL() string {
	if c == nil || c.baseURL == nil {
		return ""
	}
	if c.hostHeader != "" {
		return "https://" + c.hostHeader + "/oauth/v2/token"
	}
	return c.baseURL.ResolveReference(&url.URL{Path: "/oauth/v2/token"}).String()
}
