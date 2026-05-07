package app

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	verself "github.com/verself/verself-go"
)

const deviceGrantType = "urn:ietf:params:oauth:grant-type:device_code"

type authCredential struct {
	AccessToken  string    `json:"access_token"`
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
	case "logout":
		return c.authLogout(args[1:])
	case "whoami":
		return c.authWhoami(ctx, args[1:])
	case "token":
		return c.authToken(args[1:])
	case "profiles":
		return c.authProfiles(args[1:])
	default:
		return fmt.Errorf("unknown auth command %q", args[0])
	}
}

func (c CLI) authLogin(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("auth login", flag.ContinueOnError)
	fs.SetOutput(c.err)
	profileName := fs.String("profile", "default", "profile name")
	tokenFile := fs.String("token-file", "", "read bearer token from owner-only file")
	issuerURL := fs.String("issuer", "", "OIDC issuer URL")
	clientID := fs.String("client-id", "", "OIDC public client ID")
	audience := fs.String("audience", "", "Zitadel project audience ID")
	iamURL := fs.String("iam-url", "", "IAM service base URL")
	projectsURL := fs.String("projects-url", "", "Projects service base URL")
	notificationsURL := fs.String("notifications-url", "", "Notifications service base URL")
	billingURL := fs.String("billing-url", "", "Billing service base URL")
	sandboxURL := fs.String("sandbox-url", "", "Sandbox rental service base URL")
	secretsURL := fs.String("secrets-url", "", "Secrets service base URL")
	sourceURL := fs.String("source-url", "", "Source service base URL")
	jsonOut := fs.Bool("json", false, "json output")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: auth login --token-file PATH [--profile NAME]")
	}
	profile := ProfileRecord{
		Version:          1,
		Name:             strings.TrimSpace(*profileName),
		IAMURL:           strings.TrimSpace(firstNonEmpty(*iamURL, c.getenv("VERSELF_IAM_API_URL"))),
		ProjectsURL:      strings.TrimSpace(firstNonEmpty(*projectsURL, c.getenv("VERSELF_PROJECTS_API_URL"))),
		NotificationsURL: strings.TrimSpace(firstNonEmpty(*notificationsURL, c.getenv("VERSELF_NOTIFICATIONS_API_URL"))),
		BillingURL:       strings.TrimSpace(firstNonEmpty(*billingURL, c.getenv("VERSELF_BILLING_API_URL"))),
		SandboxURL:       strings.TrimSpace(firstNonEmpty(*sandboxURL, c.getenv("VERSELF_SANDBOX_API_URL"))),
		SecretsURL:       strings.TrimSpace(firstNonEmpty(*secretsURL, c.getenv("VERSELF_SECRETS_API_URL"))),
		SourceURL:        strings.TrimSpace(firstNonEmpty(*sourceURL, c.getenv("VERSELF_SOURCE_API_URL"))),
	}
	if profile.Name == "" {
		return errors.New("auth login profile name is required")
	}
	credential, err := c.loginCredential(ctx, loginCredentialOptions{
		TokenFile: strings.TrimSpace(*tokenFile),
		IssuerURL: firstNonEmpty(*issuerURL, c.getenv("VERSELF_AUTH_ISSUER_URL")),
		ClientID:  firstNonEmpty(*clientID, c.getenv("VERSELF_CLI_CLIENT_ID")),
		Audience:  firstNonEmpty(*audience, c.getenv("VERSELF_IAM_SERVICE_AUTH_AUDIENCE")),
	})
	if err != nil {
		return err
	}
	sdk, err := verself.New(verself.Options{
		BearerToken:      credential.AccessToken,
		IAMURL:           profile.IAMURL,
		ProjectsURL:      profile.ProjectsURL,
		NotificationsURL: profile.NotificationsURL,
		BillingURL:       profile.BillingURL,
		SandboxURL:       profile.SandboxURL,
		SecretsURL:       profile.SecretsURL,
		SourceURL:        profile.SourceURL,
	})
	if err != nil {
		return err
	}
	org, err := sdk.IAM.GetOrganization(ctx)
	if err != nil {
		return err
	}
	profile.SelectedOrg = &OrgRef{OrgID: org.OrgID, Slug: org.Slug, DisplayName: org.DisplayName}
	store, err := newStore(c.getenv)
	if err != nil {
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
	profile.TokenRef = ref
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
		return writeJSON(c.out, profile)
	}
	return writef(c.out, "logged in as %s for %s\n", org.Caller.DisplayName, org.DisplayName)
}

