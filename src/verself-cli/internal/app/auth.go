package app

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	verself "github.com/verself/verself-go"
)

const (
	deviceGrantType  = "urn:ietf:params:oauth:grant-type:device_code"
	iamAuthErrorsURL = "https://verself.sh/docs/reference/iam/errors#"
)

type authCLIError struct {
	Code   string
	Detail string
}

func (e authCLIError) Error() string {
	return e.Code + ": " + e.Detail + "; see " + iamAuthErrorsURL + authProblemAnchor(e.Code)
}

func authCommandError(code, detail string) error {
	return authCLIError{Code: strings.TrimSpace(code), Detail: strings.TrimSpace(detail)}
}

func authProblemAnchor(code string) string {
	return strings.NewReplacer(".", "-", "_", "-").Replace(strings.TrimSpace(code))
}

type authCredential struct {
	AccessToken  string    `json:"access_token"`
	IDToken      string    `json:"id_token,omitempty"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	TokenType    string    `json:"token_type,omitempty"`
	ExpiresAt    time.Time `json:"expires_at,omitempty"`
	TokenURL     string    `json:"token_url,omitempty"`
	ClientID     string    `json:"client_id,omitempty"`
}

type oidcDiscovery struct {
	DeviceAuthorizationEndpoint string `json:"device_authorization_endpoint"`
	TokenEndpoint               string `json:"token_endpoint"`
}

type verselfDiscovery struct {
	SchemaVersion int `json:"schema_version"`
	Auth          struct {
		IssuerURL          string `json:"issuer_url"`
		CLIClientID        string `json:"cli_client_id"`
		ProductAPIAudience string `json:"product_api_audience"`
	} `json:"auth"`
	PublicAPIs map[string]struct {
		BaseURL string `json:"base_url"`
	} `json:"public_apis"`
}

type deviceAuthorizationResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int64  `json:"expires_in"`
	Interval                int64  `json:"interval"`
}

type tokenEndpointResponse struct {
	AccessToken  string `json:"access_token"`
	IDToken      string `json:"id_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	Error        string `json:"error"`
	ErrorDesc    string `json:"error_description"`
}

func (c CLI) runAuth(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("auth command is required")
	}
	switch args[0] {
	case "login":
		return c.authLogin(ctx, args[1:])
	case "signup":
		return c.authSignup(ctx, args[1:])
	case "logout":
		return c.authLogout(ctx, args[1:])
	case "whoami":
		return c.authWhoami(ctx, args[1:])
	case "accounts":
		return c.authAccounts(ctx, args[1:])
	case "sessions":
		return c.authSessions(ctx, args[1:])
	case "profiles":
		return c.authProfiles(args[1:])
	default:
		return fmt.Errorf("unknown auth command %q", args[0])
	}
}

