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
	if name == "" || adminUserID == "" {
		return identity.DirectoryCreateOrganizationResult{}, fmt.Errorf("%w: organization name and admin user id are required", identity.ErrInvalidInput)
	}
	body := map[string]any{
		"name": name,
		"admins": []map[string]any{{
			"userId": adminUserID,
		}},
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

func (c *Client) InviteMember(ctx context.Context, orgID string, input identity.InviteMemberRequest) (identity.InviteMemberResult, error) {
	userID, err := c.createHumanUser(ctx, orgID, input)
	if err != nil {
		return identity.InviteMemberResult{}, err
	}
	return identity.InviteMemberResult{
		UserID: userID,
		Email:  input.Email,
		Status: "invited",
	}, nil
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
		Email string `json:"email"`
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
	ID     string `json:"id"`
	UserID string `json:"userId"`
}

func (c *Client) createHumanUser(ctx context.Context, orgID string, input identity.InviteMemberRequest) (string, error) {
	body := map[string]any{
		"organizationId": orgID,
		"username":       input.Email,
		"human": map[string]any{
			"profile": map[string]any{
				"givenName":  firstNonEmpty(input.GivenName, input.Email),
				"familyName": firstNonEmpty(input.FamilyName, "Member"),
			},
			"email": map[string]any{
				"email":    input.Email,
				"sendCode": map[string]any{},
			},
		},
	}
	var out createUserResponse
	if err := c.doJSON(ctx, http.MethodPost, "/v2/users/new", body, &out); err != nil {
		return "", fmt.Errorf("%w: create user: %v", identity.ErrZitadelUnavailable, err)
	}
	if out.ID != "" {
		return out.ID, nil
	}
	if out.UserID != "" {
		return out.UserID, nil
	}
	return "", fmt.Errorf("%w: create user returned no user id", identity.ErrZitadelUnavailable)
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
	reqBody, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}
	reqURL := c.baseURL.ResolveReference(&url.URL{Path: path})
	req, err := http.NewRequestWithContext(ctx, method, reqURL.String(), bytes.NewReader(reqBody))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.adminToken)
	req.Header.Set("Content-Type", "application/json")
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
		return fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
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