type loginCredentialOptions struct {
	TokenFile string
	IssuerURL string
	ClientID  string
	Audience  string
}

func (c CLI) loginCredential(ctx context.Context, opts loginCredentialOptions) (authCredential, error) {
	if opts.TokenFile != "" {
		token, err := readTokenFile(opts.TokenFile)
		if err != nil {
			return authCredential{}, err
		}
		return authCredential{AccessToken: token, TokenType: "Bearer"}, nil
	}
	return c.deviceLogin(ctx, opts)
}

func (c CLI) deviceLogin(ctx context.Context, opts loginCredentialOptions) (authCredential, error) {
	issuer := strings.TrimRight(strings.TrimSpace(opts.IssuerURL), "/")
	if issuer == "" {
		return authCredential{}, errors.New("auth login requires --issuer or VERSELF_AUTH_ISSUER_URL")
	}
	clientID := strings.TrimSpace(opts.ClientID)
	if clientID == "" {
		return authCredential{}, errors.New("auth login requires --client-id or VERSELF_CLI_CLIENT_ID")
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
	return authCredential{
		AccessToken:  token.AccessToken,
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
		"urn:zitadel:iam:org:projects:roles",
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
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return oidcDiscovery{}, fmt.Errorf("OIDC discovery failed with HTTP %d", resp.StatusCode)
	}
	var discovery oidcDiscovery
	if err := json.NewDecoder(resp.Body).Decode(&discovery); err != nil {
		return oidcDiscovery{}, err
	}
	return discovery, nil
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
	defer resp.Body.Close()
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
			return tokenEndpointResponse{}, errors.New("device authorization expired")
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
			return token, nil
		case "authorization_pending":
			continue
		case "slow_down":
			interval += 5 * time.Second
			continue
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
	defer resp.Body.Close()
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

func (c CLI) authLogout(args []string) error {
	fs := flag.NewFlagSet("auth logout", flag.ContinueOnError)
	fs.SetOutput(c.err)
	profileName := fs.String("profile", "", "profile name")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: auth logout [--profile NAME]")
	}
	store, err := newStore(c.getenv)
	if err != nil {
		return err
	}
	name := strings.TrimSpace(*profileName)
	if name == "" {
		cfg, err := store.LoadConfig()
		if err != nil {
			return err
		}
		name = cfg.ActiveProfile
	}
	if err := store.DeleteProfile(name); err != nil {
		return err
	}
	if name == "" {
		return writeln(c.out, "logged out")
	}
	return writef(c.out, "logged out %s\n", name)
}

func (c CLI) authWhoami(ctx context.Context, args []string) error {
	fs, serviceFlags := serviceFlagSet("auth whoami", c.err)
	jsonOut := fs.Bool("json", false, "json output")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: auth whoami [--json]")
	}
	client, err := c.serviceClient(*serviceFlags)
	if err != nil {
		return err
	}
	org, err := client.IAM.GetOrganization(ctx)
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(c.out, org)
	}
	return writef(c.out, "%s\t%s\t%s\t%s\n", org.Caller.UserID, org.Caller.DisplayName, org.OrgID, org.DisplayName)
}

func (c CLI) authToken(args []string) error {
	fs, serviceFlags := serviceFlagSet("auth token", c.err)
	jsonOut := fs.Bool("json", false, "json output")
	audience := fs.String("audience", "", "requested audience label")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: auth token [--audience AUDIENCE]")
	}
	token, _, err := c.bearerTokenWithProfile(serviceFlags.tokenFile)
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(c.out, map[string]string{
			"access_token": token,
			"audience":     strings.TrimSpace(*audience),
		})
	}
	return writef(c.out, "%s\n", token)
}

func (c CLI) authProfiles(args []string) error {
	fs := flag.NewFlagSet("auth profiles", flag.ContinueOnError)
	fs.SetOutput(c.err)
	jsonOut := fs.Bool("json", false, "json output")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: auth profiles [--json]")
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