func (c CLI) authLogin(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("login", flag.ContinueOnError)
	fs.SetOutput(c.err)
	profileName := fs.String("profile", "default", "profile name")
	serverURL := fs.String("server-url", "", "Verself installation URL")
	issuerURL := fs.String("issuer", "", "OIDC issuer URL")
	clientID := fs.String("client-id", "", "OIDC public client ID")
	audience := fs.String("audience", "", "Verself product API audience ID")
	iamURL := fs.String("iam-url", "", "IAM service base URL")
	projectsURL := fs.String("projects-url", "", "Projects service base URL")
	notificationsURL := fs.String("notifications-url", "", "Notifications service base URL")
	billingURL := fs.String("billing-url", "", "Billing service base URL")
	distributionURL := fs.String("distribution-url", "", "Distribution service base URL")
	governanceURL := fs.String("governance-url", "", "Governance service base URL")
	sandboxURL := fs.String("sandbox-url", "", "Sandbox rental service base URL")
	secretsURL := fs.String("secrets-url", "", "Secrets service base URL")
	sourceURL := fs.String("source-url", "", "Source service base URL")
	jsonOut := fs.Bool("json", false, "json output")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: login [--server-url URL] [--profile NAME]")
	}
	if !c.interactiveTTY() {
		return authCommandError("auth.non_interactive_login_required", "interactive device login requires a TTY; use VERSELF_TOKEN or a future workload credential for non-interactive runs")
	}
	store, err := newStore(c.getenv)
	if err != nil {
		return err
	}
	name := strings.TrimSpace(*profileName)
	if name == "" {
		return errors.New("login profile name is required")
	}
	profile := ProfileRecord{
		Version: 1,
		Name:    name,
	}
	if existing, err := store.LoadProfile(name); err == nil {
		profile = existing
		profile.Name = name
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	profile.ServerURL = strings.TrimSpace(firstNonEmpty(*serverURL, c.getenv("VERSELF_SERVER_URL"), profile.ServerURL, verself.DefaultServerURL))
	discovery, err := fetchVerselfDiscovery(ctx, profile.ServerURL)
	if err != nil {
		if firstNonEmpty(*issuerURL, c.getenv("VERSELF_AUTH_ISSUER_URL")) == "" || firstNonEmpty(*clientID, c.getenv("VERSELF_CLI_CLIENT_ID")) == "" {
			return err
		}
	}
	profile.IAMURL = strings.TrimSpace(firstNonEmpty(*iamURL, c.getenv("VERSELF_IAM_API_URL"), discoveryAPIBaseURL(discovery, "iam"), profile.IAMURL))
	profile.ProjectsURL = strings.TrimSpace(firstNonEmpty(*projectsURL, c.getenv("VERSELF_PROJECTS_API_URL"), discoveryAPIBaseURL(discovery, "projects"), profile.ProjectsURL))
	profile.NotificationsURL = strings.TrimSpace(firstNonEmpty(*notificationsURL, c.getenv("VERSELF_NOTIFICATIONS_API_URL"), discoveryAPIBaseURL(discovery, "notifications"), profile.NotificationsURL))
	profile.BillingURL = strings.TrimSpace(firstNonEmpty(*billingURL, c.getenv("VERSELF_BILLING_API_URL"), discoveryAPIBaseURL(discovery, "billing"), profile.BillingURL))
	profile.DistributionURL = strings.TrimSpace(firstNonEmpty(*distributionURL, c.getenv("VERSELF_DISTRIBUTION_API_URL"), discoveryAPIBaseURL(discovery, "distribution"), profile.DistributionURL))
	profile.GovernanceURL = strings.TrimSpace(firstNonEmpty(*governanceURL, c.getenv("VERSELF_GOVERNANCE_API_URL"), discoveryAPIBaseURL(discovery, "governance"), profile.GovernanceURL))
	profile.SandboxURL = strings.TrimSpace(firstNonEmpty(*sandboxURL, c.getenv("VERSELF_SANDBOX_API_URL"), discoveryAPIBaseURL(discovery, "sandbox"), profile.SandboxURL))
	profile.SecretsURL = strings.TrimSpace(firstNonEmpty(*secretsURL, c.getenv("VERSELF_SECRETS_API_URL"), discoveryAPIBaseURL(discovery, "secrets"), profile.SecretsURL))
	profile.SourceURL = strings.TrimSpace(firstNonEmpty(*sourceURL, c.getenv("VERSELF_SOURCE_API_URL"), discoveryAPIBaseURL(discovery, "source"), profile.SourceURL))
	issuer := firstNonEmpty(*issuerURL, c.getenv("VERSELF_AUTH_ISSUER_URL"), discovery.Auth.IssuerURL)
	cliClientID := firstNonEmpty(*clientID, c.getenv("VERSELF_CLI_CLIENT_ID"), discovery.Auth.CLIClientID)
	productAudience := firstNonEmpty(*audience, c.getenv("VERSELF_PRODUCT_API_AUTH_AUDIENCE"), discovery.Auth.ProductAPIAudience)
	credential, err := c.loginCredential(ctx, loginCredentialOptions{
		IssuerURL: issuer,
		ClientID:  cliClientID,
		Audience:  productAudience,
	})
	if err != nil {
		return err
	}
	sdk, err := verself.New(verself.Options{
		BearerToken:      credential.IDToken,
		ServerURL:        profile.ServerURL,
		IAMURL:           profile.IAMURL,
		ProjectsURL:      profile.ProjectsURL,
		NotificationsURL: profile.NotificationsURL,
		BillingURL:       profile.BillingURL,
		DistributionURL:  profile.DistributionURL,
		GovernanceURL:    profile.GovernanceURL,
		SandboxURL:       profile.SandboxURL,
		SecretsURL:       profile.SecretsURL,
		SourceURL:        profile.SourceURL,
	})
	if err != nil {
		return err
	}
	authContext, err := sdk.Auth.CreateDeviceSession(ctx, verself.CreateDeviceSessionInput{
		Channel:     verself.AuthChannelCLI,
		DeviceLabel: cliDeviceLabel(),
	})
	if err != nil {
		return err
	}
	selectedOrg := selectedOrgFromAuthContext(authContext)
	account := AccountRecord{
		Version:         1,
		ProfileName:     profile.Name,
		Handle:          authContext.Account.AccountID,
		AccountID:       authContext.Account.AccountID,
		Issuer:          authContext.Account.Issuer,
		Subject:         authContext.Account.Subject,
		Email:           authContext.Account.Email,
		DisplayName:     authContext.Account.DisplayName,
		DeviceSessionID: authContext.Session.SessionID,
		SelectedOrg:     selectedOrg,
	}
	var previousCredentialRef string
	if existing, err := store.LoadAccount(account.ProfileName, account.Handle); err == nil {
		previousCredentialRef = existing.CredentialRef
		if account.SelectedOrg == nil {
			account.SelectedOrg = existing.SelectedOrg
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	credentialJSON, err := json.Marshal(credential)
	if err != nil {
		return err
	}
	ref, err := store.SaveCredential(string(credentialJSON))
	if err != nil {
		return err
	}
	account.CredentialRef = ref
	if err := store.SaveAccount(account); err != nil {
		_ = store.DeleteCredential(ref)
		return err
	}
	if previousCredentialRef != "" && previousCredentialRef != ref {
		_ = store.DeleteCredential(previousCredentialRef)
	}
	profile.ActiveAccount = account.Handle
	profile.Account = nil
	if err := store.SaveProfile(profile); err != nil {
		return err
	}
	cfg, err := store.LoadConfig()
	if err != nil {
		return err
	}
	cfg.ActiveProfile = profile.Name
	if err := store.SaveConfig(cfg); err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(c.out, map[string]any{"profile": profile, "account": account})
	}
	if account.SelectedOrg == nil {
		return writeln(c.out, "logged in; no organizations available")
	}
	return writef(c.out, "logged in for %s\n", account.SelectedOrg.DisplayName)
}

type loginCredentialOptions struct {
	IssuerURL string
	ClientID  string
	Audience  string
}

func (c CLI) loginCredential(ctx context.Context, opts loginCredentialOptions) (authCredential, error) {
	return c.deviceLogin(ctx, opts)
}

func (c CLI) interactiveTTY() bool {
	file, ok := c.in.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func cliDeviceLabel() string {
	host, err := os.Hostname()
	if err != nil || strings.TrimSpace(host) == "" {
		return "Verself CLI"
	}
	return "Verself CLI on " + strings.TrimSpace(host)
}

func selectedOrgFromAuthContext(context verself.AuthContext) *OrgRef {
	for _, org := range context.Organizations {
		if org.Selected {
			return orgRefFromAuthOrganization(org)
		}
	}
	if len(context.Organizations) == 1 {
		return orgRefFromAuthOrganization(context.Organizations[0])
	}
	return nil
}

func orgRefFromAuthOrganization(org verself.AuthOrganization) *OrgRef {
	return &OrgRef{
		OrgID:       org.OrgID,
		Slug:        org.Slug,
		DisplayName: org.DisplayName,
	}
}

func (c CLI) deviceLogin(ctx context.Context, opts loginCredentialOptions) (authCredential, error) {
	issuer := strings.TrimRight(strings.TrimSpace(opts.IssuerURL), "/")
	if issuer == "" {
		return authCredential{}, errors.New("login requires --issuer or VERSELF_AUTH_ISSUER_URL")
	}
	clientID := strings.TrimSpace(opts.ClientID)
	if clientID == "" {
		return authCredential{}, errors.New("login requires --client-id or VERSELF_CLI_CLIENT_ID")
	}
	discovery, err := fetchOIDCDiscovery(ctx, issuer)
	if err != nil {
		return authCredential{}, err
	}
	if strings.TrimSpace(discovery.DeviceAuthorizationEndpoint) == "" {
		return authCredential{}, errors.New("OIDC issuer does not advertise device authorization")
	}
	if strings.TrimSpace(discovery.TokenEndpoint) == "" {
		return authCredential{}, errors.New("OIDC issuer does not advertise a token endpoint")
	}
	device, err := startDeviceAuthorization(ctx, discovery.DeviceAuthorizationEndpoint, clientID, deviceLoginScopes(opts.Audience))
	if err != nil {
		return authCredential{}, err
	}
	verificationURI := firstNonEmpty(device.VerificationURIComplete, device.VerificationURI)
	if verificationURI == "" || device.UserCode == "" || device.DeviceCode == "" {
		return authCredential{}, errors.New("device authorization response is incomplete")
	}
	if err := writef(c.out, "open %s\ncode %s\n", verificationURI, device.UserCode); err != nil {
		return authCredential{}, err
	}
	token, err := pollDeviceToken(ctx, discovery.TokenEndpoint, clientID, device)
	if err != nil {
		return authCredential{}, err
	}
	expiresAt := time.Time{}
	if token.ExpiresIn > 0 {
		expiresAt = time.Now().UTC().Add(time.Duration(token.ExpiresIn) * time.Second)
	}
	if strings.TrimSpace(token.IDToken) == "" {
		return authCredential{}, errors.New("token endpoint returned no id_token")
	}
	return authCredential{
		AccessToken:  token.AccessToken,
		IDToken:      token.IDToken,
		RefreshToken: token.RefreshToken,
		TokenType:    token.TokenType,
		ExpiresAt:    expiresAt,
		TokenURL:     discovery.TokenEndpoint,
		ClientID:     clientID,
	}, nil
}

func deviceLoginScopes(audience string) string {
	scopes := []string{
		"openid",
		"profile",
		"email",
		"offline_access",
		"urn:zitadel:iam:user:resourceowner",
	}
	if strings.TrimSpace(audience) != "" {
		scopes = append(scopes, "urn:zitadel:iam:org:project:id:"+strings.TrimSpace(audience)+":aud")
	}
	return strings.Join(scopes, " ")
}

func fetchOIDCDiscovery(ctx context.Context, issuer string) (oidcDiscovery, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, issuer+"/.well-known/openid-configuration", nil)
	if err != nil {
		return oidcDiscovery{}, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return oidcDiscovery{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return oidcDiscovery{}, fmt.Errorf("OIDC discovery failed with HTTP %d", resp.StatusCode)
	}
	var discovery oidcDiscovery
	if err := json.NewDecoder(resp.Body).Decode(&discovery); err != nil {
		return oidcDiscovery{}, err
	}
	return discovery, nil
}

func fetchVerselfDiscovery(ctx context.Context, serverURL string) (verselfDiscovery, error) {
	base := strings.TrimRight(strings.TrimSpace(serverURL), "/")
	if base == "" {
		return verselfDiscovery{}, errors.New("verself server URL is required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/.well-known/verself", nil)
	if err != nil {
		return verselfDiscovery{}, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return verselfDiscovery{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return verselfDiscovery{}, fmt.Errorf("verself discovery failed with HTTP %d", resp.StatusCode)
	}
	var discovery verselfDiscovery
	if err := json.NewDecoder(resp.Body).Decode(&discovery); err != nil {
		return verselfDiscovery{}, err
	}
	if discovery.SchemaVersion != 1 {
		return verselfDiscovery{}, fmt.Errorf("unsupported Verself discovery schema version %d", discovery.SchemaVersion)
	}
	return discovery, nil
}

func discoveryAPIBaseURL(discovery verselfDiscovery, service string) string {
	if discovery.PublicAPIs == nil {
		return ""
	}
	entry, ok := discovery.PublicAPIs[service]
	if !ok {
		return ""
	}
	return strings.TrimSpace(entry.BaseURL)
}

func startDeviceAuthorization(ctx context.Context, endpoint, clientID, scope string) (deviceAuthorizationResponse, error) {
	values := url.Values{
		"client_id": {clientID},
		"scope":     {scope},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return deviceAuthorizationResponse{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return deviceAuthorizationResponse{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return deviceAuthorizationResponse{}, fmt.Errorf("device authorization failed with HTTP %d", resp.StatusCode)
	}
	var out deviceAuthorizationResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return deviceAuthorizationResponse{}, err
	}
	return out, nil
}

func pollDeviceToken(ctx context.Context, tokenURL, clientID string, device deviceAuthorizationResponse) (tokenEndpointResponse, error) {
	interval := time.Duration(device.Interval) * time.Second
	if interval <= 0 {
		interval = 5 * time.Second
	}
	expiresAt := time.Now().UTC().Add(time.Duration(device.ExpiresIn) * time.Second)
	for {
		if !expiresAt.IsZero() && time.Now().UTC().After(expiresAt) {
			return tokenEndpointResponse{}, authCommandError("auth.device_code_expired", "device authorization expired")
		}
		select {
		case <-ctx.Done():
			return tokenEndpointResponse{}, ctx.Err()
		case <-time.After(interval):
		}
		token, err := requestDeviceToken(ctx, tokenURL, clientID, device.DeviceCode)
		if err != nil {
			return tokenEndpointResponse{}, err
		}
		switch token.Error {
		case "":
			if strings.TrimSpace(token.AccessToken) == "" {
				return tokenEndpointResponse{}, errors.New("token endpoint returned no access_token")
			}
			if strings.TrimSpace(token.IDToken) == "" {
				return tokenEndpointResponse{}, errors.New("token endpoint returned no id_token")
			}
			return token, nil
		case "authorization_pending":
			continue
		case "slow_down":
			interval += 5 * time.Second
			continue
		case "access_denied":
			return tokenEndpointResponse{}, authCommandError("auth.device_authorization_denied", "device authorization was denied")
		case "expired_token":
			return tokenEndpointResponse{}, authCommandError("auth.device_code_expired", "device authorization expired")
		default:
			if token.ErrorDesc != "" {
				return tokenEndpointResponse{}, fmt.Errorf("%s: %s", token.Error, token.ErrorDesc)
			}
			return tokenEndpointResponse{}, errors.New(token.Error)
		}
	}
}

func requestDeviceToken(ctx context.Context, tokenURL, clientID, deviceCode string) (tokenEndpointResponse, error) {
	values := url.Values{
		"grant_type":  {deviceGrantType},
		"device_code": {deviceCode},
		"client_id":   {clientID},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(values.Encode()))
	if err != nil {
		return tokenEndpointResponse{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return tokenEndpointResponse{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	var token tokenEndpointResponse
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return tokenEndpointResponse{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if token.Error == "" {
			return tokenEndpointResponse{}, fmt.Errorf("token endpoint failed with HTTP %d", resp.StatusCode)
		}
		return token, nil
	}
	return token, nil
}

func parseStoredAuthCredential(value string) (authCredential, bool) {
	var credential authCredential
	if err := json.Unmarshal([]byte(value), &credential); err != nil {
		return authCredential{}, false
	}
	return credential, strings.TrimSpace(credential.AccessToken) != ""
}

type profileCredentialSource struct {
	store         *Store
	profileName   string
	accountHandle string
}

func (s *profileCredentialSource) BearerToken(ctx context.Context) (string, error) {
	if s == nil || s.store == nil {
		return "", errors.New("auth credential source is not initialized")
	}
	account, err := s.store.LoadAccount(s.profileName, s.accountHandle)
	if err != nil {
		return "", err
	}
	value, err := s.store.ReadCredential(account.CredentialRef)
	if err != nil {
		return "", err
	}
	credential, ok := parseStoredAuthCredential(value)
	if !ok {
		return "", errors.New("active auth profile credential is invalid")
	}
	if credentialRefreshDue(credential) {
		refreshed, err := refreshAuthCredential(ctx, credential)
		if err != nil {
			return "", err
		}
		credential = refreshed
		encoded, err := json.Marshal(credential)
		if err != nil {
			return "", err
		}
		if err := s.store.UpdateCredential(account.CredentialRef, string(encoded)); err != nil {
			return "", err
		}
	}
	if strings.TrimSpace(credential.AccessToken) == "" {
		return "", errors.New("active auth profile credential is empty")
	}
	return strings.TrimSpace(credential.AccessToken), nil
}

func credentialRefreshDue(credential authCredential) bool {
	return !credential.ExpiresAt.IsZero() && time.Now().UTC().Add(2*time.Minute).After(credential.ExpiresAt)
}

func refreshAuthCredential(ctx context.Context, credential authCredential) (authCredential, error) {
	refreshToken := strings.TrimSpace(credential.RefreshToken)
	tokenURL := strings.TrimSpace(credential.TokenURL)
	clientID := strings.TrimSpace(credential.ClientID)
	if refreshToken == "" || tokenURL == "" || clientID == "" {
		return authCredential{}, authCommandError("auth.reauthentication_required", "stored credential cannot be refreshed")
	}
	values := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {clientID},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(values.Encode()))
	if err != nil {
		return authCredential{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return authCredential{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	var token tokenEndpointResponse
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return authCredential{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || strings.TrimSpace(token.Error) != "" {
		if strings.TrimSpace(token.Error) != "" {
			return authCredential{}, authCommandError("auth.reauthentication_required", firstNonEmpty(token.ErrorDesc, token.Error))
		}
		return authCredential{}, fmt.Errorf("refresh token request failed with HTTP %d", resp.StatusCode)
	}
	if strings.TrimSpace(token.AccessToken) == "" {
		return authCredential{}, errors.New("refresh token response returned no access_token")
	}
	credential.AccessToken = token.AccessToken
	if strings.TrimSpace(token.IDToken) != "" {
		credential.IDToken = token.IDToken
	}
	credential.TokenType = firstNonEmpty(token.TokenType, credential.TokenType, "Bearer")
	if strings.TrimSpace(token.RefreshToken) != "" {
		credential.RefreshToken = token.RefreshToken
	}
	if token.ExpiresIn > 0 {
		credential.ExpiresAt = time.Now().UTC().Add(time.Duration(token.ExpiresIn) * time.Second)
	}
	return credential, nil
}

func (c CLI) authLogout(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("logout", flag.ContinueOnError)
	fs.SetOutput(c.err)
	profileName := fs.String("profile", "", "profile name")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: logout [--profile NAME]")
	}
	store, profile, accounts, err := c.loadProfileAccounts(*profileName)
	if err != nil {
		return err
	}
	for _, account := range accounts {
		if err := c.revokeStoredAccountDevice(ctx, store, profile, account); err != nil {
			return err
		}
	}
	name := profile.Name
	if err := store.DeleteProfile(name); err != nil {
		return err
	}
	if name == "" {
		return writeln(c.out, "logged out")
	}
	return writef(c.out, "logged out %s\n", name)
}

func (c CLI) authWhoami(ctx context.Context, args []string) error {
	fs, serviceFlags := serviceFlagSet("whoami", c.err)
	jsonOut := fs.Bool("json", false, "json output")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: whoami [--json]")
	}
	client, _, err := c.serviceClientWithProfile(*serviceFlags)
	if err != nil {
		return err
	}
	authContext, err := client.Auth.GetContext(ctx)
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(c.out, authContext)
	}
	selected := selectedOrgLabel(authContext)
	return writef(c.out, "%s\t%s\t%s\n", authContext.Account.AccountID, authContext.Account.Email, selected)
}

func selectedOrgLabel(context verself.AuthContext) string {
	for _, org := range context.Organizations {
		if org.Selected {
			return org.OrgID + "\t" + org.DisplayName
		}
	}
	if len(context.Organizations) == 1 {
		org := context.Organizations[0]
		return org.OrgID + "\t" + org.DisplayName
	}
	return "no-org\t"
}

func (c CLI) authSessions(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("sessions command is required")
	}
	switch args[0] {
	case "list", "ls":
		return c.authSessionsList(ctx, args[1:])
	case "revoke", "rm":
		return c.authSessionsRevoke(ctx, args[1:])
	default:
		return fmt.Errorf("unknown sessions command %q", args[0])
	}
}

func (c CLI) authSessionsList(ctx context.Context, args []string) error {
	fs, serviceFlags := serviceFlagSet("sessions list", c.err)
	jsonOut := fs.Bool("json", false, "json output")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: sessions list [--json]")
	}
	client, err := c.serviceClient(*serviceFlags)
	if err != nil {
		return err
	}
	sessions, err := client.Auth.ListDeviceSessions(ctx)
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(c.out, sessions)
	}
	for _, session := range sessions {
		current := ""
		if session.Current {
			current = "current"
		}
		if err := writef(c.out, "%s\t%s\t%s\t%s\t%s\n", session.SessionID, session.State, session.Channel, session.DeviceLabel, current); err != nil {
			return err
		}
	}
	return nil
}

func (c CLI) authSessionsRevoke(ctx context.Context, args []string) error {
	fs, serviceFlags := serviceFlagSet("sessions revoke", c.err)
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: sessions revoke <session-id>")
	}
	client, err := c.serviceClient(*serviceFlags)
	if err != nil {
		return err
	}
	if err := client.Auth.RevokeDeviceSession(ctx, fs.Arg(0)); err != nil {
		return err
	}
	return writef(c.out, "revoked session %s\n", strings.TrimSpace(fs.Arg(0)))
}

func (c CLI) authAccounts(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("accounts command is required")
	}
	switch args[0] {
	case "list", "ls":
		return c.authAccountsList(args[1:])
	case "use":
		return c.authAccountsUse(ctx, args[1:])
	case "logout", "remove", "rm":
		return c.authAccountsLogout(ctx, args[1:])
	default:
		return fmt.Errorf("unknown accounts command %q", args[0])
	}
}

func (c CLI) authAccountsList(args []string) error {
	fs := flag.NewFlagSet("accounts list", flag.ContinueOnError)
	fs.SetOutput(c.err)
	profileName := fs.String("profile", "", "profile name")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: accounts list [--profile NAME]")
	}
	_, profile, accounts, err := c.loadProfileAccounts(*profileName)
	if err != nil {
		return err
	}
	return writeJSON(c.out, map[string]any{
		"profile":       profile.Name,
		"activeAccount": profile.ActiveAccount,
		"accounts":      accounts,
	})
}

func (c CLI) authAccountsUse(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("accounts use", flag.ContinueOnError)
	fs.SetOutput(c.err)
	profileName := fs.String("profile", "", "profile name")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: accounts use <handle|email|subject> [--profile NAME]")
	}
	store, profile, accounts, err := c.loadProfileAccounts(*profileName)
	if err != nil {
		return err
	}
	account, err := matchCLIAccount(accounts, fs.Arg(0))
	if err != nil {
		return err
	}
	profile.ActiveAccount = account.Handle
	if err := store.SaveProfile(profile); err != nil {
		return err
	}
	cfg, err := store.LoadConfig()
	if err != nil {
		return err
	}
	cfg.ActiveProfile = profile.Name
	if err := store.SaveConfig(cfg); err != nil {
		return err
	}
	client, _, err := c.serviceClientWithProfile(serviceClientFlags{})
	if err != nil {
		return err
	}
	if _, err := client.Auth.GetContext(ctx); err != nil {
		return fmt.Errorf("%w; run `verself login --profile %s` to refresh this account", err, profile.Name)
	}
	return writeJSON(c.out, map[string]any{
		"profile": profile.Name,
		"account": account,
		"message": "active account selected",
	})
}

func (c CLI) authAccountsLogout(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("accounts logout", flag.ContinueOnError)
	fs.SetOutput(c.err)
	profileName := fs.String("profile", "", "profile name")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() > 1 {
		return errors.New("usage: accounts logout [handle|email|subject] [--profile NAME]")
	}
	store, profile, accounts, err := c.loadProfileAccounts(*profileName)
	if err != nil {
		return err
	}
	var account AccountRecord
	if fs.NArg() == 0 {
		if profile.ActiveAccount == "" {
			return errors.New("profile has no active account")
		}
		account, err = matchCLIAccount(accounts, profile.ActiveAccount)
	} else {
		account, err = matchCLIAccount(accounts, fs.Arg(0))
	}
	if err != nil {
		return err
	}
	if err := c.revokeStoredAccountDevice(ctx, store, profile, account); err != nil {
		return err
	}
	if err := store.DeleteAccount(profile.Name, account.Handle); err != nil {
		return err
	}
	if profile.ActiveAccount == account.Handle {
		profile.ActiveAccount = ""
		if err := store.SaveProfile(profile); err != nil {
			return err
		}
	}
	return writeJSON(c.out, map[string]any{
		"profile": profile.Name,
		"account": account.Handle,
		"message": "account logged out",
	})
}

func (c CLI) revokeStoredAccountDevice(ctx context.Context, store *Store, profile ProfileRecord, account AccountRecord) error {
	sessionID := strings.TrimSpace(account.DeviceSessionID)
	if sessionID == "" {
		return authCommandError("auth.reauthentication_required", "stored account has no device session; sign in again before logout")
	}
	client, err := c.serviceClientForStoredAccount(store, profile, account)
	if err != nil {
		return err
	}
	if err := client.Auth.RevokeDeviceSession(ctx, sessionID); err != nil {
		return err
	}
	return nil
}

func (c CLI) serviceClientForStoredAccount(store *Store, profile ProfileRecord, account AccountRecord) (*verself.Client, error) {
	if store == nil {
		return nil, errors.New("auth credential store is required")
	}
	profileName := strings.TrimSpace(profile.Name)
	accountHandle := strings.TrimSpace(account.Handle)
	if profileName == "" || accountHandle == "" {
		return nil, errors.New("stored account identity is incomplete")
	}
	return verself.New(verself.Options{
		CredentialSource: &profileCredentialSource{
			store:         store,
			profileName:   profileName,
			accountHandle: accountHandle,
		},
		DeviceSessionID:  strings.TrimSpace(account.DeviceSessionID),
		ServerURL:        profile.ServerURL,
		IAMURL:           profile.IAMURL,
		ProjectsURL:      profile.ProjectsURL,
		NotificationsURL: profile.NotificationsURL,
		BillingURL:       profile.BillingURL,
		DistributionURL:  profile.DistributionURL,
		GovernanceURL:    profile.GovernanceURL,
		SandboxURL:       profile.SandboxURL,
		SecretsURL:       profile.SecretsURL,
		SourceURL:        profile.SourceURL,
	})
}

func (c CLI) loadProfileAccounts(profileName string) (*Store, ProfileRecord, []AccountRecord, error) {
	store, err := newStore(c.getenv)
	if err != nil {
		return nil, ProfileRecord{}, nil, err
	}
	profile, err := store.LoadProfile(strings.TrimSpace(profileName))
	if err != nil {
		return nil, ProfileRecord{}, nil, err
	}
	accounts, err := store.ListAccounts(profile.Name)
	if err != nil {
		return nil, ProfileRecord{}, nil, err
	}
	return store, profile, accounts, nil
}

func matchCLIAccount(accounts []AccountRecord, value string) (AccountRecord, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return AccountRecord{}, errors.New("account selector is required")
	}
	var matched *AccountRecord
	for i := range accounts {
		account := accounts[i]
		if account.Handle != value && account.AccountID != value && account.Email != value && account.Subject != value {
			continue
		}
		if matched != nil {
			return AccountRecord{}, fmt.Errorf("account selector %q is ambiguous", value)
		}
		matched = &account
	}
	if matched == nil {
		return AccountRecord{}, fmt.Errorf("account %q was not found", value)
	}
	return *matched, nil
}

func (c CLI) authProfiles(args []string) error {
	fs := flag.NewFlagSet("profiles", flag.ContinueOnError)
	fs.SetOutput(c.err)
	jsonOut := fs.Bool("json", false, "json output")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: profiles [--json]")
	}
	store, err := newStore(c.getenv)
	if err != nil {
		return err
	}
	profiles, err := store.ListProfiles()
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(c.out, profiles)
	}
	for _, profile := range profiles {
		if err := writeln(c.out, profile); err != nil {
			return err
		}
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
