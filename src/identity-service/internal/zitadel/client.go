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

	"github.com/forge-metal/identity-service/internal/identity"
)

const (
	defaultTimeout         = 5 * time.Second
	authorizationPageLimit = 1000
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

func (c *Client) ListMembers(ctx context.Context, orgID, projectID string) ([]identity.Member, error) {
	orgID = strings.TrimSpace(orgID)
	projectID = strings.TrimSpace(projectID)
	if orgID == "" || projectID == "" {
		return nil, fmt.Errorf("%w: org_id and project_id are required", identity.ErrInvalidInput)
	}
	assignments, err := c.listAuthorizations(ctx, authorizationFilterProjectID(projectID), authorizationFilterOrganizationID(orgID))
	if err != nil {
		return nil, err
	}
	membersByID := map[string]identity.Member{}
	for _, assignment := range assignments {
		if assignment.ProjectID != projectID || assignment.OrganizationID != orgID || assignment.UserID == "" || !assignment.Active() {
			continue
		}
		member := membersByID[assignment.UserID]
		member.UserID = assignment.UserID
		member.DisplayName = firstNonEmpty(member.DisplayName, assignment.UserDisplayName)
		member.RoleKeys = append(member.RoleKeys, assignment.RoleKeys...)
		membersByID[assignment.UserID] = member
	}
	if len(membersByID) == 0 {
		return []identity.Member{}, nil
	}
	users, err := c.listUsers(ctx, keys(membersByID))
	if err != nil {
		return nil, err
	}
	for userID, user := range users {
		member := membersByID[userID]
		member.Type = user.Type
		member.Email = user.Email
		member.LoginName = user.LoginName
		member.DisplayName = firstNonEmpty(member.DisplayName, user.DisplayName, user.LoginName, user.Email)
		member.State = user.State
		membersByID[userID] = member
	}
	members := make([]identity.Member, 0, len(membersByID))
	for _, member := range membersByID {
		member.RoleKeys = compactStrings(member.RoleKeys)
		members = append(members, member)
	}
	sort.Slice(members, func(i, j int) bool {
		return members[i].DisplayName < members[j].DisplayName
	})
	return members, nil
}

func (c *Client) InviteMember(ctx context.Context, orgID, projectID string, input identity.InviteMemberRequest) (identity.InviteMemberResult, error) {
	userID, err := c.createHumanUser(ctx, orgID, input)
	if err != nil {
		return identity.InviteMemberResult{}, err
	}
	if _, err := c.upsertAuthorization(ctx, orgID, projectID, userID, input.RoleKeys); err != nil {
		return identity.InviteMemberResult{}, err
	}
	return identity.InviteMemberResult{
		UserID:   userID,
		Email:    input.Email,
		RoleKeys: compactStrings(input.RoleKeys),
		Status:   "invited",
	}, nil
}

func (c *Client) UpdateMemberRoles(ctx context.Context, orgID, projectID, userID string, roleKeys []string) (identity.Member, error) {
	assignment, err := c.upsertAuthorization(ctx, orgID, projectID, userID, roleKeys)
	if err != nil {
		return identity.Member{}, err
	}
	users, err := c.listUsers(ctx, []string{userID})
	if err != nil {
		return identity.Member{}, err
	}
	user := users[userID]
	return identity.Member{
		UserID:      userID,
		Type:        user.Type,
		Email:       user.Email,
		LoginName:   user.LoginName,
		DisplayName: firstNonEmpty(user.DisplayName, user.LoginName, user.Email, assignment.UserDisplayName),
		State:       user.State,
		RoleKeys:    compactStrings(roleKeys),
	}, nil
}

type authorization struct {
	ID              string
	UserID          string
	UserDisplayName string
	ProjectID       string
	OrganizationID  string
	RoleKeys        []string
	State           string
}

func (a authorization) Active() bool {
	return a.State == "" || a.State == "STATE_ACTIVE"
}

type authorizationListRequest struct {
	Pagination pagination       `json:"pagination"`
	Filters    []map[string]any `json:"filters,omitempty"`
}

type authorizationListResponse struct {
	Pagination struct {
		TotalResult  flexibleInt `json:"totalResult"`
		AppliedLimit flexibleInt `json:"appliedLimit"`
	} `json:"pagination"`
	Authorizations []struct {
		ID           string `json:"id"`
		User         entity `json:"user"`
		Project      entity `json:"project"`
		Organization entity `json:"organization"`
		Roles        []role `json:"roles"`
		State        string `json:"state"`
	} `json:"authorizations"`
}

type entity struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
}

type role struct {
	Key string `json:"key"`
}

func (c *Client) listAuthorizations(ctx context.Context, filters ...map[string]any) ([]authorization, error) {
	assignments := []authorization{}
	for {
		var out authorizationListResponse
		err := c.doJSON(ctx, http.MethodPost, "/zitadel.authorization.v2.AuthorizationService/ListAuthorizations", authorizationListRequest{
			Pagination: pagination{Limit: authorizationPageLimit, Offset: len(assignments)},
			Filters:    filters,
		}, &out, true)
		if err != nil {
			return nil, fmt.Errorf("%w: list authorizations: %v", identity.ErrZitadelUnavailable, err)
		}
		page := authorizationPage(out)
		assignments = append(assignments, page...)
		if len(page) == 0 || out.Pagination.TotalResult.Int() <= len(assignments) {
			return assignments, nil
		}
	}
}

