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
	"strings"
	"time"

	opruntime "github.com/verself/operator-runtime/runtime"
	"go.opentelemetry.io/otel/attribute"
)

type platformRuntimeAuthAudienceSpec struct {
	ComponentName  string
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

type platformZitadelOIDCApp struct {
	ID           string
	ClientID     string
	ClientSecret string
}

type platformZitadelActionTarget struct {
	ID         string
	Name       string
	Endpoint   string
	Timeout    string
	SigningKey string
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

func platformRuntimeAuthAudienceSpecs() []platformRuntimeAuthAudienceSpec {
	return []platformRuntimeAuthAudienceSpec{
		{ComponentName: "analytics-service", CredentialPath: "/etc/credstore/analytics-service/auth-audience", Group: "analytics_service"},
		{ComponentName: "iam-service", CredentialPath: "/etc/credstore/iam-service/auth-audience", Group: "iam_service"},
		{ComponentName: "governance-service", CredentialPath: "/etc/credstore/governance-service/auth-audience", Group: "governance_service"},
		{ComponentName: "notifications-service", CredentialPath: "/etc/credstore/notifications-service/auth-audience", Group: "notifications_service"},
		{ComponentName: "object-storage-service", CredentialPath: "/etc/credstore/object-storage-service/auth-audience", Group: "object_storage_service"},
		{ComponentName: "profile-service", CredentialPath: "/etc/credstore/profile-service/auth-audience", Group: "profile_service"},
		{ComponentName: "projects-service", CredentialPath: "/etc/credstore/projects-service/auth-audience", Group: "projects_service"},
		{ComponentName: "source-code-hosting-service", CredentialPath: "/etc/credstore/source-code-hosting-service/auth-audience", Group: "source_code_hosting_service"},
		{ComponentName: "billing", CredentialPath: "/etc/credstore/billing/auth-audience", Group: "billing"},
		{ComponentName: "sandbox-rental", CredentialPath: "/etc/credstore/sandbox-rental/auth-audience", Group: "sandbox_rental"},
		{ComponentName: "secrets-service", CredentialPath: "/etc/credstore/secrets-service/auth-audience", Group: "secrets_service"},
		{ComponentName: "email-service", CredentialPath: "/etc/credstore/email-service/auth-audience", Group: "email_service"},
	}
}

func (r *platformRunner) ensurePlatformOwner() (platformZitadelUser, error) {
	var owner platformZitadelUser
	err := r.withSpan("platform.zitadel_owner.ensure", []attribute.KeyValue{
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
		project, err := client.EnsureProject(ctx, platformProductAPIProjectName)
		if err != nil {
			return err
		}
		if err := r.ensureRuntimeAuthAudienceCredentials(ctx, project.ID); err != nil {
			return err
		}
		if err := r.ensureBrowserOIDCApplication(ctx, client, project.ID); err != nil {
			return err
		}
		if err := r.ensureCLIOIDCApplication(ctx, client, project.ID); err != nil {
			return err
		}
		if err := r.ensureProductTokenClaimsAction(ctx, client); err != nil {
			return err
		}
		owner = user
		return nil
	})
	return owner, err
}

func (r *platformRunner) ensurePlatformRootOwner() (platformZitadelUser, error) {
	var owner platformZitadelUser
	err := r.withSpan("platform.zitadel_root_owner.ensure", []attribute.KeyValue{
		attribute.String("zitadel.host", r.cfg.ZitadelHost),
		attribute.String("verself.org_id", r.cfg.OrgIDText),
		attribute.String("verself.root_owner_email", r.cfg.RootOwnerEmail),
	}, func(ctx context.Context) error {
		client, closeFn, err := r.zitadelClient(ctx)
		if err != nil {
			return err
		}
		defer closeFn()

		user, found, err := client.FindHumanByEmail(ctx, r.cfg.RootOwnerEmail)
		if err != nil {
			return err
		}
		if user.ResourceOwner != "" && user.ResourceOwner != r.cfg.OrgIDText {
			return fmt.Errorf("platform root owner: %s belongs to Zitadel org %s, expected %s", r.cfg.RootOwnerEmail, user.ResourceOwner, r.cfg.OrgIDText)
		}
		if !found {
			displayName := rootOwnerDisplayName(r.cfg.RootOwnerEmail)
			user, err = client.CreateHumanInvite(ctx, r.cfg.OrgIDText, platformHumanInvite{
				Email:       r.cfg.RootOwnerEmail,
				Username:    r.cfg.RootOwnerEmail,
				DisplayName: displayName,
				GivenName:   ownerGivenName(displayName, rootOwnerLocalPart(r.cfg.RootOwnerEmail)),
				FamilyName:  ownerFamilyName(displayName),
			})
			if err != nil {
				return err
			}
			r.markChanged("zitadel.root_owner.created")
		}
		owner = user
		return nil
	})
	return owner, err
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
		rootOwner, found, err := client.FindHumanByEmail(ctx, r.cfg.RootOwnerEmail)
		if err != nil {
			return err
		}
		if !found {
			mismatches = append(mismatches, "root_owner.missing")
		} else if rootOwner.ResourceOwner != "" && rootOwner.ResourceOwner != r.cfg.OrgIDText {
			mismatches = append(mismatches, fmt.Sprintf("root_owner.resource_owner=%q", rootOwner.ResourceOwner))
		}
		project, err := client.ProjectByName(ctx, platformProductAPIProjectName)
		if err != nil {
			return err
		}
		credentialMismatches, err := r.platformRuntimeAuthAudienceCredentialMismatches(ctx, project.ID)
		if err != nil {
			return err
		}
		mismatches = append(mismatches, credentialMismatches...)
		browserAudienceMismatches, err := r.browserOIDCApplicationMismatches(ctx, client, project.ID)
		if err != nil {
			return err
		}
		mismatches = append(mismatches, browserAudienceMismatches...)
		cliAudienceMismatches, err := r.cliOIDCApplicationMismatches(ctx, client, project.ID)
		if err != nil {
			return err
		}
		mismatches = append(mismatches, cliAudienceMismatches...)
		tokenActionMismatches, err := r.productTokenClaimsActionMismatches(ctx, client)
		if err != nil {
			return err
		}
		mismatches = append(mismatches, tokenActionMismatches...)
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

func (r *platformRunner) ensureRuntimeAuthAudienceCredentials(ctx context.Context, projectID string) error {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return fmt.Errorf("product auth audience project ID is required")
	}
	for _, spec := range platformRuntimeAuthAudienceSpecs() {
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

func (r *platformRunner) ensureBrowserOIDCApplication(ctx context.Context, client platformZitadelClient, projectID string) error {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return fmt.Errorf("browser OIDC application project ID is required")
	}
	app, found, err := client.FindOIDCAppByName(ctx, projectID, browserOIDCAppName)
	if err != nil {
		return err
	}
	storedProjectID := ""
	if raw, readErr := opruntime.ReadRemoteFile(ctx, r.rt.SSH, browserOIDCCredstoreDir+"/oidc-project-id"); readErr == nil {
		storedProjectID = strings.TrimSpace(string(raw))
	}
	storedSecret := ""
	if raw, readErr := opruntime.ReadRemoteFile(ctx, r.rt.SSH, browserOIDCCredstoreDir+"/oidc-client-secret"); readErr == nil {
		storedSecret = strings.TrimSpace(string(raw))
	}
	needsSecretReissue := found && strings.TrimSpace(app.ClientSecret) == "" && (storedProjectID != projectID || storedSecret == "")
	if needsSecretReissue {
		if err := client.DeleteOIDCApp(ctx, projectID, app.ID); err != nil {
			return err
		}
		r.markChanged("iam.browser_oidc_app.reissued")
		found = false
	}
	if !found || strings.TrimSpace(app.ClientID) == "" {
		app, err = client.CreateBrowserOIDCApp(ctx, projectID, browserOIDCAppName, r.browserOIDCRedirectURIs(), r.browserOIDCPostLogoutRedirectURIs())
		if err != nil {
			return err
		}
		r.markChanged("iam.browser_oidc_app.created")
	} else if err := client.ReconcileBrowserOIDCApp(ctx, projectID, app.ID, browserOIDCAppName, r.browserOIDCRedirectURIs(), r.browserOIDCPostLogoutRedirectURIs()); err != nil {
		return err
	}
	values := map[string]string{
		"oidc-app-id":     app.ID + "\n",
		"oidc-client-id":  app.ClientID + "\n",
		"oidc-project-id": projectID + "\n",
	}
	if strings.TrimSpace(app.ClientSecret) != "" {
		values["oidc-client-secret"] = app.ClientSecret + "\n"
	} else if storedSecret == "" {
		return fmt.Errorf("browser OIDC app exists but no client secret is available in %s/oidc-client-secret", browserOIDCCredstoreDir)
	}
	for name, value := range values {
		changed, err := r.writeRootCredential(ctx, browserOIDCCredstoreDir+"/"+name, browserOIDCCredstoreGroup, value)
		if err != nil {
			return fmt.Errorf("write browser OIDC %s credential: %w", name, err)
		}
		if changed {
			r.markChanged("iam.browser_oidc_app." + name + ".updated")
		}
	}
	marker := fmt.Sprintf("app_id=%s\nclient_id=%s\nauth_method=OIDC_AUTH_METHOD_TYPE_POST\n", app.ID, app.ClientID)
	if changed, err := r.writeRootCredential(ctx, browserOIDCCredstoreDir+"/oidc-web-cutover", browserOIDCCredstoreGroup, marker); err != nil {
		return fmt.Errorf("write browser OIDC cutover marker: %w", err)
	} else if changed {
		r.markChanged("iam.browser_oidc_app.cutover_marker.updated")
	}
	return nil
}

func (r *platformRunner) ensureCLIOIDCApplication(ctx context.Context, client platformZitadelClient, projectID string) error {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return fmt.Errorf("CLI OIDC application project ID is required")
	}
	app, found, err := client.FindOIDCAppByName(ctx, projectID, cliOIDCAppName)
	if err != nil {
		return err
	}
	if !found || strings.TrimSpace(app.ClientID) == "" {
		app, err = client.CreateNativeOIDCApp(ctx, projectID, cliOIDCAppName)
		if err != nil {
			return err
		}
		r.markChanged("iam.cli_oidc_app.created")
	} else if err := client.ReconcileNativeOIDCApp(ctx, projectID, app.ID, cliOIDCAppName); err != nil {
		return err
	}
	values := map[string]string{
		"oidc-cli-app-id":     app.ID + "\n",
		"oidc-cli-client-id":  app.ClientID + "\n",
		"oidc-cli-project-id": projectID + "\n",
	}
	for name, value := range values {
		changed, err := r.writeRootCredential(ctx, browserOIDCCredstoreDir+"/"+name, browserOIDCCredstoreGroup, value)
		if err != nil {
			return fmt.Errorf("write CLI OIDC %s credential: %w", name, err)
		}
		if changed {
			r.markChanged("iam.cli_oidc_app." + name + ".updated")
		}
	}
	marker := fmt.Sprintf("app_id=%s\nclient_id=%s\nauth_method=OIDC_AUTH_METHOD_TYPE_NONE\n", app.ID, app.ClientID)
	if changed, err := r.writeRootCredential(ctx, browserOIDCCredstoreDir+"/oidc-cli-cutover", browserOIDCCredstoreGroup, marker); err != nil {
		return fmt.Errorf("write CLI OIDC cutover marker: %w", err)
	} else if changed {
		r.markChanged("iam.cli_oidc_app.cutover_marker.updated")
	}
	return nil
}

func (r *platformRunner) browserOIDCRedirectURIs() []string {
	return []string{"https://" + r.cfg.VerselfDomain + "/api/v1/auth/callback"}
}

func (r *platformRunner) browserOIDCPostLogoutRedirectURIs() []string {
	return []string{"https://" + r.cfg.VerselfDomain}
}

const (
	platformProductAPIProjectName = "verself-api"
	browserOIDCAppName            = "verself-web"
	cliOIDCAppName                = "verself-cli"
	browserOIDCCredstoreDir       = "/etc/credstore/iam-service"
	browserOIDCCredstoreGroup     = "iam_service"
	productTokenClaimsTargetName  = "verself-product-token-claims"
	productTokenClaimsActionPath  = "/internal/zitadel/actions/product-token-claims"
	productTokenClaimsFunction    = "preaccesstoken"
)

func (r *platformRunner) ensureProductTokenClaimsAction(ctx context.Context, client platformZitadelClient) error {
	endpoint := r.productTokenClaimsActionEndpoint()
	target, found, err := client.FindActionTargetByName(ctx, productTokenClaimsTargetName)
	if err != nil {
		return err
	}
	if !found {
		target, err = client.CreateProductTokenClaimsTarget(ctx, productTokenClaimsTargetName, endpoint)
		if err != nil {
			return err
		}
		r.markChanged("zitadel.product_token_claims_target.created")
	} else if target.Name != productTokenClaimsTargetName || target.Endpoint != endpoint || target.Timeout != "1s" {
		target, err = client.UpdateProductTokenClaimsTarget(ctx, target, productTokenClaimsTargetName, endpoint)
		if err != nil {
			return err
		}
		r.markChanged("zitadel.product_token_claims_target.updated")
	}
	if strings.TrimSpace(target.ID) == "" || strings.TrimSpace(target.SigningKey) == "" {
		return fmt.Errorf("zitadel product token claims target returned incomplete credentials")
	}
	if changed, err := r.writeRootCredential(ctx, browserOIDCCredstoreDir+"/zitadel-action-signing-key", browserOIDCCredstoreGroup, target.SigningKey+"\n"); err != nil {
		return fmt.Errorf("write Zitadel product token claims signing key: %w", err)
	} else if changed {
		r.markChanged("zitadel.product_token_claims_target.signing_key.updated")
	}
	if err := client.SetFunctionExecution(ctx, productTokenClaimsFunction, []string{target.ID}); err != nil {
		return err
	}
	return nil
}

func (r *platformRunner) productTokenClaimsActionEndpoint() string {
	return "https://" + r.cfg.VerselfDomain + productTokenClaimsActionPath
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

func (r *platformRunner) platformRuntimeAuthAudienceCredentialMismatches(ctx context.Context, projectID string) ([]string, error) {
	projectID = strings.TrimSpace(projectID)
	var mismatches []string
	for _, spec := range platformRuntimeAuthAudienceSpecs() {
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

func (r *platformRunner) browserOIDCApplicationMismatches(ctx context.Context, client platformZitadelClient, projectID string) ([]string, error) {
	var mismatches []string
	app, found, err := client.FindOIDCAppByName(ctx, projectID, browserOIDCAppName)
	if err != nil {
		return nil, err
	}
	if !found {
		mismatches = append(mismatches, "iam.browser_oidc_app.missing")
		return mismatches, nil
	}
	expected := map[string]string{
		"oidc-app-id":     app.ID,
		"oidc-client-id":  app.ClientID,
		"oidc-project-id": projectID,
	}
	for name, value := range expected {
		path := browserOIDCCredstoreDir + "/" + name
		raw, err := opruntime.ReadRemoteFile(ctx, r.rt.SSH, path)
		if err != nil {
			mismatches = append(mismatches, fmt.Sprintf("iam.browser_oidc_app.%s.read_error=%q", name, err.Error()))
			continue
		}
		if strings.TrimSpace(string(raw)) != value {
			mismatches = append(mismatches, "iam.browser_oidc_app."+name+".value_mismatch")
		}
		stat, err := r.remoteCredentialStat(ctx, path)
		if err != nil {
			mismatches = append(mismatches, fmt.Sprintf("iam.browser_oidc_app.%s.stat_error=%q", name, err.Error()))
			continue
		}
		if stat.Group != browserOIDCCredstoreGroup {
			mismatches = append(mismatches, fmt.Sprintf("iam.browser_oidc_app.%s.group=%q", name, stat.Group))
		}
		if stat.Mode != "640" {
			mismatches = append(mismatches, fmt.Sprintf("iam.browser_oidc_app.%s.mode=%q", name, stat.Mode))
		}
	}
	return mismatches, nil
}

func (r *platformRunner) cliOIDCApplicationMismatches(ctx context.Context, client platformZitadelClient, projectID string) ([]string, error) {
	var mismatches []string
	app, found, err := client.FindOIDCAppByName(ctx, projectID, cliOIDCAppName)
	if err != nil {
		return nil, err
	}
	if !found {
		mismatches = append(mismatches, "iam.cli_oidc_app.missing")
		return mismatches, nil
	}
	expected := map[string]string{
		"oidc-cli-app-id":     app.ID,
		"oidc-cli-client-id":  app.ClientID,
		"oidc-cli-project-id": projectID,
	}
	for name, value := range expected {
		path := browserOIDCCredstoreDir + "/" + name
		raw, err := opruntime.ReadRemoteFile(ctx, r.rt.SSH, path)
		if err != nil {
			mismatches = append(mismatches, fmt.Sprintf("iam.cli_oidc_app.%s.read_error=%q", name, err.Error()))
			continue
		}
		if strings.TrimSpace(string(raw)) != value {
			mismatches = append(mismatches, "iam.cli_oidc_app."+name+".value_mismatch")
		}
		stat, err := r.remoteCredentialStat(ctx, path)
		if err != nil {
			mismatches = append(mismatches, fmt.Sprintf("iam.cli_oidc_app.%s.stat_error=%q", name, err.Error()))
			continue
		}
		if stat.Group != browserOIDCCredstoreGroup {
			mismatches = append(mismatches, fmt.Sprintf("iam.cli_oidc_app.%s.group=%q", name, stat.Group))
		}
		if stat.Mode != "640" {
			mismatches = append(mismatches, fmt.Sprintf("iam.cli_oidc_app.%s.mode=%q", name, stat.Mode))
		}
	}
	return mismatches, nil
}

func (r *platformRunner) productTokenClaimsActionMismatches(ctx context.Context, client platformZitadelClient) ([]string, error) {
	var mismatches []string
	target, found, err := client.FindActionTargetByName(ctx, productTokenClaimsTargetName)
	if err != nil {
		return nil, err
	}
	if !found {
		mismatches = append(mismatches, "zitadel.product_token_claims_target.missing")
		return mismatches, nil
	}
	if target.Endpoint != r.productTokenClaimsActionEndpoint() {
		mismatches = append(mismatches, fmt.Sprintf("zitadel.product_token_claims_target.endpoint=%q", target.Endpoint))
	}
	if target.Timeout != "1s" {
		mismatches = append(mismatches, fmt.Sprintf("zitadel.product_token_claims_target.timeout=%q", target.Timeout))
	}
	raw, err := opruntime.ReadRemoteFile(ctx, r.rt.SSH, browserOIDCCredstoreDir+"/zitadel-action-signing-key")
	if err != nil {
		mismatches = append(mismatches, fmt.Sprintf("zitadel.product_token_claims_target.signing_key.read_error=%q", err.Error()))
	} else if strings.TrimSpace(string(raw)) != target.SigningKey {
		mismatches = append(mismatches, "zitadel.product_token_claims_target.signing_key.value_mismatch")
	}
	executionTargets, found, err := client.FunctionExecutionTargets(ctx, productTokenClaimsFunction)
	if err != nil {
		return nil, err
	}
	if !found {
		mismatches = append(mismatches, "zitadel.product_token_claims_execution.missing")
	} else if len(executionTargets) != 1 || executionTargets[0] != target.ID {
		mismatches = append(mismatches, fmt.Sprintf("zitadel.product_token_claims_execution.targets=%q", strings.Join(executionTargets, ",")))
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
	project, found, err := c.FindProjectByName(ctx, name)
	if err != nil {
		return platformZitadelProject{}, err
	}
	if !found {
		return platformZitadelProject{}, fmt.Errorf("zitadel project not found: %s", name)
	}
	return project, nil
}

func (c platformZitadelClient) EnsureProject(ctx context.Context, name string) (platformZitadelProject, error) {
	project, found, err := c.FindProjectByName(ctx, name)
	if err != nil {
		return platformZitadelProject{}, err
	}
	if !found {
		var out struct {
			ID string `json:"id"`
		}
		body := map[string]any{
			"name":                 strings.TrimSpace(name),
			"projectRoleAssertion": false,
			"projectRoleCheck":     false,
		}
		if err := c.doJSON(ctx, http.MethodPost, "/management/v1/projects", body, &out, false); err != nil {
			return platformZitadelProject{}, fmt.Errorf("create Zitadel project %s: %w", name, err)
		}
		if strings.TrimSpace(out.ID) == "" {
			return platformZitadelProject{}, fmt.Errorf("create Zitadel project %s returned no id", name)
		}
		return platformZitadelProject{ID: strings.TrimSpace(out.ID), Name: strings.TrimSpace(name), State: "PROJECT_STATE_ACTIVE"}, nil
	}
	body := map[string]any{
		"name":                 strings.TrimSpace(name),
		"projectRoleAssertion": false,
		"projectRoleCheck":     false,
	}
	if err := c.doJSON(ctx, http.MethodPut, "/management/v1/projects/"+url.PathEscape(project.ID), body, nil, false); err != nil && !isZitadelNoChanges(err) {
		return platformZitadelProject{}, fmt.Errorf("update Zitadel project %s defaults: %w", name, err)
	}
	return project, nil
}

func (c platformZitadelClient) FindProjectByName(ctx context.Context, name string) (platformZitadelProject, bool, error) {
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
		return platformZitadelProject{}, false, fmt.Errorf("search Zitadel project %s: %w", name, err)
	}
	if len(out.Result) == 0 || strings.TrimSpace(out.Result[0].ID) == "" {
		return platformZitadelProject{}, false, nil
	}
	item := out.Result[0]
	return platformZitadelProject{ID: item.ID, Name: item.Name, State: item.State}, true, nil
}

func (c platformZitadelClient) FindOIDCAppByName(ctx context.Context, projectID, name string) (platformZitadelOIDCApp, bool, error) {
	var out struct {
		Result []struct {
			ID         string `json:"id"`
			Name       string `json:"name"`
			OIDCConfig struct {
				ClientID string `json:"clientId"`
			} `json:"oidcConfig"`
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
	path := "/management/v1/projects/" + url.PathEscape(strings.TrimSpace(projectID)) + "/apps/_search"
	if err := c.doJSON(ctx, http.MethodPost, path, body, &out, false); err != nil {
		return platformZitadelOIDCApp{}, false, fmt.Errorf("search Zitadel OIDC app %s: %w", name, err)
	}
	if len(out.Result) == 0 || strings.TrimSpace(out.Result[0].ID) == "" {
		return platformZitadelOIDCApp{}, false, nil
	}
	item := out.Result[0]
	return platformZitadelOIDCApp{ID: item.ID, ClientID: item.OIDCConfig.ClientID}, true, nil
}

func (c platformZitadelClient) CreateBrowserOIDCApp(ctx context.Context, projectID, name string, redirectURIs, postLogoutRedirectURIs []string) (platformZitadelOIDCApp, error) {
	body := browserOIDCConfigBody(redirectURIs, postLogoutRedirectURIs)
	body["name"] = strings.TrimSpace(name)
	var out struct {
		AppID        string `json:"appId"`
		ClientID     string `json:"clientId"`
		ClientSecret string `json:"clientSecret"`
	}
	path := "/management/v1/projects/" + url.PathEscape(strings.TrimSpace(projectID)) + "/apps/oidc"
	if err := c.doJSON(ctx, http.MethodPost, path, body, &out, false); err != nil {
		return platformZitadelOIDCApp{}, fmt.Errorf("create Zitadel OIDC app %s: %w", name, err)
	}
	appID := strings.TrimSpace(out.AppID)
	clientID := strings.TrimSpace(out.ClientID)
	clientSecret := strings.TrimSpace(out.ClientSecret)
	if appID == "" || clientID == "" || clientSecret == "" {
		return platformZitadelOIDCApp{}, fmt.Errorf("create Zitadel OIDC app %s returned incomplete credentials", name)
	}
	return platformZitadelOIDCApp{ID: appID, ClientID: clientID, ClientSecret: clientSecret}, nil
}

func (c platformZitadelClient) CreateNativeOIDCApp(ctx context.Context, projectID, name string) (platformZitadelOIDCApp, error) {
	body := nativeOIDCConfigBody()
	body["name"] = strings.TrimSpace(name)
	var out struct {
		AppID    string `json:"appId"`
		ClientID string `json:"clientId"`
	}
	path := "/management/v1/projects/" + url.PathEscape(strings.TrimSpace(projectID)) + "/apps/oidc"
	if err := c.doJSON(ctx, http.MethodPost, path, body, &out, false); err != nil {
		return platformZitadelOIDCApp{}, fmt.Errorf("create Zitadel native OIDC app %s: %w", name, err)
	}
	appID := strings.TrimSpace(out.AppID)
	clientID := strings.TrimSpace(out.ClientID)
	if appID == "" || clientID == "" {
		return platformZitadelOIDCApp{}, fmt.Errorf("create Zitadel native OIDC app %s returned incomplete credentials", name)
	}
	return platformZitadelOIDCApp{ID: appID, ClientID: clientID}, nil
}

func (c platformZitadelClient) ReconcileBrowserOIDCApp(ctx context.Context, projectID, appID, name string, redirectURIs, postLogoutRedirectURIs []string) error {
	body := browserOIDCConfigBody(redirectURIs, postLogoutRedirectURIs)
	body["accessTokenRoleAssertion"] = false
	body["idTokenRoleAssertion"] = false
	body["idTokenUserinfoAssertion"] = true
	path := "/management/v1/projects/" + url.PathEscape(strings.TrimSpace(projectID)) + "/apps/" + url.PathEscape(strings.TrimSpace(appID)) + "/oidc_config"
	if err := c.doJSON(ctx, http.MethodPut, path, body, nil, false); err != nil && !isZitadelNoChanges(err) {
		return fmt.Errorf("update Zitadel OIDC app %s: %w", name, err)
	}
	return nil
}

func (c platformZitadelClient) ReconcileNativeOIDCApp(ctx context.Context, projectID, appID, name string) error {
	body := nativeOIDCConfigBody()
	body["accessTokenRoleAssertion"] = false
	body["idTokenRoleAssertion"] = false
	body["idTokenUserinfoAssertion"] = true
	path := "/management/v1/projects/" + url.PathEscape(strings.TrimSpace(projectID)) + "/apps/" + url.PathEscape(strings.TrimSpace(appID)) + "/oidc_config"
	if err := c.doJSON(ctx, http.MethodPut, path, body, nil, false); err != nil && !isZitadelNoChanges(err) {
		return fmt.Errorf("update Zitadel native OIDC app %s: %w", name, err)
	}
	return nil
}

func (c platformZitadelClient) DeleteOIDCApp(ctx context.Context, projectID, appID string) error {
	path := "/management/v1/projects/" + url.PathEscape(strings.TrimSpace(projectID)) + "/apps/" + url.PathEscape(strings.TrimSpace(appID))
	if err := c.doJSON(ctx, http.MethodDelete, path, nil, nil, false); err != nil {
		return fmt.Errorf("delete Zitadel OIDC app %s: %w", appID, err)
	}
	return nil
}

func browserOIDCConfigBody(redirectURIs, postLogoutRedirectURIs []string) map[string]any {
	return map[string]any{
		"redirectUris":           redirectURIs,
		"responseTypes":          []string{"OIDC_RESPONSE_TYPE_CODE"},
		"grantTypes":             []string{"OIDC_GRANT_TYPE_AUTHORIZATION_CODE", "OIDC_GRANT_TYPE_REFRESH_TOKEN", "OIDC_GRANT_TYPE_TOKEN_EXCHANGE"},
		"appType":                "OIDC_APP_TYPE_WEB",
		"authMethodType":         "OIDC_AUTH_METHOD_TYPE_POST",
		"postLogoutRedirectUris": postLogoutRedirectURIs,
		"devMode":                false,
		"accessTokenType":        "OIDC_TOKEN_TYPE_JWT",
	}
}

func nativeOIDCConfigBody() map[string]any {
	return map[string]any{
		"redirectUris":           []string{},
		"responseTypes":          []string{"OIDC_RESPONSE_TYPE_CODE"},
		"grantTypes":             []string{"OIDC_GRANT_TYPE_DEVICE_CODE", "OIDC_GRANT_TYPE_REFRESH_TOKEN"},
		"appType":                "OIDC_APP_TYPE_NATIVE",
		"authMethodType":         "OIDC_AUTH_METHOD_TYPE_NONE",
		"postLogoutRedirectUris": []string{},
		"devMode":                false,
		"accessTokenType":        "OIDC_TOKEN_TYPE_JWT",
	}
}

func (c platformZitadelClient) FindActionTargetByName(ctx context.Context, name string) (platformZitadelActionTarget, bool, error) {
	targets, err := c.ListActionTargets(ctx)
	if err != nil {
		return platformZitadelActionTarget{}, false, err
	}
	for _, target := range targets {
		if target.Name == name {
			return target, true, nil
		}
	}
	return platformZitadelActionTarget{}, false, nil
}

func (c platformZitadelClient) ListActionTargets(ctx context.Context) ([]platformZitadelActionTarget, error) {
	var out struct {
		Targets []struct {
			ID         string `json:"id"`
			Name       string `json:"name"`
			Timeout    string `json:"timeout"`
			Endpoint   string `json:"endpoint"`
			SigningKey string `json:"signingKey"`
		} `json:"targets"`
	}
	body := map[string]any{"pagination": map[string]any{"limit": 200}}
	if err := c.doJSON(ctx, http.MethodPost, "/v2/actions/targets/search", body, &out, false); err != nil {
		return nil, fmt.Errorf("search Zitadel action targets: %w", err)
	}
	targets := make([]platformZitadelActionTarget, 0, len(out.Targets))
	for _, item := range out.Targets {
		targets = append(targets, platformZitadelActionTarget{
			ID:         strings.TrimSpace(item.ID),
			Name:       strings.TrimSpace(item.Name),
			Endpoint:   strings.TrimSpace(item.Endpoint),
			Timeout:    strings.TrimSpace(item.Timeout),
			SigningKey: strings.TrimSpace(item.SigningKey),
		})
	}
	return targets, nil
}

func (c platformZitadelClient) CreateProductTokenClaimsTarget(ctx context.Context, name, endpoint string) (platformZitadelActionTarget, error) {
	var out struct {
		ID         string `json:"id"`
		SigningKey string `json:"signingKey"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/v2/actions/targets", productTokenClaimsTargetBody(name, endpoint), &out, false); err != nil {
		return platformZitadelActionTarget{}, fmt.Errorf("create Zitadel product token claims target: %w", err)
	}
	target := platformZitadelActionTarget{
		ID:         strings.TrimSpace(out.ID),
		Name:       strings.TrimSpace(name),
		Endpoint:   strings.TrimSpace(endpoint),
		Timeout:    "1s",
		SigningKey: strings.TrimSpace(out.SigningKey),
	}
	if target.ID == "" || target.SigningKey == "" {
		return platformZitadelActionTarget{}, fmt.Errorf("create Zitadel product token claims target returned incomplete credentials")
	}
	return target, nil
}

func (c platformZitadelClient) UpdateProductTokenClaimsTarget(ctx context.Context, target platformZitadelActionTarget, name, endpoint string) (platformZitadelActionTarget, error) {
	var out struct {
		SigningKey string `json:"signingKey"`
	}
	path := "/v2/actions/targets/" + url.PathEscape(strings.TrimSpace(target.ID))
	if err := c.doJSON(ctx, http.MethodPost, path, productTokenClaimsTargetBody(name, endpoint), &out, false); err != nil {
		return platformZitadelActionTarget{}, fmt.Errorf("update Zitadel product token claims target: %w", err)
	}
	target.Name = strings.TrimSpace(name)
	target.Endpoint = strings.TrimSpace(endpoint)
	target.Timeout = "1s"
	if signingKey := strings.TrimSpace(out.SigningKey); signingKey != "" {
		target.SigningKey = signingKey
	}
	return target, nil
}

func productTokenClaimsTargetBody(name, endpoint string) map[string]any {
	return map[string]any{
		"name":        strings.TrimSpace(name),
		"restCall":    map[string]any{"interruptOnError": true},
		"endpoint":    strings.TrimSpace(endpoint),
		"timeout":     "1s",
		"payloadType": "PAYLOAD_TYPE_JSON",
	}
}

func (c platformZitadelClient) SetFunctionExecution(ctx context.Context, function string, targets []string) error {
	body := map[string]any{
		"condition": map[string]any{
			"function": map[string]any{"name": strings.TrimSpace(function)},
		},
		"targets": targets,
	}
	if err := c.doJSON(ctx, http.MethodPut, "/v2/actions/executions", body, nil, false); err != nil {
		return fmt.Errorf("set Zitadel %s execution: %w", function, err)
	}
	return nil
}

func (c platformZitadelClient) FunctionExecutionTargets(ctx context.Context, function string) ([]string, bool, error) {
	var out struct {
		Executions []struct {
			Condition struct {
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"condition"`
			Targets []string `json:"targets"`
		} `json:"executions"`
	}
	body := map[string]any{"pagination": map[string]any{"limit": 200}}
	if err := c.doJSON(ctx, http.MethodPost, "/v2/actions/executions/search", body, &out, false); err != nil {
		return nil, false, fmt.Errorf("search Zitadel action executions: %w", err)
	}
	for _, execution := range out.Executions {
		if execution.Condition.Function.Name != function {
			continue
		}
		targets := make([]string, 0, len(execution.Targets))
		for _, target := range execution.Targets {
			if target = strings.TrimSpace(target); target != "" {
				targets = append(targets, target)
			}
		}
		return targets, true, nil
	}
	return nil, false, nil
}

func isZitadelNoChanges(err error) bool {
	var status platformZitadelStatusError
	return errors.As(err, &status) && status.Status == http.StatusBadRequest && strings.Contains(status.Body, "No changes")
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

func rootOwnerDisplayName(email string) string {
	local := rootOwnerLocalPart(email)
	if local == "" {
		return strings.TrimSpace(email)
	}
	return uppercaseASCIIHead(local)
}

func rootOwnerLocalPart(email string) string {
	local, _, found := strings.Cut(strings.TrimSpace(email), "@")
	if !found {
		return strings.TrimSpace(email)
	}
	local = strings.ReplaceAll(local, ".", " ")
	local = strings.ReplaceAll(local, "_", " ")
	local = strings.ReplaceAll(local, "-", " ")
	return strings.TrimSpace(local)
}

func uppercaseASCIIHead(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	first := value[0]
	if first >= 'a' && first <= 'z' {
		first -= 'a' - 'A'
	}
	return string(first) + value[1:]
}
