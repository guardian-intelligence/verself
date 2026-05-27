package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	verselfotel "github.com/verself/observability/otel"
)

const (
	defaultProjectName                         = "verself-api"
	defaultBrowserAppName                      = "verself-web"
	defaultCLIAppName                          = "verself-cli"
	defaultClaimsTargetName                    = "verself-product-token-claims"
	defaultClaimsActionPath                    = "/internal/zitadel/actions/product-token-claims"
	productTokenClaimsFunction                 = "preaccesstoken"
	defaultZitadelBaseURL                      = "http://127.0.0.1:8085"
	defaultZitadelAdminPATPath                 = "/etc/zitadel/admin.pat"
	defaultIAMCredstoreDir                     = "/etc/credstore/iam-service"
	defaultIAMCredstoreGroup                   = "iam_service"
	desiredPasswordMinLength                   = 15
	desiredPasswordLockoutAttempts             = 10
	credentialMode                 os.FileMode = 0o640
)

type config struct {
	zitadelBaseURL      string
	zitadelHost         string
	adminPATPath        string
	verselfDomain       string
	iamCredstoreDir     string
	iamCredstoreGroup   string
	projectName         string
	browserAppName      string
	cliAppName          string
	claimsTargetName    string
	claimsActionPath    string
	zitadelReadyWait    time.Duration
	zitadelReadyBackoff time.Duration
}

type zitadelClient struct {
	baseURL    string
	hostHeader string
	token      string
	client     *http.Client
}

type zitadelProject struct {
	ID    string
	Name  string
	State string
}

type zitadelOIDCApp struct {
	ID           string
	ClientID     string
	ClientSecret string
}

type actionTarget struct {
	ID         string
	Name       string
	Endpoint   string
	Timeout    string
	SigningKey string
}

type statusError struct {
	Method string
	Path   string
	Status int
	Body   string
}

type audienceSpec struct {
	ComponentName  string
	CredentialPath string
	Group          string
}

type policyInt int

type passwordComplexityPolicy struct {
	MinLength    policyInt `json:"minLength"`
	HasUppercase bool      `json:"hasUppercase"`
	HasLowercase bool      `json:"hasLowercase"`
	HasNumber    bool      `json:"hasNumber"`
	HasSymbol    bool      `json:"hasSymbol"`
}

type passwordAgePolicy struct {
	MaxAgeDays     policyInt `json:"maxAgeDays"`
	ExpireWarnDays policyInt `json:"expireWarnDays"`
}

type lockoutPolicy struct {
	MaxPasswordAttempts policyInt `json:"maxPasswordAttempts"`
	MaxOTPAttempts      policyInt `json:"maxOtpAttempts"`
}

func (e statusError) Error() string {
	return fmt.Sprintf("zitadel %s %s status %d: %s", e.Method, e.Path, e.Status, e.Body)
}

func (v *policyInt) UnmarshalJSON(data []byte) error {
	raw := strings.TrimSpace(string(data))
	if raw == "" || raw == "null" {
		*v = 0
		return nil
	}
	var quoted string
	if err := json.Unmarshal(data, &quoted); err == nil {
		raw = strings.TrimSpace(quoted)
		if raw == "" {
			*v = 0
			return nil
		}
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return fmt.Errorf("parse policy integer %q: %w", raw, err)
	}
	*v = policyInt(n)
	return nil
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "auth-control-plane-apply: "+err.Error())
		os.Exit(1)
	}
}

