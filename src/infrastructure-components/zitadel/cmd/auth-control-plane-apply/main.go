package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
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
	defaultProjectName             = "verself-api"
	defaultBrowserAppName          = "verself-web"
	defaultCLIAppName              = "verself-cli"
	defaultClaimsTargetName        = "verself-product-token-claims"
	defaultClaimsActionPath        = "/internal/zitadel/actions/product-token-claims"
	productTokenClaimsFunction     = "preaccesstoken"
	defaultZitadelBaseURL          = "http://127.0.0.1:8085"
	defaultGithubLoginIDPName      = "GitHub"
	desiredPasswordMinLength       = 8
	desiredPasswordLockoutAttempts = 10
)

const (
	iamAuthAudienceSecret     = "iam-service.zitadel.auth_audience"
	iamOIDCAppIDSecret        = "iam-service.zitadel.oidc_app_id"
	iamOIDCClientIDSecret     = "iam-service.zitadel.oidc_client_id"
	iamOIDCClientSecretSecret = "iam-service.zitadel.oidc_client_secret"
	iamOIDCProjectIDSecret    = "iam-service.zitadel.oidc_project_id"
	iamOIDCCLIAppIDSecret     = "iam-service.zitadel.oidc_cli_app_id"
	iamOIDCCLIClientIDSecret  = "iam-service.zitadel.oidc_cli_client_id"
	iamOIDCCLIProjectIDSecret = "iam-service.zitadel.oidc_cli_project_id"
	iamActionSigningKeySecret = "iam-service.zitadel.action_signing_key"
	iamGithubLoginIDPIDSecret = "iam-service.zitadel.github_login_idp_id"
)

