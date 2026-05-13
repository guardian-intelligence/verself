package verself

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"

	billingcore "github.com/verself/verself-go/internal/generated/billing"
	governancecore "github.com/verself/verself-go/internal/generated/governance"
	iamcore "github.com/verself/verself-go/internal/generated/iam"
	notificationscore "github.com/verself/verself-go/internal/generated/notifications"
	projectscore "github.com/verself/verself-go/internal/generated/projects"
	sandboxcore "github.com/verself/verself-go/internal/generated/sandbox"
	secretscore "github.com/verself/verself-go/internal/generated/secrets"
	sourcecore "github.com/verself/verself-go/internal/generated/source"
)

const (
	DefaultServerURL        = "https://verself.sh"
	DefaultIAMURL           = "https://iam.api.verself.sh"
	DefaultProjectsURL      = "https://projects.api.verself.sh"
	DefaultNotificationsURL = "https://notifications.api.verself.sh"
	DefaultBillingURL       = "https://billing.api.verself.sh"
	DefaultGovernanceURL    = "https://governance.api.verself.sh"
	DefaultSandboxURL       = "https://sandbox.api.verself.sh"
	DefaultSecretsURL       = "https://secrets.api.verself.sh"
	DefaultSourceURL        = "https://source.api.verself.sh"
)

type Options struct {
	BearerToken      string
	ServerURL        string
	IAMURL           string
	ProjectsURL      string
	NotificationsURL string
	BillingURL       string
	GovernanceURL    string
	SandboxURL       string
	SecretsURL       string
	SourceURL        string
	HTTPClient       *http.Client
	Traceparent      string
}

type Client struct {
	IAM           *IAMClient
	Projects      *ProjectsClient
	Notifications *NotificationsClient
	Billing       *BillingClient
	Governance    *GovernanceClient
	Sandbox       *SandboxClient
	Secrets       *SecretsClient
	Source        *SourceClient
}

func New(options Options) (*Client, error) {
	token := strings.TrimSpace(options.BearerToken)
	if token == "" {
		return nil, errors.New("verself sdk: bearer token is required")
	}
	iamURL, err := serviceURL(options.IAMURL, options.ServerURL, "iam")
	if err != nil {
		return nil, err
	}
	projectsURL, err := serviceURL(options.ProjectsURL, options.ServerURL, "projects")
	if err != nil {
		return nil, err
	}
	notificationsURL, err := serviceURL(options.NotificationsURL, options.ServerURL, "notifications")
	if err != nil {
		return nil, err
	}
	billingURL, err := serviceURL(options.BillingURL, options.ServerURL, "billing")
	if err != nil {
		return nil, err
	}
	governanceURL, err := serviceURL(options.GovernanceURL, options.ServerURL, "governance")
	if err != nil {
		return nil, err
	}
	sourceURL, err := serviceURL(options.SourceURL, options.ServerURL, "source")
	if err != nil {
		return nil, err
	}
	sandboxURL, err := serviceURL(options.SandboxURL, options.ServerURL, "sandbox")
	if err != nil {
		return nil, err
	}
	secretsURL, err := serviceURL(options.SecretsURL, options.ServerURL, "secrets")
	if err != nil {
		return nil, err
	}
	httpClient := options.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	iamEditor := iamRequestEditor(token, options.Traceparent)
	generatedIAM, err := iamcore.NewClient(
		iamURL,
		iamcore.WithHTTPClient(httpClient),
		iamcore.WithRequestEditorFn(iamEditor),
	)
	if err != nil {
		return nil, err
	}
	projectsEditor := projectsRequestEditor(token, options.Traceparent)
	generatedProjects, err := projectscore.NewClientWithResponses(
		projectsURL,
		projectscore.WithHTTPClient(httpClient),
		projectscore.WithRequestEditorFn(projectsEditor),
	)
	if err != nil {
		return nil, err
	}
	notificationsEditor := notificationsRequestEditor(token, options.Traceparent)
	generatedNotifications, err := notificationscore.NewClientWithResponses(
		notificationsURL,
		notificationscore.WithHTTPClient(httpClient),
		notificationscore.WithRequestEditorFn(notificationsEditor),
	)
	if err != nil {
		return nil, err
	}
	billingEditor := billingRequestEditor(token, options.Traceparent)
	generatedBilling, err := billingcore.NewClientWithResponses(
		billingURL,
		billingcore.WithHTTPClient(httpClient),
		billingcore.WithRequestEditorFn(billingEditor),
	)
	if err != nil {
		return nil, err
	}
	governanceEditor := governanceRequestEditor(token, options.Traceparent)
	generatedGovernance, err := governancecore.NewClient(
		governanceURL,
		governancecore.WithHTTPClient(httpClient),
		governancecore.WithRequestEditorFn(governanceEditor),
	)
	if err != nil {
		return nil, err
	}
	sourceEditor := sourceRequestEditor(token, options.Traceparent)
	generatedSource, err := sourcecore.NewClientWithResponses(
		sourceURL,
		sourcecore.WithHTTPClient(httpClient),
		sourcecore.WithRequestEditorFn(sourceEditor),
	)
	if err != nil {
		return nil, err
	}
	sandboxEditor := sandboxRequestEditor(token, options.Traceparent)
	generatedSandbox, err := sandboxcore.NewClientWithResponses(
		sandboxURL,
		sandboxcore.WithHTTPClient(httpClient),
		sandboxcore.WithRequestEditorFn(sandboxEditor),
	)
	if err != nil {
		return nil, err
	}
	secretsEditor := secretsRequestEditor(token, options.Traceparent)
	generatedSecrets, err := secretscore.NewClientWithResponses(
		secretsURL,
		secretscore.WithHTTPClient(httpClient),
		secretscore.WithRequestEditorFn(secretsEditor),
	)
	if err != nil {
		return nil, err
	}
	return &Client{
		IAM:           &IAMClient{client: generatedIAM},
		Projects:      &ProjectsClient{client: generatedProjects},
		Notifications: &NotificationsClient{client: generatedNotifications},
		Billing:       &BillingClient{client: generatedBilling},
		Governance:    &GovernanceClient{client: generatedGovernance},
		Sandbox:       &SandboxClient{client: generatedSandbox},
		Secrets:       &SecretsClient{client: generatedSecrets},
		Source:        &SourceClient{client: generatedSource},
	}, nil
}