func run(args []string) error {
	cfg := config{
		zitadelBaseURL:      envOr("AUTH_CONTROL_PLANE_ZITADEL_BASE_URL", defaultZitadelBaseURL),
		zitadelHost:         envOr("AUTH_CONTROL_PLANE_ZITADEL_HOST", ""),
		adminPATPath:        envOr("AUTH_CONTROL_PLANE_ADMIN_PAT_PATH", defaultZitadelAdminPATPath),
		verselfDomain:       envOr("AUTH_CONTROL_PLANE_VERSELF_DOMAIN", ""),
		iamCredstoreDir:     envOr("AUTH_CONTROL_PLANE_IAM_CREDSTORE_DIR", defaultIAMCredstoreDir),
		iamCredstoreGroup:   envOr("AUTH_CONTROL_PLANE_IAM_CREDSTORE_GROUP", defaultIAMCredstoreGroup),
		projectName:         defaultProjectName,
		browserAppName:      defaultBrowserAppName,
		cliAppName:          defaultCLIAppName,
		claimsTargetName:    defaultClaimsTargetName,
		claimsActionPath:    defaultClaimsActionPath,
		zitadelReadyWait:    60 * time.Second,
		zitadelReadyBackoff: time.Second,
	}
	fs := flag.NewFlagSet("auth-control-plane-apply", flag.ContinueOnError)
	fs.StringVar(&cfg.zitadelBaseURL, "zitadel-base-url", cfg.zitadelBaseURL, "Local Zitadel base URL.")
	fs.StringVar(&cfg.zitadelHost, "zitadel-host", cfg.zitadelHost, "Zitadel HTTP Host header.")
	fs.StringVar(&cfg.adminPATPath, "admin-pat-path", cfg.adminPATPath, "Zitadel admin PAT path.")
	fs.StringVar(&cfg.verselfDomain, "verself-domain", cfg.verselfDomain, "Product apex domain.")
	fs.StringVar(&cfg.iamCredstoreDir, "iam-credstore-dir", cfg.iamCredstoreDir, "IAM credstore directory.")
	fs.StringVar(&cfg.iamCredstoreGroup, "iam-credstore-group", cfg.iamCredstoreGroup, "IAM credstore group.")
	fs.StringVar(&cfg.projectName, "project-name", cfg.projectName, "Zitadel project for product API audiences.")
	fs.StringVar(&cfg.browserAppName, "browser-app-name", cfg.browserAppName, "Browser OIDC app name.")
	fs.StringVar(&cfg.cliAppName, "cli-app-name", cfg.cliAppName, "CLI OIDC app name.")
	fs.StringVar(&cfg.claimsTargetName, "claims-target-name", cfg.claimsTargetName, "Product token claims action target name.")
	fs.StringVar(&cfg.claimsActionPath, "claims-action-path", cfg.claimsActionPath, "Product token claims action path.")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected positional args: %s", strings.Join(fs.Args(), " "))
	}
	if err := cfg.validate(); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	shutdown, err := initTelemetry(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "auth-control-plane-apply: telemetry disabled: %v\n", err)
	} else {
		defer func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := shutdown(shutdownCtx); err != nil {
				fmt.Fprintf(os.Stderr, "auth-control-plane-apply: telemetry shutdown: %v\n", err)
			}
		}()
	}
	return apply(ctx, cfg)
}

func (cfg config) validate() error {
	missing := []string{}
	for name, value := range map[string]string{
		"--zitadel-base-url":    cfg.zitadelBaseURL,
		"--zitadel-host":        cfg.zitadelHost,
		"--admin-pat-path":      cfg.adminPATPath,
		"--verself-domain":      cfg.verselfDomain,
		"--iam-credstore-dir":   cfg.iamCredstoreDir,
		"--iam-credstore-group": cfg.iamCredstoreGroup,
		"--project-name":        cfg.projectName,
		"--browser-app-name":    cfg.browserAppName,
		"--cli-app-name":        cfg.cliAppName,
		"--claims-target-name":  cfg.claimsTargetName,
		"--claims-action-path":  cfg.claimsActionPath,
	} {
		if strings.TrimSpace(value) == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required flags: %s", strings.Join(missing, ", "))
	}
	if _, err := url.ParseRequestURI("https://" + cfg.verselfDomain); err != nil {
		return fmt.Errorf("invalid --verself-domain: %w", err)
	}
	return nil
}

func initTelemetry(ctx context.Context) (func(context.Context) error, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	shutdown, _, err := verselfotel.Init(ctx, verselfotel.Config{ServiceName: "auth-control-plane-apply"})
	if err != nil {
		return nil, err
	}
	return shutdown, nil
}

func apply(ctx context.Context, cfg config) error {
	tracer := otel.Tracer("github.com/verself/infrastructure-components/zitadel/cmd/auth-control-plane-apply")
	ctx, span := tracer.Start(ctx, "auth_control_plane.apply")
	defer span.End()
	span.SetAttributes(
		attribute.String("zitadel.host", cfg.zitadelHost),
		attribute.String("verself.domain", cfg.verselfDomain),
	)
	if err := waitForZitadel(ctx, cfg); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	token, err := readSecret(cfg.adminPATPath)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	client := zitadelClient{
		baseURL:    cfg.zitadelBaseURL,
		hostHeader: cfg.zitadelHost,
		token:      token,
		client:     &http.Client{Timeout: 5 * time.Second},
	}
	if err := client.EnsurePasswordPolicies(ctx); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	project, err := client.EnsureProject(ctx, cfg.projectName)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	if err := ensureRuntimeAudiences(project.ID, cfg); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	if err := ensureBrowserOIDCApplication(ctx, client, project.ID, cfg); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	if err := ensureCLIOIDCApplication(ctx, client, project.ID, cfg); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	if err := ensureProductTokenClaimsAction(ctx, client, cfg); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	span.SetStatus(codes.Ok, "")
	fmt.Println("auth-control-plane-apply: changed state reconciled")
	return nil
}