type config struct {
	zitadelBaseURL      string
	zitadelHost         string
	adminPAT            string
	adminPATFile        string
	adminPATSecretName  string
	verselfDomain       string
	iamServiceDomain    string
	projectName         string
	browserAppName      string
	cliAppName          string
	claimsTargetName    string
	claimsActionPath    string
	zitadelReadyWait    time.Duration
	zitadelReadyBackoff time.Duration
	openBaoAddr         string
	openBaoCACert       string
	openBaoToken        string
	openBaoTokenFile    string
	resourceGraph       string
	resourceName        string
	// GitHub login IdP ("Sign in with GitHub"). Optional: when either value is
	// empty, GitHub IdP provisioning is skipped so deployments without GitHub
	// login still converge. Values come from the Guardian resource graph plus
	// OpenBao runtime secrets.
	githubLoginIDPName          string
	githubLoginClientID         string
	githubLoginClientSecret     string
	githubLoginClientSecretFile string
	githubLoginClientSecretName string
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

type runtimeSecretStore struct {
	addr  string
	token string
	http  *http.Client
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
		verselfDomain:       envOr("AUTH_CONTROL_PLANE_VERSELF_DOMAIN", ""),
		iamServiceDomain:    envOr("AUTH_CONTROL_PLANE_IAM_SERVICE_DOMAIN", ""),
		projectName:         defaultProjectName,
		browserAppName:      defaultBrowserAppName,
		cliAppName:          defaultCLIAppName,
		claimsTargetName:    defaultClaimsTargetName,
		claimsActionPath:    defaultClaimsActionPath,
		zitadelReadyWait:    60 * time.Second,
		zitadelReadyBackoff: time.Second,
		openBaoAddr:         "",
		openBaoCACert:       "",
		resourceName:        "auth-control-plane",

		githubLoginIDPName:  envOr("AUTH_CONTROL_PLANE_GITHUB_LOGIN_IDP_NAME", defaultGithubLoginIDPName),
		githubLoginClientID: envOr("AUTH_CONTROL_PLANE_GITHUB_LOGIN_CLIENT_ID", ""),
	}
	fs := flag.NewFlagSet("auth-control-plane-apply", flag.ContinueOnError)
	fs.StringVar(&cfg.zitadelBaseURL, "zitadel-base-url", cfg.zitadelBaseURL, "Local Zitadel base URL.")
	fs.StringVar(&cfg.zitadelHost, "zitadel-host", cfg.zitadelHost, "Zitadel HTTP Host header.")
	fs.StringVar(&cfg.adminPATFile, "admin-pat-file", cfg.adminPATFile, "File containing the Zitadel admin PAT.")
	fs.StringVar(&cfg.verselfDomain, "verself-domain", cfg.verselfDomain, "Product apex domain.")
	fs.StringVar(&cfg.iamServiceDomain, "iam-service-domain", cfg.iamServiceDomain, "Public IAM service domain.")
	fs.StringVar(&cfg.projectName, "project-name", cfg.projectName, "Zitadel project for product API audiences.")
	fs.StringVar(&cfg.browserAppName, "browser-app-name", cfg.browserAppName, "Browser OIDC app name.")
	fs.StringVar(&cfg.cliAppName, "cli-app-name", cfg.cliAppName, "CLI OIDC app name.")
	fs.StringVar(&cfg.claimsTargetName, "claims-target-name", cfg.claimsTargetName, "Product token claims action target name.")
	fs.StringVar(&cfg.claimsActionPath, "claims-action-path", cfg.claimsActionPath, "Product token claims action path.")
	fs.StringVar(&cfg.openBaoAddr, "openbao-addr", cfg.openBaoAddr, "OpenBao address for publishing Zitadel outputs.")
	fs.StringVar(&cfg.openBaoCACert, "openbao-ca-cert", cfg.openBaoCACert, "OpenBao CA certificate path.")
	fs.StringVar(&cfg.openBaoTokenFile, "openbao-token-file", cfg.openBaoTokenFile, "File containing the OpenBao workload token.")
	fs.StringVar(&cfg.resourceGraph, "resource-graph", cfg.resourceGraph, "Guardian resource graph document path.")
	fs.StringVar(&cfg.resourceName, "resource-name", cfg.resourceName, "ZitadelAuthControlPlane resource name.")
	fs.StringVar(&cfg.githubLoginIDPName, "github-login-idp-name", cfg.githubLoginIDPName, "Zitadel IdP display name for Sign in with GitHub.")
	fs.StringVar(&cfg.githubLoginClientID, "github-login-client-id", cfg.githubLoginClientID, "GitHub OAuth App client id for Sign in with GitHub.")
	fs.StringVar(&cfg.githubLoginClientSecretFile, "github-login-client-secret-file", cfg.githubLoginClientSecretFile, "File containing the GitHub OAuth App client secret.")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if cfg.resourceGraph != "" {
		next, err := applyResourceGraphConfig(cfg)
		if err != nil {
			return err
		}
		cfg = next
	}
	if err := cfg.loadCredentialFiles(); err != nil {
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

func (cfg *config) loadCredentialFiles() error {
	openBaoToken, err := readRequiredCredentialFile(cfg.openBaoTokenFile, "--openbao-token-file")
	if err != nil {
		return err
	}
	cfg.openBaoToken = openBaoToken

	if strings.TrimSpace(cfg.adminPATFile) != "" {
		adminPAT, err := readRequiredCredentialFile(cfg.adminPATFile, "--admin-pat-file")
		if err != nil {
			return err
		}
		cfg.adminPAT = adminPAT
	}
	if strings.TrimSpace(cfg.githubLoginClientSecretFile) == "" {
		return nil
	}
	secret, err := readRequiredCredentialFile(cfg.githubLoginClientSecretFile, "--github-login-client-secret-file")
	if err != nil {
		return err
	}
	cfg.githubLoginClientSecret = secret
	return nil
}

func readRequiredCredentialFile(path, flagName string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("%s is required", flagName)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", flagName, err)
	}
	value := strings.TrimSpace(string(body))
	if value == "" {
		return "", fmt.Errorf("%s is empty", flagName)
	}
	return value, nil
}

func (cfg config) validate() error {
	missing := []string{}
	for name, value := range map[string]string{
		"--zitadel-base-url":   cfg.zitadelBaseURL,
		"--zitadel-host":       cfg.zitadelHost,
		"--verself-domain":     cfg.verselfDomain,
		"--iam-service-domain": cfg.iamServiceDomain,
		"--project-name":       cfg.projectName,
		"--browser-app-name":   cfg.browserAppName,
		"--cli-app-name":       cfg.cliAppName,
		"--claims-target-name": cfg.claimsTargetName,
		"--claims-action-path": cfg.claimsActionPath,
		"--openbao-addr":       cfg.openBaoAddr,
		"--openbao-token-file": cfg.openBaoToken,
	} {
		if strings.TrimSpace(value) == "" {
			missing = append(missing, name)
		}
	}
	if strings.TrimSpace(cfg.adminPAT) == "" && strings.TrimSpace(cfg.adminPATSecretName) == "" {
		missing = append(missing, "--admin-pat-file or ZitadelAuthControlPlane.spec.openBao.adminPATSecretRef")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required flags: %s", strings.Join(missing, ", "))
	}
	if strings.TrimSpace(cfg.githubLoginClientID) != "" && strings.TrimSpace(cfg.githubLoginClientSecret) == "" && strings.TrimSpace(cfg.githubLoginClientSecretName) == "" {
		return fmt.Errorf("--github-login-client-secret-file or ZitadelAuthControlPlane.spec.openBao.githubLoginClientSecretRef is required when GitHub login client id is set")
	}
	if err := validateDomainFlag("--verself-domain", cfg.verselfDomain); err != nil {
		return err
	}
	if err := validateDomainFlag("--iam-service-domain", cfg.iamServiceDomain); err != nil {
		return err
	}
	if !strings.HasPrefix(cfg.claimsActionPath, "/") {
		return fmt.Errorf("invalid --claims-action-path: must start with /")
	}
	if _, err := url.ParseRequestURI(cfg.claimsActionPath); err != nil {
		return fmt.Errorf("invalid --claims-action-path: %w", err)
	}
	return nil
}

func validateDomainFlag(name, value string) error {
	value = strings.TrimSpace(value)
	if strings.Contains(value, "://") || strings.ContainsAny(value, "/?#") {
		return fmt.Errorf("invalid %s: expected host name", name)
	}
	if _, err := url.ParseRequestURI("https://" + value); err != nil {
		return fmt.Errorf("invalid %s: %w", name, err)
	}
	return nil
}

type guardianDocument struct {
	Entrypoint json.RawMessage    `json:"entrypoint,omitempty"`
	Resources  []guardianResource `json:"resources"`
}

type guardianResource struct {
	APIVersion string          `json:"apiVersion"`
	Kind       string          `json:"kind"`
	Metadata   resourceMeta    `json:"metadata"`
	Spec       json.RawMessage `json:"spec"`
}

type resourceMeta struct {
	Name string `json:"name"`
}

type objectRef struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
}

