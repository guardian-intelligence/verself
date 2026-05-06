package main

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

	opruntime "github.com/verself/operator-runtime/runtime"
	"go.opentelemetry.io/otel/attribute"
)

type platformOwnerGrantSpec struct {
	ProjectName string
	RoleKeys    []string
}

type platformRuntimeAuthAudienceSpec struct {
	ComponentName  string
	ProjectName    string
	CredentialPath string
	Group          string
}

type platformZitadelClient struct {
	BaseURL    string
	HostHeader string
	Token      string
	Client     *http.Client
}

type platformZitadelOrg struct {
	ID            string
	Name          string
	PrimaryDomain string
	State         string
}

type platformZitadelUser struct {
	ID            string
	Email         string
	LoginName     string
	DisplayName   string
	ResourceOwner string
	State         string
}

type platformZitadelProject struct {
	ID    string
	Name  string
	State string
}

type platformZitadelAuthorization struct {
	ID             string
	UserID         string
	ProjectID      string
	OrganizationID string
	RoleKeys       []string
	State          string
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

type platformZitadelStatusError struct {
	Method string
	Path   string
	Status int
	Body   string
}

func (e platformZitadelStatusError) Error() string {
	return fmt.Sprintf("zitadel %s %s status %d: %s", e.Method, e.Path, e.Status, e.Body)
}

func platformOwnerGrantSpecs() []platformOwnerGrantSpec {
	return []platformOwnerGrantSpec{
		{ProjectName: "iam-service", RoleKeys: []string{"owner"}},
		{ProjectName: "sandbox-rental", RoleKeys: []string{"owner"}},
		{ProjectName: "secrets-service", RoleKeys: []string{"owner"}},
		{ProjectName: "forgejo", RoleKeys: []string{"forgejo_admin"}},
		{ProjectName: "mailbox-service", RoleKeys: []string{"mailbox_user"}},
	}
}

func platformRuntimeAuthAudienceSpecs() []platformRuntimeAuthAudienceSpec {
	return []platformRuntimeAuthAudienceSpec{
		{ComponentName: "iam-service", ProjectName: "iam-service", CredentialPath: "/etc/credstore/iam-service/auth-audience", Group: "iam_service"},
		{ComponentName: "governance-service", ProjectName: "iam-service", CredentialPath: "/etc/credstore/governance-service/auth-audience", Group: "governance_service"},
		{ComponentName: "notifications-service", ProjectName: "iam-service", CredentialPath: "/etc/credstore/notifications-service/auth-audience", Group: "notifications_service"},
		{ComponentName: "profile-service", ProjectName: "iam-service", CredentialPath: "/etc/credstore/profile-service/auth-audience", Group: "profile_service"},
		{ComponentName: "projects-service", ProjectName: "iam-service", CredentialPath: "/etc/credstore/projects-service/auth-audience", Group: "projects_service"},
		{ComponentName: "source-code-hosting-service", ProjectName: "iam-service", CredentialPath: "/etc/credstore/source-code-hosting-service/auth-audience", Group: "source_code_hosting_service"},
		{ComponentName: "billing", ProjectName: "billing", CredentialPath: "/etc/credstore/billing/auth-audience", Group: "billing"},
		{ComponentName: "sandbox-rental", ProjectName: "sandbox-rental", CredentialPath: "/etc/credstore/sandbox-rental/auth-audience", Group: "sandbox_rental"},
		{ComponentName: "secrets-service", ProjectName: "secrets-service", CredentialPath: "/etc/credstore/secrets-service/auth-audience", Group: "secrets_service"},
		{ComponentName: "mailbox-service", ProjectName: "mailbox-service", CredentialPath: "/etc/credstore/mailbox-service/auth-audience", Group: "mailbox_service"},
		{ComponentName: "verself-web.iam-service", ProjectName: "iam-service", CredentialPath: "/etc/credstore/verself-web/iam-service-auth-audience", Group: "verself-web"},
		{ComponentName: "verself-web.notifications-service", ProjectName: "iam-service", CredentialPath: "/etc/credstore/verself-web/notifications-service-auth-audience", Group: "verself-web"},
		{ComponentName: "verself-web.profile-service", ProjectName: "iam-service", CredentialPath: "/etc/credstore/verself-web/profile-service-auth-audience", Group: "verself-web"},
		{ComponentName: "verself-web.projects-service", ProjectName: "iam-service", CredentialPath: "/etc/credstore/verself-web/projects-service-auth-audience", Group: "verself-web"},
		{ComponentName: "verself-web.source-code-hosting-service", ProjectName: "iam-service", CredentialPath: "/etc/credstore/verself-web/source-code-hosting-service-auth-audience", Group: "verself-web"},
		{ComponentName: "verself-web.sandbox-rental", ProjectName: "sandbox-rental", CredentialPath: "/etc/credstore/verself-web/sandbox-rental-auth-audience", Group: "verself-web"},
	}
}

func (r *platformRunner) ensurePlatformOwner() error {
	return r.withSpan("platform.zitadel_owner.ensure", []attribute.KeyValue{
		attribute.String("zitadel.host", r.cfg.ZitadelHost),
		attribute.String("verself.org_id", r.cfg.OrgIDText),
		attribute.String("verself.owner_email", r.cfg.OwnerEmail),
	}, func(ctx context.Context) error {
		client, closeFn, err := r.zitadelClient(ctx)
		if err != nil {
			return err
		}
		defer closeFn()

		if changed, err := client.EnsureOrganizationName(ctx, r.cfg.OrgIDText, r.cfg.OrganizationName); err != nil {
			return err
		} else if changed {
			r.markChanged("zitadel.organization.updated")
		}

		user, found, err := client.FindHumanByEmail(ctx, r.cfg.OwnerEmail)
		if err != nil {
			return err
		}
		if found && user.ResourceOwner != "" && user.ResourceOwner != r.cfg.OrgIDText {
			return fmt.Errorf("platform owner: %s already belongs to Zitadel org %s, expected %s", r.cfg.OwnerEmail, user.ResourceOwner, r.cfg.OrgIDText)
		}
		if !found {
			user, err = client.CreateHumanInvite(ctx, r.cfg.OrgIDText, platformHumanInvite{
				Email:       r.cfg.OwnerEmail,
				Username:    r.cfg.OwnerEmail,
				DisplayName: r.cfg.OwnerName,
				GivenName:   ownerGivenName(r.cfg.OwnerName, r.cfg.OwnerAlias),
				FamilyName:  ownerFamilyName(r.cfg.OwnerName),
			})
			if err != nil {
				return err
			}
			r.markChanged("zitadel.owner.created")
		}
		projectIDs, err := r.resolvePlatformZitadelProjectIDs(ctx, client)
		if err != nil {
			return err
		}
		for _, spec := range platformOwnerGrantSpecs() {
			projectID := strings.TrimSpace(projectIDs[spec.ProjectName])
			if projectID == "" {
				return fmt.Errorf("zitadel project not resolved: %s", spec.ProjectName)
			}
			changed, err := client.UpsertAuthorization(ctx, r.cfg.OrgIDText, projectID, user.ID, spec.RoleKeys)
			if err != nil {
				return err
			}
			if changed {
				r.markChanged("zitadel.owner_grant." + spec.ProjectName + ".upserted")
			}
		}
		if err := r.ensureRuntimeAuthAudienceCredentials(ctx, projectIDs); err != nil {
			return err
		}
		if err := r.ensureBrowserAuthLoginAudienceCredential(ctx, projectIDs); err != nil {
			return err
		}
		return nil
	})
}

func (r *platformRunner) checkPlatformOwner(issues *[]string) platformBoundaryRow {
	row := platformBoundaryRow{Boundary: "zitadel.platform_owner", Status: "ok"}
	err := r.withSpan("platform.zitadel_owner.check", []attribute.KeyValue{
		attribute.String("zitadel.host", r.cfg.ZitadelHost),
		attribute.String("verself.org_id", r.cfg.OrgIDText),
		attribute.String("verself.owner_email", r.cfg.OwnerEmail),
	}, func(ctx context.Context) error {
		client, closeFn, err := r.zitadelClient(ctx)
		if err != nil {
			return err
		}
		defer closeFn()

		var mismatches []string
		org, found, err := client.OrganizationByID(ctx, r.cfg.OrgIDText)
		if err != nil {
			return err
		}
		if !found {
			*issues = append(*issues, "Zitadel organization is missing")
			row.Status = "missing"
			return nil
		}
		if org.Name != r.cfg.OrganizationName {
			mismatches = append(mismatches, fmt.Sprintf("org.name=%q", org.Name))
		}
		user, found, err := client.FindHumanByEmail(ctx, r.cfg.OwnerEmail)
		if err != nil {
			return err
		}
		if !found {
			*issues = append(*issues, "Zitadel platform owner is missing")
			row.Status = "missing"
			return nil
		}
		if user.ResourceOwner != "" && user.ResourceOwner != r.cfg.OrgIDText {
			mismatches = append(mismatches, fmt.Sprintf("owner.resource_owner=%q", user.ResourceOwner))
		}
		projectIDs, err := r.resolvePlatformZitadelProjectIDs(ctx, client)
		if err != nil {
			return err
		}
		for _, spec := range platformOwnerGrantSpecs() {
			projectID := strings.TrimSpace(projectIDs[spec.ProjectName])
			if projectID == "" {
				mismatches = append(mismatches, fmt.Sprintf("%s.project_missing", spec.ProjectName))
				continue
			}
			ok, roles, err := client.AuthorizationHasRoles(ctx, r.cfg.OrgIDText, projectID, user.ID, spec.RoleKeys)
			if err != nil {
				return err
			}
			if !ok {
				mismatches = append(mismatches, fmt.Sprintf("%s.roles=%q", spec.ProjectName, strings.Join(roles, ",")))
			}
		}
		credentialMismatches, err := r.platformRuntimeAuthAudienceCredentialMismatches(ctx, projectIDs)
		if err != nil {
			return err
		}
		mismatches = append(mismatches, credentialMismatches...)
		if len(mismatches) > 0 {
			*issues = append(*issues, "Zitadel platform owner mismatch: "+strings.Join(mismatches, ", "))
			row.Status = "mismatch"
			row.Detail = strings.Join(mismatches, ", ")
		}
		return nil
	})
	if err != nil {
		row.Status = "error"
		row.Detail = err.Error()
		*issues = append(*issues, err.Error())
	}
	return row
}

func (r *platformRunner) zitadelClient(ctx context.Context) (platformZitadelClient, func(), error) {
	rawToken, err := opruntime.ReadRemoteFile(ctx, r.rt.SSH, r.opts.zitadelAdminPATPath)
	if err != nil {
		return platformZitadelClient{}, func() {}, fmt.Errorf("zitadel: read admin PAT: %w", err)
	}
	token := strings.TrimSpace(string(rawToken))
	if token == "" {
		return platformZitadelClient{}, func() {}, fmt.Errorf("zitadel: admin PAT is empty")
	}
	forward, err := r.rt.SSH.Forward(ctx, "zitadel-http", r.opts.zitadelRemoteAddr)
	if err != nil {
		return platformZitadelClient{}, func() {}, fmt.Errorf("zitadel: open HTTP forward: %w", err)
	}
	closeFn := func() { _ = forward.Close() }
	client := platformZitadelClient{
		BaseURL:    "http://" + forward.ListenAddr,
		HostHeader: r.cfg.ZitadelHost,
		Token:      token,
		Client:     &http.Client{Timeout: 5 * time.Second},
	}
	return client, closeFn, nil
}

func (r *platformRunner) resolvePlatformZitadelProjectIDs(ctx context.Context, client platformZitadelClient) (map[string]string, error) {
	projectNames := map[string]struct{}{}
	for _, spec := range platformOwnerGrantSpecs() {
		projectNames[spec.ProjectName] = struct{}{}
	}
	for _, spec := range platformRuntimeAuthAudienceSpecs() {
		projectNames[spec.ProjectName] = struct{}{}
	}
	names := make([]string, 0, len(projectNames))
	for name := range projectNames {
		names = append(names, name)
	}
	sort.Strings(names)
	projectIDs := make(map[string]string, len(names))
	for _, name := range names {
		project, err := client.ProjectByName(ctx, name)
		if err != nil {
			return nil, err
		}
		projectIDs[name] = project.ID
	}
	return projectIDs, nil
}

func (r *platformRunner) ensureRuntimeAuthAudienceCredentials(ctx context.Context, projectIDs map[string]string) error {
	for _, spec := range platformRuntimeAuthAudienceSpecs() {
		projectID := strings.TrimSpace(projectIDs[spec.ProjectName])
		if projectID == "" {
			return fmt.Errorf("auth audience credential %s requires project %s", spec.ComponentName, spec.ProjectName)
		}
		value := projectID + "\n"
		changed, err := r.writeRootCredential(ctx, spec.CredentialPath, spec.Group, value)
		if err != nil {
			return fmt.Errorf("write %s auth audience credential: %w", spec.ComponentName, err)
		}
		if changed {
			r.markChanged("runtime.auth_audience." + spec.ComponentName + ".updated")
		}
	}
	return nil
}

func (r *platformRunner) ensureBrowserAuthLoginAudienceCredential(ctx context.Context, projectIDs map[string]string) error {
	iamID := strings.TrimSpace(projectIDs["iam-service"])
	sandboxID := strings.TrimSpace(projectIDs["sandbox-rental"])
	if iamID == "" || sandboxID == "" {
		return fmt.Errorf("browser auth login audiences require iam-service and sandbox-rental project IDs")
	}
	path := "/etc/credstore/iam-service/browser-auth-login-audiences"
	value := sandboxID + "," + iamID + "\n"
	changed, err := r.writeRootCredential(ctx, path, "iam_service", value)
	if err != nil {
		return fmt.Errorf("write browser auth login audiences credential: %w", err)
	}
	if changed {
		r.markChanged("iam.browser_auth_login_audiences.updated")
	}
	return nil
}

func (r *platformRunner) writeRootCredential(ctx context.Context, path, group, value string) (bool, error) {
	existing, err := opruntime.ReadRemoteFile(ctx, r.rt.SSH, path)
	if err == nil && string(existing) == value {
		stat, statErr := r.remoteCredentialStat(ctx, path)
		if statErr == nil && stat.Group == group && stat.Mode == "640" {
			return false, nil
		}
	}
	pathWord, err := opruntime.ShellWord(path)
	if err != nil {
		return false, err
	}
	groupWord, err := opruntime.ShellWord(group)
	if err != nil {
		return false, err
	}
	if err := r.rt.SSH.Run(ctx, "sudo /usr/bin/install -D -o root -g "+groupWord+" -m 0640 /dev/stdin "+pathWord, strings.NewReader(value), io.Discard, io.Discard); err != nil {
		return false, err
	}
	return true, nil
}

type remoteCredentialStat struct {
	Group string
	Mode  string
}

func (r *platformRunner) remoteCredentialStat(ctx context.Context, path string) (remoteCredentialStat, error) {
	pathWord, err := opruntime.ShellWord(path)
	if err != nil {
		return remoteCredentialStat{}, err
	}
	raw, err := r.rt.SSH.Exec(ctx, "sudo /usr/bin/stat -c '%G %a' -- "+pathWord)
	if err != nil {
		return remoteCredentialStat{}, err
	}
	fields := strings.Fields(string(raw))
	if len(fields) != 2 {
		return remoteCredentialStat{}, fmt.Errorf("unexpected stat output for %s: %q", path, strings.TrimSpace(string(raw)))
	}
	return remoteCredentialStat{Group: fields[0], Mode: fields[1]}, nil
}

func (r *platformRunner) platformRuntimeAuthAudienceCredentialMismatches(ctx context.Context, projectIDs map[string]string) ([]string, error) {
	var mismatches []string
	for _, spec := range platformRuntimeAuthAudienceSpecs() {
		projectID := strings.TrimSpace(projectIDs[spec.ProjectName])
		if projectID == "" {
			mismatches = append(mismatches, fmt.Sprintf("%s.auth_audience.project_missing", spec.ComponentName))
			continue
		}
		raw, err := opruntime.ReadRemoteFile(ctx, r.rt.SSH, spec.CredentialPath)
		if err != nil {
			mismatches = append(mismatches, fmt.Sprintf("%s.auth_audience.read_error=%q", spec.ComponentName, err.Error()))
			continue
		}
		if strings.TrimSpace(string(raw)) != projectID {
			mismatches = append(mismatches, fmt.Sprintf("%s.auth_audience=%q", spec.ComponentName, strings.TrimSpace(string(raw))))
		}
		stat, err := r.remoteCredentialStat(ctx, spec.CredentialPath)
		if err != nil {
			mismatches = append(mismatches, fmt.Sprintf("%s.auth_audience.stat_error=%q", spec.ComponentName, err.Error()))
			continue
		}
		if stat.Group != spec.Group {
			mismatches = append(mismatches, fmt.Sprintf("%s.auth_audience.group=%q", spec.ComponentName, stat.Group))
		}
		if stat.Mode != "640" {
			mismatches = append(mismatches, fmt.Sprintf("%s.auth_audience.mode=%q", spec.ComponentName, stat.Mode))
		}
	}
	return mismatches, nil
}

func (c platformZitadelClient) OrganizationByID(ctx context.Context, orgID string) (platformZitadelOrg, bool, error) {
	var out struct {
		Result []struct {
			ID            string `json:"id"`
			Name          string `json:"name"`
			PrimaryDomain string `json:"primaryDomain"`
			State         string `json:"state"`
		} `json:"result"`
	}
	body := map[string]any{
		"queries": []map[string]any{{
			"idQuery": map[string]string{"id": strings.TrimSpace(orgID)},
		}},
	}
	if err := c.doJSON(ctx, http.MethodPost, "/v2/organizations/_search", body, &out, false); err != nil {
		return platformZitadelOrg{}, false, err
	}
	if len(out.Result) == 0 || strings.TrimSpace(out.Result[0].ID) == "" {
		return platformZitadelOrg{}, false, nil
	}
	item := out.Result[0]
	return platformZitadelOrg{ID: item.ID, Name: item.Name, PrimaryDomain: item.PrimaryDomain, State: item.State}, true, nil
}

func (c platformZitadelClient) EnsureOrganizationName(ctx context.Context, orgID, name string) (bool, error) {
	org, found, err := c.OrganizationByID(ctx, orgID)
	if err != nil {
		return false, err
	}
	if !found {
		return false, fmt.Errorf("zitadel organization %s is missing", orgID)
	}
	if org.Name == name {
		return false, nil
	}
	body := map[string]string{"name": strings.TrimSpace(name)}
	if err := c.doJSON(ctx, http.MethodPost, "/v2/organizations/"+url.PathEscape(orgID), body, nil, false); err != nil {
		return false, fmt.Errorf("update Zitadel organization name: %w", err)
	}
	return true, nil
}

func (c platformZitadelClient) ProjectByName(ctx context.Context, name string) (platformZitadelProject, error) {
	var out struct {
		Result []struct {
			ID    string `json:"id"`
			Name  string `json:"name"`
			State string `json:"state"`
		} `json:"result"`
	}
	body := map[string]any{
		"queries": []map[string]any{{
			"nameQuery": map[string]string{
				"name":   strings.TrimSpace(name),
				"method": "TEXT_QUERY_METHOD_EQUALS",
			},
		}},
	}
	if err := c.doJSON(ctx, http.MethodPost, "/management/v1/projects/_search", body, &out, false); err != nil {
		return platformZitadelProject{}, fmt.Errorf("search Zitadel project %s: %w", name, err)
	}
	if len(out.Result) == 0 || strings.TrimSpace(out.Result[0].ID) == "" {
		return platformZitadelProject{}, fmt.Errorf("zitadel project not found: %s", name)
	}
	item := out.Result[0]
	return platformZitadelProject{ID: item.ID, Name: item.Name, State: item.State}, nil
}

func (c platformZitadelClient) FindHumanByEmail(ctx context.Context, email string) (platformZitadelUser, bool, error) {
	var out struct {
		Result []struct {
			UserID  string `json:"userId"`
			Details struct {
				ResourceOwner string `json:"resourceOwner"`
			} `json:"details"`
			State              string   `json:"state"`
			Username           string   `json:"username"`
			PreferredLoginName string   `json:"preferredLoginName"`
			LoginNames         []string `json:"loginNames"`
			Human              *struct {
				Profile struct {
					DisplayName string `json:"displayName"`
					GivenName   string `json:"givenName"`
					FamilyName  string `json:"familyName"`
				} `json:"profile"`
				Email struct {
					Email string `json:"email"`
				} `json:"email"`
			} `json:"human"`
		} `json:"result"`
	}
	body := map[string]any{
		"query": map[string]any{"limit": 10},
		"queries": []map[string]any{{
			"emailQuery": map[string]string{"emailAddress": strings.TrimSpace(email)},
		}},
	}
	if err := c.doJSON(ctx, http.MethodPost, "/v2/users", body, &out, false); err != nil {
		return platformZitadelUser{}, false, err
	}
	for _, item := range out.Result {
		if item.Human == nil || !strings.EqualFold(item.Human.Email.Email, email) {
			continue
		}
		loginName := firstNonEmpty(item.PreferredLoginName, item.Username)
		if loginName == "" && len(item.LoginNames) > 0 {
			loginName = item.LoginNames[0]
		}
		return platformZitadelUser{
			ID:            item.UserID,
			Email:         item.Human.Email.Email,
			LoginName:     loginName,
			DisplayName:   firstNonEmpty(item.Human.Profile.DisplayName, strings.TrimSpace(item.Human.Profile.GivenName+" "+item.Human.Profile.FamilyName), loginName),
			ResourceOwner: item.Details.ResourceOwner,
			State:         item.State,
		}, true, nil
	}
	return platformZitadelUser{}, false, nil
}

type platformHumanInvite struct {
	Email       string
	Username    string
	GivenName   string
	FamilyName  string
	DisplayName string
}

func (c platformZitadelClient) CreateHumanInvite(ctx context.Context, orgID string, input platformHumanInvite) (platformZitadelUser, error) {
	body := map[string]any{
		"organizationId": strings.TrimSpace(orgID),
		"username":       firstNonEmpty(input.Username, input.Email),
		"human": map[string]any{
			"profile": map[string]any{
				"givenName":   firstNonEmpty(input.GivenName, input.Email),
				"familyName":  firstNonEmpty(input.FamilyName, "Owner"),
				"displayName": firstNonEmpty(input.DisplayName, input.Email),
			},
			"email": map[string]any{
				"email":    strings.TrimSpace(input.Email),
				"sendCode": map[string]any{},
			},
		},
	}
	var out struct {
		ID     string `json:"id"`
		UserID string `json:"userId"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/v2/users/new", body, &out, false); err != nil {
		return platformZitadelUser{}, fmt.Errorf("create Zitadel owner invite: %w", err)
	}
	userID := firstNonEmpty(out.ID, out.UserID)
	if userID == "" {
		return platformZitadelUser{}, errors.New("create Zitadel owner invite returned no user id")
	}
	return platformZitadelUser{
		ID:            userID,
		Email:         strings.TrimSpace(input.Email),
		LoginName:     firstNonEmpty(input.Username, input.Email),
		DisplayName:   firstNonEmpty(input.DisplayName, input.Email),
		ResourceOwner: strings.TrimSpace(orgID),
		State:         "USER_STATE_ACTIVE",
	}, nil
}

func (c platformZitadelClient) UpsertAuthorization(ctx context.Context, orgID, projectID, userID string, roleKeys []string) (bool, error) {
	assignments, err := c.listAuthorizations(ctx,
		zitadelAuthorizationFilterInUserIDs(userID),
		zitadelAuthorizationFilterProjectID(projectID),
		zitadelAuthorizationFilterOrganizationID(orgID),
	)
	if err != nil {
		return false, err
	}
	expected := compactRoleKeys(roleKeys)
	for _, assignment := range assignments {
		if assignment.UserID != userID || assignment.ProjectID != projectID || assignment.OrganizationID != orgID {
			continue
		}
		if sameStringSet(assignment.RoleKeys, expected) {
			return false, nil
		}
		body := map[string]any{
			"id":       assignment.ID,
			"roleKeys": expected,
		}
		if err := c.doJSON(ctx, http.MethodPost, "/zitadel.authorization.v2.AuthorizationService/UpdateAuthorization", body, nil, true); err != nil {
			return false, fmt.Errorf("update Zitadel authorization %s: %w", assignment.ID, err)
		}
		return true, nil
	}
	body := map[string]any{
		"userId":         userID,
		"projectId":      projectID,
		"organizationId": orgID,
		"roleKeys":       expected,
	}
	if err := c.doJSON(ctx, http.MethodPost, "/zitadel.authorization.v2.AuthorizationService/CreateAuthorization", body, nil, true); err != nil {
		return false, fmt.Errorf("create Zitadel authorization: %w", err)
	}
	return true, nil
}

func (c platformZitadelClient) AuthorizationHasRoles(ctx context.Context, orgID, projectID, userID string, expected []string) (bool, []string, error) {
	assignments, err := c.listAuthorizations(ctx,
		zitadelAuthorizationFilterInUserIDs(userID),
		zitadelAuthorizationFilterProjectID(projectID),
		zitadelAuthorizationFilterOrganizationID(orgID),
	)
	if err != nil {
		return false, nil, err
	}
	for _, assignment := range assignments {
		if assignment.UserID == userID && assignment.ProjectID == projectID && assignment.OrganizationID == orgID {
			return sameStringSet(assignment.RoleKeys, expected), compactRoleKeys(assignment.RoleKeys), nil
		}
	}
	return false, nil, nil
}

func (c platformZitadelClient) listAuthorizations(ctx context.Context, filters ...map[string]any) ([]platformZitadelAuthorization, error) {
	var all []platformZitadelAuthorization
	for {
		var out struct {
			Pagination struct {
				TotalResult flexibleInt `json:"totalResult"`
			} `json:"pagination"`
			Authorizations []struct {
				ID   string `json:"id"`
				User struct {
					ID string `json:"id"`
				} `json:"user"`
				Project struct {
					ID string `json:"id"`
				} `json:"project"`
				Organization struct {
					ID string `json:"id"`
				} `json:"organization"`
				Roles []struct {
					Key string `json:"key"`
				} `json:"roles"`
				State string `json:"state"`
			} `json:"authorizations"`
		}
		body := map[string]any{
			"pagination": map[string]int{"limit": 1000, "offset": len(all)},
			"filters":    filters,
		}
		if err := c.doJSON(ctx, http.MethodPost, "/zitadel.authorization.v2.AuthorizationService/ListAuthorizations", body, &out, true); err != nil {
			return nil, fmt.Errorf("list Zitadel authorizations: %w", err)
		}
		page := make([]platformZitadelAuthorization, 0, len(out.Authorizations))
		for _, item := range out.Authorizations {
			authz := platformZitadelAuthorization{
				ID:             item.ID,
				UserID:         item.User.ID,
				ProjectID:      item.Project.ID,
				OrganizationID: item.Organization.ID,
				State:          item.State,
			}
			for _, role := range item.Roles {
				authz.RoleKeys = append(authz.RoleKeys, role.Key)
			}
			page = append(page, authz)
		}
		all = append(all, page...)
		if len(page) == 0 || int(out.Pagination.TotalResult) <= len(all) {
			return all, nil
		}
	}
}

func (c platformZitadelClient) doJSON(ctx context.Context, method, path string, body any, out any, connect bool) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.BaseURL, "/")+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.Token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if connect {
		req.Header.Set("Connect-Protocol-Version", "1")
	}
	if c.HostHeader != "" {
		req.Host = c.HostHeader
	}
	client := c.Client
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("zitadel %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return platformZitadelStatusError{Method: method, Path: path, Status: resp.StatusCode, Body: strings.TrimSpace(string(data))}
	}
	if out == nil || len(data) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("zitadel %s %s: decode response: %w", method, path, err)
	}
	return nil
}

func zitadelAuthorizationFilterInUserIDs(userIDs ...string) map[string]any {
	return map[string]any{
		"inUserIds": map[string]any{
			"ids": compactRoleKeys(userIDs),
		},
	}
}

func zitadelAuthorizationFilterProjectID(projectID string) map[string]any {
	return map[string]any{
		"projectId": map[string]any{
			"id": strings.TrimSpace(projectID),
		},
	}
}

func zitadelAuthorizationFilterOrganizationID(orgID string) map[string]any {
	return map[string]any{
		"organizationId": map[string]any{
			"id": strings.TrimSpace(orgID),
		},
	}
}

func ownerGivenName(name, fallback string) string {
	parts := strings.Fields(name)
	if len(parts) == 0 {
		return fallback
	}
	return parts[0]
}

func ownerFamilyName(name string) string {
	parts := strings.Fields(name)
	if len(parts) <= 1 {
		return "Owner"
	}
	return strings.Join(parts[1:], " ")
}

func compactRoleKeys(values []string) []string {
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

func sameStringSet(a, b []string) bool {
	left := compactRoleKeys(append([]string(nil), a...))
	right := compactRoleKeys(append([]string(nil), b...))
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