func iamRequestEditor(token, traceparent string) iamcore.RequestEditorFn {
	return func(ctx context.Context, req *http.Request) error {
		return editBearerRequest(ctx, req, token, traceparent)
	}
}

func projectsRequestEditor(token, traceparent string) projectscore.RequestEditorFn {
	return func(ctx context.Context, req *http.Request) error {
		return editBearerRequest(ctx, req, token, traceparent)
	}
}

func notificationsRequestEditor(token, traceparent string) notificationscore.RequestEditorFn {
	return func(ctx context.Context, req *http.Request) error {
		return editBearerRequest(ctx, req, token, traceparent)
	}
}

func billingRequestEditor(token, traceparent string) billingcore.RequestEditorFn {
	return func(ctx context.Context, req *http.Request) error {
		return editBearerRequest(ctx, req, token, traceparent)
	}
}

func governanceRequestEditor(token, traceparent string) governancecore.RequestEditorFn {
	return func(ctx context.Context, req *http.Request) error {
		return editBearerRequest(ctx, req, token, traceparent)
	}
}

func sourceRequestEditor(token, traceparent string) sourcecore.RequestEditorFn {
	return func(ctx context.Context, req *http.Request) error {
		return editBearerRequest(ctx, req, token, traceparent)
	}
}

func sandboxRequestEditor(token, traceparent string) sandboxcore.RequestEditorFn {
	return func(ctx context.Context, req *http.Request) error {
		return editBearerRequest(ctx, req, token, traceparent)
	}
}

func secretsRequestEditor(token, traceparent string) secretscore.RequestEditorFn {
	return func(ctx context.Context, req *http.Request) error {
		return editBearerRequest(ctx, req, token, traceparent)
	}
}

func editBearerRequest(ctx context.Context, req *http.Request, token, traceparent string) error {
	if req.Header.Get("Accept") == "" {
		req.Header.Set("Accept", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))
	if strings.TrimSpace(traceparent) != "" {
		req.Header.Set("Traceparent", strings.TrimSpace(traceparent))
	}
	return nil
}

func serviceURL(serviceOverride, serverURL, service string) (string, error) {
	if strings.TrimSpace(serviceOverride) != "" {
		return normalizeURL(serviceOverride)
	}
	if strings.TrimSpace(serverURL) == "" {
		return serviceDefaultURL(service), nil
	}
	return serviceURLFromServer(serverURL, service)
}

func serviceDefaultURL(service string) string {
	switch service {
	case "iam":
		return DefaultIAMURL
	case "projects":
		return DefaultProjectsURL
	case "notifications":
		return DefaultNotificationsURL
	case "billing":
		return DefaultBillingURL
	case "governance":
		return DefaultGovernanceURL
	case "sandbox":
		return DefaultSandboxURL
	case "secrets":
		return DefaultSecretsURL
	case "source":
		return DefaultSourceURL
	default:
		return DefaultServerURL
	}
}

func serviceURLFromServer(serverURL, service string) (string, error) {
	normalized, err := normalizeURL(serverURL)
	if err != nil {
		return "", err
	}
	parsed, err := url.Parse(normalized)
	if err != nil {
		return "", err
	}
	host := parsed.Hostname()
	if host == "" {
		return "", errors.New("verself sdk: server URL host is empty")
	}
	expectedPrefix := service + ".api."
	if strings.HasPrefix(host, expectedPrefix) {
		parsed.Path = ""
		parsed.RawQuery = ""
		parsed.Fragment = ""
		return strings.TrimRight(parsed.String(), "/"), nil
	}
	if strings.Contains(host, ".api.") {
		return "", errors.New("verself sdk: server URL must be the installation apex; pass the service URL override for service API hosts")
	}
	serviceHost := expectedPrefix + host
	if port := parsed.Port(); port != "" {
		serviceHost += ":" + port
	}
	parsed.Host = serviceHost
	parsed.Path = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return strings.TrimRight(parsed.String(), "/"), nil
}

func normalizeURL(value string) (string, error) {
	trimmed := strings.TrimRight(strings.TrimSpace(value), "/")
	if trimmed == "" {
		return "", errors.New("verself sdk: service URL is empty")
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", errors.New("verself sdk: service URL must be absolute")
	}
	return trimmed, nil
}
