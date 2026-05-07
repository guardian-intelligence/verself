package verself

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"

	iamcore "github.com/verself/verself-go/internal/generated/iam"
	projectscore "github.com/verself/verself-go/internal/generated/projects"
)

const (
	DefaultServerURL   = "https://verself.sh"
	DefaultIAMURL      = "https://iam.api.verself.sh"
	DefaultProjectsURL = "https://projects.api.verself.sh"
)

type Options struct {
	BearerToken string
	ServerURL   string
	IAMURL      string
	ProjectsURL string
	HTTPClient  *http.Client
	Traceparent string
}

type Client struct {
	IAM      *IAMClient
	Projects *ProjectsClient
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
	httpClient := options.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	iamEditor := iamRequestEditor(token, options.Traceparent)
	generatedIAM, err := iamcore.NewClientWithResponses(
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
	return &Client{
		IAM:      &IAMClient{client: generatedIAM},
		Projects: &ProjectsClient{client: generatedProjects},
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

func editBearerRequest(ctx context.Context, req *http.Request, token, traceparent string) error {
	req.Header.Set("Accept", "application/json")
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