func waitForZitadel(ctx context.Context, cfg config) error {
	deadline := time.Now().Add(cfg.zitadelReadyWait)
	var lastErr error
	for time.Now().Before(deadline) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(cfg.zitadelBaseURL, "/")+"/debug/healthz", nil)
		if err != nil {
			return err
		}
		req.Host = cfg.zitadelHost
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
			lastErr = fmt.Errorf("zitadel health status %d", resp.StatusCode)
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(cfg.zitadelReadyBackoff):
		}
	}
	if lastErr == nil {
		lastErr = errors.New("deadline elapsed")
	}
	return fmt.Errorf("wait for Zitadel health: %w", lastErr)
}

func ensureRuntimeAudiences(projectID string, cfg config) error {
	for _, spec := range runtimeAudienceSpecs(cfg) {
		if err := writeCredential(spec.CredentialPath, spec.Group, projectID+"\n"); err != nil {
			return fmt.Errorf("write %s auth audience: %w", spec.ComponentName, err)
		}
	}
	return nil
}

func runtimeAudienceSpecs(cfg config) []audienceSpec {
	return []audienceSpec{
		{ComponentName: "analytics-service", CredentialPath: "/etc/credstore/analytics-service/auth-audience", Group: "analytics_service"},
		{ComponentName: "distribution-service", CredentialPath: "/etc/credstore/distribution-service/auth-audience", Group: "distribution_service"},
		{ComponentName: "iam-service", CredentialPath: filepath.Join(cfg.iamCredstoreDir, "auth-audience"), Group: cfg.iamCredstoreGroup},
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

func (c zitadelClient) EnsurePasswordPolicies(ctx context.Context) error {
	if err := c.EnsurePasswordComplexityPolicy(ctx); err != nil {
		return err
	}
	if err := c.EnsurePasswordAgePolicy(ctx); err != nil {
		return err
	}
	if err := c.EnsureLockoutPolicy(ctx); err != nil {
		return err
	}
	return nil
}

func (c zitadelClient) EnsurePasswordComplexityPolicy(ctx context.Context) error {
	var out struct {
		Policy passwordComplexityPolicy `json:"policy"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/admin/v1/policies/password/complexity", nil, &out); err != nil {
		return fmt.Errorf("read Zitadel password complexity policy: %w", err)
	}
	desired := desiredPasswordComplexityPolicy()
	if out.Policy == desired {
		return nil
	}
	if err := c.doJSON(ctx, http.MethodPut, "/admin/v1/policies/password/complexity", desiredPasswordComplexityPolicyBody(), nil); err != nil && !isNoChanges(err) {
		return fmt.Errorf("update Zitadel password complexity policy: %w", err)
	}
	return nil
}

func (c zitadelClient) EnsurePasswordAgePolicy(ctx context.Context) error {
	var out struct {
		Policy passwordAgePolicy `json:"policy"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/admin/v1/policies/password/age", nil, &out); err != nil {
		return fmt.Errorf("read Zitadel password age policy: %w", err)
	}
	desired := desiredPasswordAgePolicy()
	if out.Policy == desired {
		return nil
	}
	if err := c.doJSON(ctx, http.MethodPut, "/admin/v1/policies/password/age", desiredPasswordAgePolicyBody(), nil); err != nil && !isNoChanges(err) {
		return fmt.Errorf("update Zitadel password age policy: %w", err)
	}
	return nil
}

func (c zitadelClient) EnsureLockoutPolicy(ctx context.Context) error {
	var out struct {
		Policy lockoutPolicy `json:"policy"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/admin/v1/policies/lockout", nil, &out); err != nil {
		return fmt.Errorf("read Zitadel lockout policy: %w", err)
	}
	desired := desiredLockoutPolicy()
	if out.Policy == desired {
		return nil
	}
	if err := c.doJSON(ctx, http.MethodPut, "/admin/v1/policies/password/lockout", desiredLockoutPolicyBody(), nil); err != nil && !isNoChanges(err) {
		return fmt.Errorf("update Zitadel lockout policy: %w", err)
	}
	return nil
}

func desiredPasswordComplexityPolicy() passwordComplexityPolicy {
	return passwordComplexityPolicy{
		MinLength:    desiredPasswordMinLength,
		HasUppercase: false,
		HasLowercase: false,
		HasNumber:    false,
		HasSymbol:    false,
	}
}

func desiredPasswordAgePolicy() passwordAgePolicy {
	return passwordAgePolicy{MaxAgeDays: 0, ExpireWarnDays: 0}
}

func desiredLockoutPolicy() lockoutPolicy {
	return lockoutPolicy{MaxPasswordAttempts: desiredPasswordLockoutAttempts, MaxOTPAttempts: desiredPasswordLockoutAttempts}
}

func desiredPasswordComplexityPolicyBody() map[string]any {
	policy := desiredPasswordComplexityPolicy()
	return map[string]any{
		"minLength":    int(policy.MinLength),
		"hasUppercase": policy.HasUppercase,
		"hasLowercase": policy.HasLowercase,
		"hasNumber":    policy.HasNumber,
		"hasSymbol":    policy.HasSymbol,
	}
}

func desiredPasswordAgePolicyBody() map[string]any {
	policy := desiredPasswordAgePolicy()
	return map[string]any{
		"maxAgeDays":     int(policy.MaxAgeDays),
		"expireWarnDays": int(policy.ExpireWarnDays),
	}
}

func desiredLockoutPolicyBody() map[string]any {
	policy := desiredLockoutPolicy()
	return map[string]any{
		"maxPasswordAttempts": int(policy.MaxPasswordAttempts),
		"maxOtpAttempts":      int(policy.MaxOTPAttempts),
	}
}

func ensureBrowserOIDCApplication(ctx context.Context, client zitadelClient, projectID string, cfg config) error {
	app, found, err := client.FindOIDCAppByName(ctx, projectID, cfg.browserAppName)
	if err != nil {
		return err
	}
	storedProjectID, _ := readTrimmed(filepath.Join(cfg.iamCredstoreDir, "oidc-project-id"))
	storedSecret, _ := readTrimmed(filepath.Join(cfg.iamCredstoreDir, "oidc-client-secret"))
	if found && strings.TrimSpace(app.ClientSecret) == "" && (storedProjectID != projectID || storedSecret == "") {
		if err := client.DeleteOIDCApp(ctx, projectID, app.ID); err != nil {
			return err
		}
		found = false
	}
	redirectURIs := []string{"https://" + cfg.verselfDomain + "/api/v1/auth/callback"}
	postLogout := []string{"https://" + cfg.verselfDomain}
	loginBaseURI := "https://" + cfg.verselfDomain
	if !found || strings.TrimSpace(app.ClientID) == "" {
		app, err = client.CreateBrowserOIDCApp(ctx, projectID, cfg.browserAppName, redirectURIs, postLogout, loginBaseURI)
		if err != nil {
			return err
		}
	} else if err := client.ReconcileBrowserOIDCApp(ctx, projectID, app.ID, cfg.browserAppName, redirectURIs, postLogout, loginBaseURI); err != nil {
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
		return fmt.Errorf("browser OIDC app exists but no client secret is available in %s/oidc-client-secret", cfg.iamCredstoreDir)
	}
	for name, value := range values {
		if err := writeCredential(filepath.Join(cfg.iamCredstoreDir, name), cfg.iamCredstoreGroup, value); err != nil {
			return fmt.Errorf("write browser OIDC %s: %w", name, err)
		}
	}
	marker := fmt.Sprintf("app_id=%s\nclient_id=%s\nauth_method=OIDC_AUTH_METHOD_TYPE_POST\n", app.ID, app.ClientID)
	return writeCredential(filepath.Join(cfg.iamCredstoreDir, "oidc-web-cutover"), cfg.iamCredstoreGroup, marker)
}

func ensureCLIOIDCApplication(ctx context.Context, client zitadelClient, projectID string, cfg config) error {
	app, found, err := client.FindOIDCAppByName(ctx, projectID, cfg.cliAppName)
	if err != nil {
		return err
	}
	if !found || strings.TrimSpace(app.ClientID) == "" {
		app, err = client.CreateNativeOIDCApp(ctx, projectID, cfg.cliAppName, "https://"+cfg.verselfDomain)
		if err != nil {
			return err
		}
	} else if err := client.ReconcileNativeOIDCApp(ctx, projectID, app.ID, cfg.cliAppName, "https://"+cfg.verselfDomain); err != nil {
		return err
	}
	values := map[string]string{
		"oidc-cli-app-id":     app.ID + "\n",
		"oidc-cli-client-id":  app.ClientID + "\n",
		"oidc-cli-project-id": projectID + "\n",
		"oidc-cli-cutover":    fmt.Sprintf("app_id=%s\nclient_id=%s\nauth_method=OIDC_AUTH_METHOD_TYPE_NONE\n", app.ID, app.ClientID),
	}
	for name, value := range values {
		if err := writeCredential(filepath.Join(cfg.iamCredstoreDir, name), cfg.iamCredstoreGroup, value); err != nil {
			return fmt.Errorf("write CLI OIDC %s: %w", name, err)
		}
	}
	return nil
}

func ensureProductTokenClaimsAction(ctx context.Context, client zitadelClient, cfg config) error {
	endpoint := "https://" + cfg.verselfDomain + cfg.claimsActionPath
	target, found, err := client.FindActionTargetByName(ctx, cfg.claimsTargetName)
	if err != nil {
		return err
	}
	if !found {
		target, err = client.CreateProductTokenClaimsTarget(ctx, cfg.claimsTargetName, endpoint)
		if err != nil {
			return err
		}
	} else if target.Name != cfg.claimsTargetName || target.Endpoint != endpoint || target.Timeout != "1s" {
		target, err = client.UpdateProductTokenClaimsTarget(ctx, target, cfg.claimsTargetName, endpoint)
		if err != nil {
			return err
		}
	}
	if strings.TrimSpace(target.ID) == "" || strings.TrimSpace(target.SigningKey) == "" {
		return errors.New("Zitadel product token claims target returned incomplete credentials")
	}
	if err := writeCredential(filepath.Join(cfg.iamCredstoreDir, "zitadel-action-signing-key"), cfg.iamCredstoreGroup, target.SigningKey+"\n"); err != nil {
		return fmt.Errorf("write Zitadel product token claims signing key: %w", err)
	}
	return client.SetFunctionExecution(ctx, productTokenClaimsFunction, []string{target.ID})
}

func (c zitadelClient) EnsureProject(ctx context.Context, name string) (zitadelProject, error) {
	project, found, err := c.FindProjectByName(ctx, name)
	if err != nil {
		return zitadelProject{}, err
	}
	if !found {
		var out struct {
			ID string `json:"id"`
		}
		body := map[string]any{"name": strings.TrimSpace(name), "projectRoleAssertion": false, "projectRoleCheck": false}
		if err := c.doJSON(ctx, http.MethodPost, "/management/v1/projects", body, &out); err != nil {
			return zitadelProject{}, fmt.Errorf("create Zitadel project %s: %w", name, err)
		}
		if strings.TrimSpace(out.ID) == "" {
			return zitadelProject{}, fmt.Errorf("create Zitadel project %s returned no id", name)
		}
		return zitadelProject{ID: strings.TrimSpace(out.ID), Name: strings.TrimSpace(name), State: "PROJECT_STATE_ACTIVE"}, nil
	}
	body := map[string]any{"name": strings.TrimSpace(name), "projectRoleAssertion": false, "projectRoleCheck": false}
	if err := c.doJSON(ctx, http.MethodPut, "/management/v1/projects/"+url.PathEscape(project.ID), body, nil); err != nil && !isNoChanges(err) {
		return zitadelProject{}, fmt.Errorf("update Zitadel project %s defaults: %w", name, err)
	}
	return project, nil
}

func (c zitadelClient) FindProjectByName(ctx context.Context, name string) (zitadelProject, bool, error) {
	var out struct {
		Result []struct {
			ID    string `json:"id"`
			Name  string `json:"name"`
			State string `json:"state"`
		} `json:"result"`
	}
	body := map[string]any{"queries": []map[string]any{{"nameQuery": map[string]string{"name": strings.TrimSpace(name), "method": "TEXT_QUERY_METHOD_EQUALS"}}}}
	if err := c.doJSON(ctx, http.MethodPost, "/management/v1/projects/_search", body, &out); err != nil {
		return zitadelProject{}, false, fmt.Errorf("search Zitadel project %s: %w", name, err)
	}
	if len(out.Result) == 0 || strings.TrimSpace(out.Result[0].ID) == "" {
		return zitadelProject{}, false, nil
	}
	item := out.Result[0]
	return zitadelProject{ID: item.ID, Name: item.Name, State: item.State}, true, nil
}

func (c zitadelClient) FindOIDCAppByName(ctx context.Context, projectID, name string) (zitadelOIDCApp, bool, error) {
	var out struct {
		Result []struct {
			ID         string `json:"id"`
			OIDCConfig struct {
				ClientID string `json:"clientId"`
			} `json:"oidcConfig"`
		} `json:"result"`
	}
	body := map[string]any{"queries": []map[string]any{{"nameQuery": map[string]string{"name": strings.TrimSpace(name), "method": "TEXT_QUERY_METHOD_EQUALS"}}}}
	path := "/management/v1/projects/" + url.PathEscape(strings.TrimSpace(projectID)) + "/apps/_search"
	if err := c.doJSON(ctx, http.MethodPost, path, body, &out); err != nil {
		return zitadelOIDCApp{}, false, fmt.Errorf("search Zitadel OIDC app %s: %w", name, err)
	}
	if len(out.Result) == 0 || strings.TrimSpace(out.Result[0].ID) == "" {
		return zitadelOIDCApp{}, false, nil
	}
	item := out.Result[0]
	return zitadelOIDCApp{ID: item.ID, ClientID: item.OIDCConfig.ClientID}, true, nil
}

func (c zitadelClient) CreateBrowserOIDCApp(ctx context.Context, projectID, name string, redirectURIs, postLogoutRedirectURIs []string, loginBaseURI string) (zitadelOIDCApp, error) {
	body := browserOIDCConfigBody(redirectURIs, postLogoutRedirectURIs, loginBaseURI)
	body["name"] = strings.TrimSpace(name)
	var out struct {
		AppID        string `json:"appId"`
		ClientID     string `json:"clientId"`
		ClientSecret string `json:"clientSecret"`
	}
	path := "/management/v1/projects/" + url.PathEscape(strings.TrimSpace(projectID)) + "/apps/oidc"
	if err := c.doJSON(ctx, http.MethodPost, path, body, &out); err != nil {
		return zitadelOIDCApp{}, fmt.Errorf("create Zitadel OIDC app %s: %w", name, err)
	}
	app := zitadelOIDCApp{ID: strings.TrimSpace(out.AppID), ClientID: strings.TrimSpace(out.ClientID), ClientSecret: strings.TrimSpace(out.ClientSecret)}
	if app.ID == "" || app.ClientID == "" || app.ClientSecret == "" {
		return zitadelOIDCApp{}, fmt.Errorf("create Zitadel OIDC app %s returned incomplete credentials", name)
	}
	return app, nil
}

func (c zitadelClient) CreateNativeOIDCApp(ctx context.Context, projectID, name string, loginBaseURI string) (zitadelOIDCApp, error) {
	body := nativeOIDCConfigBody(loginBaseURI)
	body["name"] = strings.TrimSpace(name)
	var out struct {
		AppID    string `json:"appId"`
		ClientID string `json:"clientId"`
	}
	path := "/management/v1/projects/" + url.PathEscape(strings.TrimSpace(projectID)) + "/apps/oidc"
	if err := c.doJSON(ctx, http.MethodPost, path, body, &out); err != nil {
		return zitadelOIDCApp{}, fmt.Errorf("create Zitadel native OIDC app %s: %w", name, err)
	}
	app := zitadelOIDCApp{ID: strings.TrimSpace(out.AppID), ClientID: strings.TrimSpace(out.ClientID)}
	if app.ID == "" || app.ClientID == "" {
		return zitadelOIDCApp{}, fmt.Errorf("create Zitadel native OIDC app %s returned incomplete credentials", name)
	}
	return app, nil
}

func (c zitadelClient) ReconcileBrowserOIDCApp(ctx context.Context, projectID, appID, name string, redirectURIs, postLogoutRedirectURIs []string, loginBaseURI string) error {
	body := browserOIDCConfigBody(redirectURIs, postLogoutRedirectURIs, loginBaseURI)
	body["accessTokenRoleAssertion"] = false
	body["idTokenRoleAssertion"] = false
	body["idTokenUserinfoAssertion"] = true
	path := "/management/v1/projects/" + url.PathEscape(strings.TrimSpace(projectID)) + "/apps/" + url.PathEscape(strings.TrimSpace(appID)) + "/oidc_config"
	if err := c.doJSON(ctx, http.MethodPut, path, body, nil); err != nil && !isNoChanges(err) {
		return fmt.Errorf("update Zitadel OIDC app %s: %w", name, err)
	}
	return nil
}

func (c zitadelClient) ReconcileNativeOIDCApp(ctx context.Context, projectID, appID, name string, loginBaseURI string) error {
	body := nativeOIDCConfigBody(loginBaseURI)
	body["accessTokenRoleAssertion"] = false
	body["idTokenRoleAssertion"] = false
	body["idTokenUserinfoAssertion"] = true
	path := "/management/v1/projects/" + url.PathEscape(strings.TrimSpace(projectID)) + "/apps/" + url.PathEscape(strings.TrimSpace(appID)) + "/oidc_config"
	if err := c.doJSON(ctx, http.MethodPut, path, body, nil); err != nil && !isNoChanges(err) {
		return fmt.Errorf("update Zitadel native OIDC app %s: %w", name, err)
	}
	return nil
}

func (c zitadelClient) DeleteOIDCApp(ctx context.Context, projectID, appID string) error {
	path := "/management/v1/projects/" + url.PathEscape(strings.TrimSpace(projectID)) + "/apps/" + url.PathEscape(strings.TrimSpace(appID))
	if err := c.doJSON(ctx, http.MethodDelete, path, nil, nil); err != nil {
		return fmt.Errorf("delete Zitadel OIDC app %s: %w", appID, err)
	}
	return nil
}

func (c zitadelClient) FindActionTargetByName(ctx context.Context, name string) (actionTarget, bool, error) {
	targets, err := c.ListActionTargets(ctx)
	if err != nil {
		return actionTarget{}, false, err
	}
	for _, target := range targets {
		if target.Name == name {
			return target, true, nil
		}
	}
	return actionTarget{}, false, nil
}

func (c zitadelClient) ListActionTargets(ctx context.Context) ([]actionTarget, error) {
	var out struct {
		Targets []struct {
			ID         string `json:"id"`
			Name       string `json:"name"`
			Timeout    string `json:"timeout"`
			Endpoint   string `json:"endpoint"`
			SigningKey string `json:"signingKey"`
		} `json:"targets"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/v2/actions/targets/search", map[string]any{"pagination": map[string]any{"limit": 200}}, &out); err != nil {
		return nil, fmt.Errorf("search Zitadel action targets: %w", err)
	}
	targets := make([]actionTarget, 0, len(out.Targets))
	for _, item := range out.Targets {
		targets = append(targets, actionTarget{
			ID:         strings.TrimSpace(item.ID),
			Name:       strings.TrimSpace(item.Name),
			Endpoint:   strings.TrimSpace(item.Endpoint),
			Timeout:    strings.TrimSpace(item.Timeout),
			SigningKey: strings.TrimSpace(item.SigningKey),
		})
	}
	return targets, nil
}

func (c zitadelClient) CreateProductTokenClaimsTarget(ctx context.Context, name, endpoint string) (actionTarget, error) {
	var out struct {
		ID         string `json:"id"`
		SigningKey string `json:"signingKey"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/v2/actions/targets", productTokenClaimsTargetBody(name, endpoint), &out); err != nil {
		return actionTarget{}, fmt.Errorf("create Zitadel product token claims target: %w", err)
	}
	target := actionTarget{ID: strings.TrimSpace(out.ID), Name: strings.TrimSpace(name), Endpoint: strings.TrimSpace(endpoint), Timeout: "1s", SigningKey: strings.TrimSpace(out.SigningKey)}
	if target.ID == "" || target.SigningKey == "" {
		return actionTarget{}, errors.New("create Zitadel product token claims target returned incomplete credentials")
	}
	return target, nil
}

func (c zitadelClient) UpdateProductTokenClaimsTarget(ctx context.Context, target actionTarget, name, endpoint string) (actionTarget, error) {
	var out struct {
		SigningKey string `json:"signingKey"`
	}
	path := "/v2/actions/targets/" + url.PathEscape(strings.TrimSpace(target.ID))
	if err := c.doJSON(ctx, http.MethodPost, path, productTokenClaimsTargetBody(name, endpoint), &out); err != nil {
		return actionTarget{}, fmt.Errorf("update Zitadel product token claims target: %w", err)
	}
	target.Name = strings.TrimSpace(name)
	target.Endpoint = strings.TrimSpace(endpoint)
	target.Timeout = "1s"
	if signingKey := strings.TrimSpace(out.SigningKey); signingKey != "" {
		target.SigningKey = signingKey
	}
	return target, nil
}

func (c zitadelClient) SetFunctionExecution(ctx context.Context, function string, targets []string) error {
	body := map[string]any{
		"condition": map[string]any{"function": map[string]any{"name": strings.TrimSpace(function)}},
		"targets":   targets,
	}
	if err := c.doJSON(ctx, http.MethodPut, "/v2/actions/executions", body, nil); err != nil {
		return fmt.Errorf("set Zitadel %s execution: %w", function, err)
	}
	return nil
}

func (c zitadelClient) doJSON(ctx context.Context, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.baseURL, "/")+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.hostHeader != "" {
		req.Host = c.hostHeader
	}
	client := c.client
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
		return statusError{Method: method, Path: path, Status: resp.StatusCode, Body: strings.TrimSpace(string(data))}
	}
	if out == nil || len(data) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("zitadel %s %s: decode response: %w", method, path, err)
	}
	return nil
}

func browserOIDCConfigBody(redirectURIs, postLogoutRedirectURIs []string, loginBaseURI string) map[string]any {
	return map[string]any{
		"redirectUris":           redirectURIs,
		"responseTypes":          []string{"OIDC_RESPONSE_TYPE_CODE"},
		"grantTypes":             []string{"OIDC_GRANT_TYPE_AUTHORIZATION_CODE", "OIDC_GRANT_TYPE_REFRESH_TOKEN", "OIDC_GRANT_TYPE_TOKEN_EXCHANGE"},
		"appType":                "OIDC_APP_TYPE_WEB",
		"authMethodType":         "OIDC_AUTH_METHOD_TYPE_POST",
		"postLogoutRedirectUris": postLogoutRedirectURIs,
		"devMode":                false,
		"accessTokenType":        "OIDC_TOKEN_TYPE_JWT",
		"loginVersion":           customLoginVersion(loginBaseURI),
	}
}

func nativeOIDCConfigBody(loginBaseURI string) map[string]any {
	return map[string]any{
		"redirectUris":           []string{},
		"responseTypes":          []string{"OIDC_RESPONSE_TYPE_CODE"},
		"grantTypes":             []string{"OIDC_GRANT_TYPE_DEVICE_CODE", "OIDC_GRANT_TYPE_REFRESH_TOKEN"},
		"appType":                "OIDC_APP_TYPE_NATIVE",
		"authMethodType":         "OIDC_AUTH_METHOD_TYPE_NONE",
		"postLogoutRedirectUris": []string{},
		"devMode":                false,
		"accessTokenType":        "OIDC_TOKEN_TYPE_JWT",
		"loginVersion":           customLoginVersion(loginBaseURI),
	}
}

func customLoginVersion(loginBaseURI string) map[string]any {
	return map[string]any{
		"loginV2": map[string]any{
			"baseUri": strings.TrimRight(strings.TrimSpace(loginBaseURI), "/"),
		},
	}
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

func isNoChanges(err error) bool {
	var status statusError
	return errors.As(err, &status) && status.Status == http.StatusBadRequest && strings.Contains(status.Body, "No changes")
}

func readSecret(path string) (string, error) {
	value, err := readTrimmed(path)
	if err != nil {
		return "", err
	}
	if value == "" {
		return "", fmt.Errorf("%s is empty", path)
	}
	return value, nil
}

func readTrimmed(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	return strings.TrimSpace(string(raw)), nil
}

func writeCredential(path, group, value string) error {
	existing, err := os.ReadFile(path)
	if err == nil && string(existing) == value {
		return chmodChown(path, group)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp for %s: %w", path, err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.WriteString(value); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write %s: %w", tmpName, err)
	}
	if err := tmp.Chmod(credentialMode); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod %s: %w", tmpName, err)
	}
	if err := chownFile(tmpName, group); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename %s to %s: %w", tmpName, path, err)
	}
	return nil
}

func chmodChown(path, group string) error {
	if err := os.Chmod(path, credentialMode); err != nil {
		return fmt.Errorf("chmod %s: %w", path, err)
	}
	return chownFile(path, group)
}

func chownFile(path, group string) error {
	g, err := user.LookupGroup(group)
	if err != nil {
		return fmt.Errorf("lookup group %s: %w", group, err)
	}
	gid, err := strconv.Atoi(g.Gid)
	if err != nil {
		return fmt.Errorf("parse gid for %s: %w", group, err)
	}
	if err := os.Chown(path, 0, gid); err != nil {
		return fmt.Errorf("chown %s root:%s: %w", path, group, err)
	}
	return nil
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
