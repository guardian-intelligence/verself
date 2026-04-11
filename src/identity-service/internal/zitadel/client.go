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

const defaultTimeout = 5 * time.Second
const authorizationPageLimit = 1000

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
	Email       string
	LoginName   string
	DisplayName string
	State       string
}

type usersResponse struct {
	Result []struct {
		UserID             string `json:"userId"`
		State              string `json:"state"`
		PreferredLoginName string `json:"preferredLoginName"`
		Human              struct {
			Username           string   `json:"username"`
			LoginNames         []string `json:"loginNames"`
			PreferredLoginName string   `json:"preferredLoginName"`
			State              string   `json:"state"`
			Profile            struct {
				GivenName   string `json:"givenName"`
				FamilyName  string `json:"familyName"`
				DisplayName string `json:"displayName"`
			} `json:"profile"`
			Email struct {
				Email string `json:"email"`
			} `json:"email"`
		} `json:"human"`
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
		loginName := firstNonEmpty(item.Human.PreferredLoginName, item.PreferredLoginName, item.Human.Username)
		if loginName == "" && len(item.Human.LoginNames) > 0 {
			loginName = item.Human.LoginNames[0]
		}
		displayName := firstNonEmpty(item.Human.Profile.DisplayName, strings.TrimSpace(item.Human.Profile.GivenName+" "+item.Human.Profile.FamilyName), loginName)
		users[item.UserID] = userSummary{
			Email:       item.Human.Email.Email,
			LoginName:   loginName,
			DisplayName: displayName,
			State:       firstNonEmpty(item.Human.State, item.State),
		}
	}
	return users, nil
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