func (c *Client) upsertAuthorization(ctx context.Context, orgID, projectID, userID string, roleKeys []string) (authorization, error) {
	assignments, err := c.listAuthorizations(
		ctx,
		authorizationFilterInUserIDs(userID),
		authorizationFilterProjectID(projectID),
		authorizationFilterOrganizationID(orgID),
	)
	if err != nil {
		return authorization{}, err
	}
	for _, assignment := range assignments {
		if assignment.UserID == userID && assignment.ProjectID == projectID && assignment.OrganizationID == orgID {
			return assignment, c.updateAuthorization(ctx, assignment.ID, roleKeys)
		}
	}
	if err := c.createAuthorization(ctx, orgID, projectID, userID, roleKeys); err != nil {
		return authorization{}, err
	}
	return authorization{UserID: userID, ProjectID: projectID, OrganizationID: orgID, RoleKeys: compactStrings(roleKeys)}, nil
}

func (c *Client) createAuthorization(ctx context.Context, orgID, projectID, userID string, roleKeys []string) error {
	body := map[string]any{
		"userId":         userID,
		"projectId":      projectID,
		"organizationId": orgID,
		"roleKeys":       compactStrings(roleKeys),
	}
	if err := c.doJSON(ctx, http.MethodPost, "/zitadel.authorization.v2.AuthorizationService/CreateAuthorization", body, nil, true); err != nil {
		return fmt.Errorf("%w: create authorization: %v", identity.ErrZitadelUnavailable, err)
	}
	return nil
}

func (c *Client) updateAuthorization(ctx context.Context, authorizationID string, roleKeys []string) error {
	body := map[string]any{
		"id":       authorizationID,
		"roleKeys": compactStrings(roleKeys),
	}
	if err := c.doJSON(ctx, http.MethodPost, "/zitadel.authorization.v2.AuthorizationService/UpdateAuthorization", body, nil, true); err != nil {
		return fmt.Errorf("%w: update authorization: %v", identity.ErrZitadelUnavailable, err)
	}
	return nil
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
	if err := c.doJSON(ctx, http.MethodPost, "/v2/users", body, &out, false); err != nil {
		return nil, fmt.Errorf("%w: list users: %v", identity.ErrZitadelUnavailable, err)
	}
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
	return users, nil
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
	if err := c.doJSON(ctx, http.MethodPatch, "/v2/users/"+url.PathEscape(subjectID), body, &updated, false); err != nil {
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
	if err := c.doJSON(ctx, http.MethodPost, "/v2/users/new", body, &out, false); err != nil {
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

type pagination struct {
	Offset int `json:"offset,omitempty"`
	Limit  int `json:"limit"`
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

func authorizationFilterInUserIDs(userIDs ...string) map[string]any {
	return map[string]any{
		"inUserIds": map[string]any{
			"ids": compactStrings(userIDs),
		},
	}
}

func authorizationFilterProjectID(projectID string) map[string]any {
	return map[string]any{
		"projectId": map[string]any{
			"id": strings.TrimSpace(projectID),
		},
	}
}

func authorizationFilterOrganizationID(orgID string) map[string]any {
	return map[string]any{
		"organizationId": map[string]any{
			"id": strings.TrimSpace(orgID),
		},
	}
}

func authorizationPage(out authorizationListResponse) []authorization {
	assignments := make([]authorization, 0, len(out.Authorizations))
	for _, item := range out.Authorizations {
		assignment := authorization{
			ID:              item.ID,
			UserID:          item.User.ID,
			UserDisplayName: firstNonEmpty(item.User.DisplayName, item.User.Name),
			ProjectID:       item.Project.ID,
			OrganizationID:  item.Organization.ID,
			State:           item.State,
			RoleKeys:        make([]string, 0, len(item.Roles)),
		}
		for _, role := range item.Roles {
			assignment.RoleKeys = append(assignment.RoleKeys, role.Key)
		}
		assignments = append(assignments, assignment)
	}
	return assignments
}

func (c *Client) doJSON(ctx context.Context, method, path string, body any, out any, connect bool) error {
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
	if connect {
		req.Header.Set("Connect-Protocol-Version", "1")
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

func keys[T any](values map[string]T) []string {
	out := make([]string, 0, len(values))
	for key := range values {
		out = append(out, key)
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
			"description":     "Forge Metal API credential " + input.CredentialID,
			"accessTokenType": "ACCESS_TOKEN_TYPE_JWT",
		},
	}
	var out createUserResponse
	if err := c.doJSON(ctx, http.MethodPost, "/v2/users/new", body, &out, false); err != nil {
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
		if err := c.doJSON(ctx, http.MethodDelete, path, map[string]any{}, nil, false); err != nil {
			if zitadelResourceAlreadyGone(err) {
				return nil
			}
			return fmt.Errorf("%w: remove service account key: %v", identity.ErrZitadelUnavailable, err)
		}
	case identity.APICredentialAuthMethodClientSecret:
		path := "/v2/users/" + url.PathEscape(subjectID) + "/secret"
		if err := c.doJSON(ctx, http.MethodDelete, path, map[string]any{}, nil, false); err != nil {
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
	if err := c.doJSON(ctx, http.MethodDelete, "/v2/users/"+url.PathEscape(subjectID), map[string]any{}, nil, false); err != nil {
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
	if err := c.doJSON(ctx, http.MethodPost, "/v2/users/"+url.PathEscape(input.SubjectID)+"/keys", body, &out, false); err != nil {
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
	if err := c.doJSON(ctx, http.MethodPost, "/v2/users/"+url.PathEscape(input.SubjectID)+"/secret", map[string]any{}, &out, false); err != nil {
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
