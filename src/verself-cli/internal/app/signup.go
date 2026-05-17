package app

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"strings"

	verself "github.com/verself/verself-go"
)

func (c CLI) runSignup(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("signup", flag.ContinueOnError)
	fs.SetOutput(c.err)
	profileName := fs.String("profile", "default", "profile name")
	tokenFile := fs.String("token-file", "", "read bearer token from owner-only file")
	issuerURL := fs.String("issuer", "", "OIDC issuer URL")
	clientID := fs.String("client-id", "", "OIDC public client ID")
	audience := fs.String("audience", "", "Verself product API audience ID")
	iamURL := fs.String("iam-url", "", "IAM service base URL")
	projectsURL := fs.String("projects-url", "", "Projects service base URL")
	notificationsURL := fs.String("notifications-url", "", "Notifications service base URL")
	billingURL := fs.String("billing-url", "", "Billing service base URL")
	governanceURL := fs.String("governance-url", "", "Governance service base URL")
	sandboxURL := fs.String("sandbox-url", "", "Sandbox rental service base URL")
	secretsURL := fs.String("secrets-url", "", "Secrets service base URL")
	sourceURL := fs.String("source-url", "", "Source service base URL")
	displayName := fs.String("display-name", "", "organization display name")
	slug := fs.String("slug", "", "organization slug")
	idempotencyKey := fs.String("idempotency-key", "", "stable mutation key")
	jsonOut := fs.Bool("json", false, "json output")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: signup --display-name NAME [--slug SLUG]")
	}
	if strings.TrimSpace(*displayName) == "" {
		return errors.New("signup requires --display-name")
	}
	profile := ProfileRecord{
		Version:          1,
		Name:             strings.TrimSpace(*profileName),
		IAMURL:           strings.TrimSpace(firstNonEmpty(*iamURL, c.getenv("VERSELF_IAM_API_URL"))),
		ProjectsURL:      strings.TrimSpace(firstNonEmpty(*projectsURL, c.getenv("VERSELF_PROJECTS_API_URL"))),
		NotificationsURL: strings.TrimSpace(firstNonEmpty(*notificationsURL, c.getenv("VERSELF_NOTIFICATIONS_API_URL"))),
		BillingURL:       strings.TrimSpace(firstNonEmpty(*billingURL, c.getenv("VERSELF_BILLING_API_URL"))),
		GovernanceURL:    strings.TrimSpace(firstNonEmpty(*governanceURL, c.getenv("VERSELF_GOVERNANCE_API_URL"))),
		SandboxURL:       strings.TrimSpace(firstNonEmpty(*sandboxURL, c.getenv("VERSELF_SANDBOX_API_URL"))),
		SecretsURL:       strings.TrimSpace(firstNonEmpty(*secretsURL, c.getenv("VERSELF_SECRETS_API_URL"))),
		SourceURL:        strings.TrimSpace(firstNonEmpty(*sourceURL, c.getenv("VERSELF_SOURCE_API_URL"))),
	}
	if profile.Name == "" {
		return errors.New("signup profile name is required")
	}
	credential, err := c.loginCredential(ctx, loginCredentialOptions{
		TokenFile: strings.TrimSpace(*tokenFile),
		IssuerURL: firstNonEmpty(*issuerURL, c.getenv("VERSELF_AUTH_ISSUER_URL")),
		ClientID:  firstNonEmpty(*clientID, c.getenv("VERSELF_CLI_CLIENT_ID")),
		Audience:  firstNonEmpty(*audience, c.getenv("VERSELF_PRODUCT_API_AUTH_AUDIENCE")),
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
		GovernanceURL:    profile.GovernanceURL,
		SandboxURL:       profile.SandboxURL,
		SecretsURL:       profile.SecretsURL,
		SourceURL:        profile.SourceURL,
	})
	if err != nil {
		return err
	}
	var slugPtr *string
	if strings.TrimSpace(*slug) != "" {
		value := strings.TrimSpace(*slug)
		slugPtr = &value
	}
	org, err := sdk.IAM.CreateOrganization(ctx, verself.CreateOrganizationInput{
		DisplayName:    strings.TrimSpace(*displayName),
		Slug:           slugPtr,
		IdempotencyKey: *idempotencyKey,
	})
	if err != nil {
		return err
	}
	profile.SelectedOrg = orgRefFromSDK(org)
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
	return writef(c.out, "signed up for %s\n", org.DisplayName)
}