type authControlPlaneSpec struct {
	ZitadelBaseURL   string `json:"zitadelBaseURL"`
	ZitadelHost      string `json:"zitadelHost"`
	VerselfDomain    string `json:"verselfDomain"`
	IAMServiceDomain string `json:"iamServiceDomain"`
	ProjectName      string `json:"projectName"`
	BrowserAppName   string `json:"browserAppName"`
	CLIAppName       string `json:"cliAppName"`
	ClaimsTargetName string `json:"claimsTargetName"`
	ClaimsActionPath string `json:"claimsActionPath"`
	OpenBao          struct {
		Address                    string     `json:"address"`
		CACert                     string     `json:"caCert"`
		AdminPATSecretRef          objectRef  `json:"adminPATSecretRef"`
		GithubLoginClientID        string     `json:"githubLoginClientID"`
		GithubLoginClientSecretRef *objectRef `json:"githubLoginClientSecretRef"`
	} `json:"openBao"`
}

func applyResourceGraphConfig(cfg config) (config, error) {
	body, err := os.ReadFile(cfg.resourceGraph)
	if err != nil {
		return config{}, fmt.Errorf("read Guardian resource graph: %w", err)
	}
	var doc guardianDocument
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&doc); err != nil {
		return config{}, fmt.Errorf("decode Guardian resource graph: %w", err)
	}
	name := strings.TrimSpace(cfg.resourceName)
	if name == "" {
		name = "auth-control-plane"
	}
	var matches []guardianResource
	for _, resource := range doc.Resources {
		if resource.APIVersion == "zitadel.guardianintelligence.org/v1alpha1" && resource.Kind == "ZitadelAuthControlPlane" && resource.Metadata.Name == name {
			matches = append(matches, resource)
		}
	}
	if len(matches) != 1 {
		return config{}, fmt.Errorf("expected exactly one ZitadelAuthControlPlane resource named %q, found %d", name, len(matches))
	}
	var spec authControlPlaneSpec
	specDecoder := json.NewDecoder(bytes.NewReader(matches[0].Spec))
	specDecoder.DisallowUnknownFields()
	if err := specDecoder.Decode(&spec); err != nil {
		return config{}, fmt.Errorf("decode ZitadelAuthControlPlane %q: %w", name, err)
	}
	cfg.zitadelBaseURL = spec.ZitadelBaseURL
	cfg.zitadelHost = spec.ZitadelHost
	cfg.verselfDomain = spec.VerselfDomain
	cfg.iamServiceDomain = spec.IAMServiceDomain
	cfg.projectName = spec.ProjectName
	cfg.browserAppName = spec.BrowserAppName
	cfg.cliAppName = spec.CLIAppName
	cfg.claimsTargetName = spec.ClaimsTargetName
	cfg.claimsActionPath = spec.ClaimsActionPath
	cfg.openBaoAddr = spec.OpenBao.Address
	cfg.openBaoCACert = spec.OpenBao.CACert
	cfg.adminPATSecretName = refName(spec.OpenBao.AdminPATSecretRef)
	cfg.githubLoginClientID = strings.TrimSpace(spec.OpenBao.GithubLoginClientID)
	if spec.OpenBao.GithubLoginClientSecretRef != nil {
		cfg.githubLoginClientSecretName = refName(*spec.OpenBao.GithubLoginClientSecretRef)
	}
	return cfg, nil
}

