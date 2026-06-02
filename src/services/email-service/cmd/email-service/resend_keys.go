package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/verself/email-service/internal/resendkeys"
)

func runResendKeysCLI(ctx context.Context) (bool, error) {
	if len(os.Args) < 3 || os.Args[1] != "resend-keys" {
		return false, nil
	}
	switch os.Args[2] {
	case "apply":
		return true, applyResendKeys(ctx, os.Args[3:])
	default:
		return true, fmt.Errorf("usage: email-service resend-keys apply")
	}
}

func applyResendKeys(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("resend-keys apply", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	site := fs.String("site", envOr("VERSELF_SITE", ""), "Deployment site.")
	openBaoAddr := fs.String("openbao-addr", envOr("BAO_ADDR", envOr("VAULT_ADDR", "https://127.0.0.1:8200")), "OpenBao API address.")
	openBaoCACert := fs.String("openbao-ca-cert", envOr("BAO_CACERT", envOr("VAULT_CACERT", "/etc/openbao/tls/cert.pem")), "OpenBao CA certificate.")
	openBaoToken := fs.String("openbao-token", envOr("VAULT_TOKEN", ""), "OpenBao token. Defaults to VAULT_TOKEN from Nomad.")
	resendAPIURL := fs.String("resend-api-url", envOr("RESEND_API_URL", "https://api.resend.com"), "Resend API base URL.")
	adminSecret := fs.String("admin-secret", envOr("EMAIL_SERVICE_RESEND_ADMIN_SECRET", resendkeys.DefaultAdminSecret), "OpenBao runtime secret containing the Resend full-access API key.")
	metadataSecret := fs.String("metadata-secret", envOr("EMAIL_SERVICE_RESEND_METADATA_SECRET", resendkeys.DefaultMetadataSecret), "OpenBao runtime secret for Resend child key metadata.")
	childSecrets := fs.String("child-secrets", envOr("EMAIL_SERVICE_RESEND_CHILD_SECRETS", strings.Join(resendkeys.DefaultChildSecrets, ",")), "Comma-separated OpenBao runtime secrets that receive the Resend sending key.")
	keyNamePrefix := fs.String("key-name-prefix", envOr("EMAIL_SERVICE_RESEND_KEY_NAME_PREFIX", ""), "Optional Resend child key name prefix.")
	rotate := fs.Bool("rotate", envBool("EMAIL_SERVICE_RESEND_ROTATE", false), "Create a new child key even when existing children are present.")
	if err := fs.Parse(args); err != nil {
		return err
	}
	store, err := resendkeys.NewOpenBaoStore(*openBaoAddr, *openBaoCACert, *openBaoToken)
	if err != nil {
		return err
	}
	api := openBaoBackedResendAPI{
		store:       store,
		adminSecret: *adminSecret,
		baseURL:     *resendAPIURL,
		httpClient:  &http.Client{Timeout: 10 * time.Second},
	}
	result, err := resendkeys.Apply(ctx, resendkeys.Config{
		Site:           *site,
		AdminSecret:    *adminSecret,
		ChildSecrets:   splitCSV(*childSecrets),
		MetadataSecret: *metadataSecret,
		KeyNamePrefix:  *keyNamePrefix,
		Rotate:         *rotate,
	}, store, api)
	if err != nil {
		return err
	}
	if result.Created {
		fmt.Printf("email-service resend-keys: created key_id=%s key_name=%s token_sha256=%s updated=%s\n", result.KeyID, result.KeyName, result.TokenSHA256, strings.Join(result.UpdatedSecrets, ","))
		return nil
	}
	fmt.Println("email-service resend-keys: child keys already present")
	return nil
}

type openBaoBackedResendAPI struct {
	store       resendkeys.SecretStore
	adminSecret string
	baseURL     string
	httpClient  *http.Client
}

func (a openBaoBackedResendAPI) CreateAPIKey(ctx context.Context, req resendkeys.CreateAPIKeyRequest) (resendkeys.CreateAPIKeyResponse, error) {
	api, err := a.api(ctx)
	if err != nil {
		return resendkeys.CreateAPIKeyResponse{}, err
	}
	return api.CreateAPIKey(ctx, req)
}

func (a openBaoBackedResendAPI) DeleteAPIKey(ctx context.Context, id string) error {
	api, err := a.api(ctx)
	if err != nil {
		return err
	}
	return api.DeleteAPIKey(ctx, id)
}

func (a openBaoBackedResendAPI) api(ctx context.Context) (*resendkeys.HTTPResendAPI, error) {
	adminToken, found, err := a.store.Read(ctx, a.adminSecret)
	if err != nil {
		return nil, fmt.Errorf("read Resend full-access key: %w", err)
	}
	if !found || strings.TrimSpace(adminToken) == "" {
		return nil, fmt.Errorf("resend full-access key %s is empty or missing in OpenBao", a.adminSecret)
	}
	api, err := resendkeys.NewHTTPResendAPI(a.baseURL, adminToken, a.httpClient)
	if err != nil {
		return nil, err
	}
	return api, nil
}

func splitCSV(raw string) []string {
	var out []string
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	switch value {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}