func refName(ref objectRef) string {
	return strings.TrimSpace(ref.Name)
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
	secrets, err := newRuntimeSecretStore(cfg)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	if err := loadRuntimeSecrets(ctx, secrets, &cfg); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	if err := waitForZitadel(ctx, cfg); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	client := zitadelClient{
		baseURL:    cfg.zitadelBaseURL,
		hostHeader: cfg.zitadelHost,
		token:      strings.TrimSpace(cfg.adminPAT),
		client:     &http.Client{Timeout: 5 * time.Second},
	}
	if err := client.EnsurePasswordPolicies(ctx); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	if err := ensureLoginClientRole(ctx, client); err != nil {
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
	if err := ensureRuntimeAudiences(ctx, secrets, project.ID); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	if err := ensureBrowserOIDCApplication(ctx, secrets, client, project.ID, cfg); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	if err := ensureCLIOIDCApplication(ctx, secrets, client, project.ID, cfg); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	if err := ensureCrossOrgProjectAccess(ctx, client, project.ID); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	if err := ensureProductTokenClaimsAction(ctx, secrets, client, cfg); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	if err := ensureGitHubLoginIDP(ctx, secrets, client, cfg); err != nil {
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

func ensureRuntimeAudiences(ctx context.Context, secrets *runtimeSecretStore, projectID string) error {
	if err := secrets.writeRuntimeSecret(ctx, iamAuthAudienceSecret, projectID); err != nil {
		return fmt.Errorf("write iam-service auth audience: %w", err)
	}
	return nil
}

func loadRuntimeSecrets(ctx context.Context, secrets *runtimeSecretStore, cfg *config) error {
	if strings.TrimSpace(cfg.adminPAT) == "" {
		value, found, err := secrets.readRuntimeSecret(ctx, cfg.adminPATSecretName)
		if err != nil {
			return fmt.Errorf("read Zitadel admin PAT: %w", err)
		}
		if !found || strings.TrimSpace(value) == "" {
			return fmt.Errorf("OpenBao runtime secret %s is empty or missing", cfg.adminPATSecretName)
		}
		cfg.adminPAT = value
	}
	if strings.TrimSpace(cfg.githubLoginClientID) == "" || strings.TrimSpace(cfg.githubLoginClientSecret) != "" {
		return nil
	}
	value, found, err := secrets.readRuntimeSecret(ctx, cfg.githubLoginClientSecretName)
	if err != nil {
		return fmt.Errorf("read GitHub login client secret: %w", err)
	}
	if !found || strings.TrimSpace(value) == "" {
		return fmt.Errorf("OpenBao runtime secret %s is empty or missing", cfg.githubLoginClientSecretName)
	}
	cfg.githubLoginClientSecret = value
	return nil
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

func ensureBrowserOIDCApplication(ctx context.Context, secrets *runtimeSecretStore, client zitadelClient, projectID string, cfg config) error {
	app, found, err := client.FindOIDCAppByName(ctx, projectID, cfg.browserAppName)
	if err != nil {
		return err
	}
	storedProjectID, _, err := secrets.readRuntimeSecret(ctx, iamOIDCProjectIDSecret)
	if err != nil {
		return fmt.Errorf("read browser OIDC project id: %w", err)
	}
	storedSecret, _, err := secrets.readRuntimeSecret(ctx, iamOIDCClientSecretSecret)
	if err != nil {
		return fmt.Errorf("read browser OIDC client secret: %w", err)
	}
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
		iamOIDCAppIDSecret:     app.ID,
		iamOIDCClientIDSecret:  app.ClientID,
		iamOIDCProjectIDSecret: projectID,
	}
	if strings.TrimSpace(app.ClientSecret) != "" {
		values[iamOIDCClientSecretSecret] = app.ClientSecret
	} else if storedSecret == "" {
		return fmt.Errorf("browser OIDC app exists but no client secret is available in OpenBao")
	}
	for secret, value := range values {
		if err := secrets.writeRuntimeSecret(ctx, secret, value); err != nil {
			name := strings.TrimPrefix(secret, "iam-service.zitadel.")
			return fmt.Errorf("write browser OIDC %s: %w", name, err)
		}
	}
	return nil
}

func ensureCLIOIDCApplication(ctx context.Context, secrets *runtimeSecretStore, client zitadelClient, projectID string, cfg config) error {
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
		iamOIDCCLIAppIDSecret:     app.ID,
		iamOIDCCLIClientIDSecret:  app.ClientID,
		iamOIDCCLIProjectIDSecret: projectID,
	}
	for secret, value := range values {
		if err := secrets.writeRuntimeSecret(ctx, secret, value); err != nil {
			name := strings.TrimPrefix(secret, "iam-service.zitadel.")
			return fmt.Errorf("write CLI OIDC %s: %w", name, err)
		}
	}
	return nil
}

func ensureProductTokenClaimsAction(ctx context.Context, secrets *runtimeSecretStore, client zitadelClient, cfg config) error {
	endpoint := productTokenClaimsEndpoint(cfg)
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
	if err := secrets.writeRuntimeSecret(ctx, iamActionSigningKeySecret, target.SigningKey); err != nil {
		return fmt.Errorf("write Zitadel product token claims signing key: %w", err)
	}
	return client.SetFunctionExecution(ctx, productTokenClaimsFunction, []string{target.ID})
}

func productTokenClaimsEndpoint(cfg config) string {
	return "https://" + strings.TrimRight(strings.TrimSpace(cfg.iamServiceDomain), "/") + cfg.claimsActionPath
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

func newRuntimeSecretStore(cfg config) (*runtimeSecretStore, error) {
	addr := strings.TrimRight(strings.TrimSpace(cfg.openBaoAddr), "/")
	if addr == "" {
		return nil, fmt.Errorf("OpenBao address is required")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if cfg.openBaoCACert != "" {
		pem, err := os.ReadFile(cfg.openBaoCACert)
		if err != nil {
			return nil, fmt.Errorf("read OpenBao CA certificate: %w", err)
		}
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("parse OpenBao CA certificate %s", cfg.openBaoCACert)
		}
		transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	}
	return &runtimeSecretStore{
		addr:  addr,
		token: strings.TrimSpace(cfg.openBaoToken),
		http:  &http.Client{Transport: transport, Timeout: 5 * time.Second},
	}, nil
}

func (s *runtimeSecretStore) readRuntimeSecret(ctx context.Context, name string) (string, bool, error) {
	var out struct {
		Data struct {
			Data map[string]any `json:"data"`
		} `json:"data"`
	}
	status, err := s.doJSON(ctx, http.MethodGet, runtimeSecretPath(name), nil, &out)
	if status == http.StatusNotFound {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	value, _ := out.Data.Data["value"].(string)
	return strings.TrimSpace(value), strings.TrimSpace(value) != "", nil
}

func (s *runtimeSecretStore) writeRuntimeSecret(ctx context.Context, name, value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("%s value is empty", name)
	}
	_, err := s.doJSON(ctx, http.MethodPost, runtimeSecretPath(name), map[string]any{"data": map[string]any{"value": value}}, nil)
	return err
}

func (s *runtimeSecretStore) doJSON(ctx context.Context, method, path string, body any, out any) (int, error) {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return 0, fmt.Errorf("encode OpenBao request: %w", err)
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, s.addr+"/"+path, reader)
	if err != nil {
		return 0, err
	}
	req.Header.Set("X-Vault-Token", s.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("openbao %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if out != nil && len(raw) > 0 {
			if err := json.Unmarshal(raw, out); err != nil {
				return resp.StatusCode, fmt.Errorf("decode openbao %s %s: %w", method, path, err)
			}
		}
		return resp.StatusCode, nil
	}
	return resp.StatusCode, fmt.Errorf("openbao %s %s status %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(raw)))
}

func runtimeSecretPath(name string) string {
	return "v1/kv-runtime/data/secret/org/" + url.PathEscape(name)
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

// ensureGitHubLoginIDP provisions (or reconciles) the instance GitHub identity
// provider used for "Sign in with GitHub", links it to the login policy, and
// publishes its id to OpenBao for iam-service.
func ensureGitHubLoginIDP(ctx context.Context, secrets *runtimeSecretStore, client zitadelClient, cfg config) error {
	clientID := strings.TrimSpace(cfg.githubLoginClientID)
	clientSecret := strings.TrimSpace(cfg.githubLoginClientSecret)
	if clientID == "" || clientSecret == "" {
		fmt.Println("auth-control-plane-apply: github login idp not configured; skipping")
		return nil
	}
	idpID, found, err := client.FindIDPByName(ctx, cfg.githubLoginIDPName)
	if err != nil {
		return err
	}
	if found {
		if err := client.UpdateGitHubIDP(ctx, idpID, cfg.githubLoginIDPName, clientID, clientSecret); err != nil {
			return err
		}
	} else {
		idpID, err = client.CreateGitHubIDP(ctx, cfg.githubLoginIDPName, clientID, clientSecret)
		if err != nil {
			return err
		}
	}
	if err := client.AddIDPToLoginPolicy(ctx, idpID); err != nil {
		return err
	}
	return secrets.writeRuntimeSecret(ctx, iamGithubLoginIDPIDSecret, idpID)
}

func githubIDPBody(name, clientID, clientSecret string) map[string]any {
	return map[string]any{
		"name":         strings.TrimSpace(name),
		"clientId":     strings.TrimSpace(clientID),
		"clientSecret": clientSecret,
		"scopes":       []string{"read:user", "user:email"},
		"providerOptions": map[string]any{
			"isLinkingAllowed":  true,
			"isCreationAllowed": true,
			"isAutoCreation":    true,
			"isAutoUpdate":      true,
			"autoLinking":       "AUTO_LINKING_OPTION_EMAIL",
		},
	}
}

func (c zitadelClient) FindIDPByName(ctx context.Context, name string) (string, bool, error) {
	var out struct {
		Result []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"result"`
	}
	body := map[string]any{"queries": []map[string]any{{"idpNameQuery": map[string]string{"name": strings.TrimSpace(name), "method": "TEXT_QUERY_METHOD_EQUALS"}}}}
	if err := c.doJSON(ctx, http.MethodPost, "/admin/v1/idps/_search", body, &out); err != nil {
		return "", false, fmt.Errorf("search Zitadel IdP %s: %w", name, err)
	}
	for _, item := range out.Result {
		if strings.EqualFold(strings.TrimSpace(item.Name), strings.TrimSpace(name)) && strings.TrimSpace(item.ID) != "" {
			return strings.TrimSpace(item.ID), true, nil
		}
	}
	return "", false, nil
}

func (c zitadelClient) CreateGitHubIDP(ctx context.Context, name, clientID, clientSecret string) (string, error) {
	var out struct {
		ID string `json:"id"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/admin/v1/idps/github", githubIDPBody(name, clientID, clientSecret), &out); err != nil {
		return "", fmt.Errorf("create Zitadel GitHub IdP %s: %w", name, err)
	}
	if strings.TrimSpace(out.ID) == "" {
		return "", fmt.Errorf("create Zitadel GitHub IdP %s returned no id", name)
	}
	return strings.TrimSpace(out.ID), nil
}

func (c zitadelClient) UpdateGitHubIDP(ctx context.Context, idpID, name, clientID, clientSecret string) error {
	path := "/admin/v1/idps/github/" + url.PathEscape(strings.TrimSpace(idpID))
	if err := c.doJSON(ctx, http.MethodPut, path, githubIDPBody(name, clientID, clientSecret), nil); err != nil && !isNoChanges(err) {
		return fmt.Errorf("update Zitadel GitHub IdP %s: %w", name, err)
	}
	return nil
}

func (c zitadelClient) AddIDPToLoginPolicy(ctx context.Context, idpID string) error {
	body := map[string]any{"idpId": strings.TrimSpace(idpID), "ownerType": "IDP_OWNER_TYPE_SYSTEM"}
	if err := c.doJSON(ctx, http.MethodPost, "/admin/v1/policies/login/idps", body, nil); err != nil && !isAlreadyExists(err) {
		return fmt.Errorf("link Zitadel IdP %s to login policy: %w", idpID, err)
	}
	return nil
}

// ensureLoginClientRole grants the iam-service machine user (the admin token's
// own user) the instance IAM_LOGIN_CLIENT role. The headless login finalizes OIDC
// auth requests with a session via OIDCService.CreateCallback; that operation's
// permission check (checkUserPermissions) requires the login-client role, which
// IAM_OWNER does not include. Without it, finalizing a session for a user in any
// org fails with "No matching permissions found (AUTH-AWfge)" and login 503s.
// Idempotent and reconciled every deploy, so it also self-heals fresh instances.
func ensureLoginClientRole(ctx context.Context, client zitadelClient) error {
	userID, err := client.MyUserID(ctx)
	if err != nil {
		return err
	}
	roles, err := client.InstanceMemberRoles(ctx, userID)
	if err != nil {
		return err
	}
	for _, r := range roles {
		if r == "IAM_LOGIN_CLIENT" {
			return nil
		}
	}
	if err := client.SetInstanceMemberRoles(ctx, userID, append(roles, "IAM_LOGIN_CLIENT")); err != nil {
		return fmt.Errorf("grant IAM_LOGIN_CLIENT to %s: %w", userID, err)
	}
	fmt.Printf("auth-control-plane-apply: granted IAM_LOGIN_CLIENT to login machine user %s\n", userID)
	return nil
}

func (c zitadelClient) MyUserID(ctx context.Context) (string, error) {
	var out struct {
		User struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/auth/v1/users/me", nil, &out); err != nil {
		return "", fmt.Errorf("resolve own user id: %w", err)
	}
	if strings.TrimSpace(out.User.ID) == "" {
		return "", fmt.Errorf("resolve own user id returned empty")
	}
	return strings.TrimSpace(out.User.ID), nil
}

func (c zitadelClient) InstanceMemberRoles(ctx context.Context, userID string) ([]string, error) {
	var out struct {
		Result []struct {
			UserID string   `json:"userId"`
			Roles  []string `json:"roles"`
		} `json:"result"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/admin/v1/members/_search", map[string]any{}, &out); err != nil {
		return nil, fmt.Errorf("list instance members: %w", err)
	}
	for _, m := range out.Result {
		if strings.TrimSpace(m.UserID) == strings.TrimSpace(userID) {
			return m.Roles, nil
		}
	}
	return nil, nil
}

func (c zitadelClient) SetInstanceMemberRoles(ctx context.Context, userID string, roles []string) error {
	body := map[string]any{"roles": roles}
	path := "/admin/v1/members/" + url.PathEscape(strings.TrimSpace(userID))
	return c.doJSON(ctx, http.MethodPut, path, body, nil)
}

// crossOrgProjectRoleKey is the role attached to each tenant-org project grant.
// projectRoleCheck is disabled, so the role is not required for authorization; the
// grant itself is what makes the shared project reachable from a tenant org.
const crossOrgProjectRoleKey = "member"

// ensureCrossOrgProjectAccess makes the shared verself-api project usable by users
// in every tenant organization. The project is owned by the Zitadel instance org
// (shared auth infra, not a tenant); users live in per-tenant orgs. Without a
// project grant Zitadel refuses to finalize an OIDC auth request for a tenant-org
// user against this project ("No matching permissions found"). Granting the
// project to each org is the Zitadel-native cross-org mechanism. Idempotent and
// reconciled on every deploy, so existing and future tenant orgs both get access.
func ensureCrossOrgProjectAccess(ctx context.Context, client zitadelClient, projectID string) error {
	ownerOrg, err := client.ProjectOwnerOrg(ctx, projectID)
	if err != nil {
		return err
	}
	if err := client.EnsureProjectRole(ctx, projectID, crossOrgProjectRoleKey, "Member"); err != nil {
		return err
	}
	existing, err := client.ListProjectGrantOrgs(ctx, projectID)
	if err != nil {
		return err
	}
	orgs, err := client.ListOrgs(ctx)
	if err != nil {
		return err
	}
	granted := 0
	for _, org := range orgs {
		if org == ownerOrg || existing[org] {
			continue
		}
		if err := client.CreateProjectGrant(ctx, projectID, org, []string{crossOrgProjectRoleKey}); err != nil {
			if isAlreadyExists(err) {
				continue
			}
			return fmt.Errorf("grant project %s to org %s: %w", projectID, org, err)
		}
		granted++
	}
	fmt.Printf("auth-control-plane-apply: cross-org project access reconciled (owner_org=%s orgs=%d existing_grants=%d new_grants=%d)\n", ownerOrg, len(orgs), len(existing), granted)
	return nil
}

func (c zitadelClient) ProjectOwnerOrg(ctx context.Context, projectID string) (string, error) {
	var out struct {
		Project struct {
			Details struct {
				ResourceOwner string `json:"resourceOwner"`
			} `json:"details"`
		} `json:"project"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/management/v1/projects/"+url.PathEscape(strings.TrimSpace(projectID)), nil, &out); err != nil {
		return "", fmt.Errorf("get project %s: %w", projectID, err)
	}
	owner := strings.TrimSpace(out.Project.Details.ResourceOwner)
	if owner == "" {
		return "", fmt.Errorf("project %s returned no resource owner", projectID)
	}
	return owner, nil
}

func (c zitadelClient) ListOrgs(ctx context.Context) ([]string, error) {
	var ids []string
	offset := 0
	for {
		var out struct {
			Result []struct {
				ID string `json:"id"`
			} `json:"result"`
		}
		body := map[string]any{"query": map[string]any{"offset": offset, "limit": 100, "asc": true}}
		if err := c.doJSON(ctx, http.MethodPost, "/admin/v1/orgs/_search", body, &out); err != nil {
			return nil, fmt.Errorf("list orgs: %w", err)
		}
		for _, r := range out.Result {
			if id := strings.TrimSpace(r.ID); id != "" {
				ids = append(ids, id)
			}
		}
		if len(out.Result) < 100 {
			return ids, nil
		}
		offset += len(out.Result)
	}
}

func (c zitadelClient) EnsureProjectRole(ctx context.Context, projectID, roleKey, displayName string) error {
	body := map[string]any{"roleKey": strings.TrimSpace(roleKey), "displayName": strings.TrimSpace(displayName)}
	path := "/management/v1/projects/" + url.PathEscape(strings.TrimSpace(projectID)) + "/roles"
	if err := c.doJSON(ctx, http.MethodPost, path, body, nil); err != nil && !isAlreadyExists(err) {
		return fmt.Errorf("ensure project role %s: %w", roleKey, err)
	}
	return nil
}

func (c zitadelClient) ListProjectGrantOrgs(ctx context.Context, projectID string) (map[string]bool, error) {
	var out struct {
		Result []struct {
			GrantedOrgID string `json:"grantedOrgId"`
		} `json:"result"`
	}
	path := "/management/v1/projects/" + url.PathEscape(strings.TrimSpace(projectID)) + "/grants/_search"
	if err := c.doJSON(ctx, http.MethodPost, path, map[string]any{}, &out); err != nil {
		return nil, fmt.Errorf("list project grants: %w", err)
	}
	orgs := make(map[string]bool, len(out.Result))
	for _, r := range out.Result {
		if id := strings.TrimSpace(r.GrantedOrgID); id != "" {
			orgs[id] = true
		}
	}
	return orgs, nil
}

func (c zitadelClient) CreateProjectGrant(ctx context.Context, projectID, grantedOrgID string, roleKeys []string) error {
	body := map[string]any{"grantedOrgId": strings.TrimSpace(grantedOrgID), "roleKeys": roleKeys}
	path := "/management/v1/projects/" + url.PathEscape(strings.TrimSpace(projectID)) + "/grants"
	return c.doJSON(ctx, http.MethodPost, path, body, nil)
}

func isAlreadyExists(err error) bool {
	var status statusError
	if !errors.As(err, &status) {
		return false
	}
	if status.Status == http.StatusConflict {
		return true
	}
	return status.Status == http.StatusBadRequest && strings.Contains(strings.ToLower(status.Body), "already")
}

func isNoChanges(err error) bool {
	var status statusError
	return errors.As(err, &status) && status.Status == http.StatusBadRequest && strings.Contains(status.Body, "No changes")
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
