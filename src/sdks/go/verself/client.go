package verself

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"

	projectscore "github.com/verself/verself-go/internal/generated/projects"
)

const (
	DefaultServerURL   = "https://verself.sh"
	DefaultProjectsURL = "https://projects.api.verself.sh"
)

type Options struct {
	BearerToken string
	ServerURL   string
	ProjectsURL string
	HTTPClient  *http.Client
	Traceparent string
}

type Client struct {
	Projects *ProjectsClient
}

func New(options Options) (*Client, error) {
	token := strings.TrimSpace(options.BearerToken)
	if token == "" {
		return nil, errors.New("verself sdk: bearer token is required")
	}
	projectsURL, err := serviceURL(options.ProjectsURL, options.ServerURL, "projects")
	if err != nil {
		return nil, err
	}
	httpClient := options.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	editor := requestEditor(token, options.Traceparent)
	generatedProjects, err := projectscore.NewClientWithResponses(
		projectsURL,
		projectscore.WithHTTPClient(httpClient),
		projectscore.WithRequestEditorFn(editor),
	)
	if err != nil {
		return nil, err
	}
	return &Client{
		Projects: &ProjectsClient{client: generatedProjects},
	}, nil
}

func requestEditor(token, traceparent string) projectscore.RequestEditorFn {
	return func(ctx context.Context, req *http.Request) error {
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))
		if strings.TrimSpace(traceparent) != "" {
			req.Header.Set("Traceparent", strings.TrimSpace(traceparent))
		}
		return nil
	}
}

func serviceURL(serviceOverride, serverURL, service string) (string, error) {
	if strings.TrimSpace(serviceOverride) != "" {
		return normalizeURL(serviceOverride)
	}
	if strings.TrimSpace(serverURL) == "" {
		switch service {
		case "projects":
			return DefaultProjectsURL, nil
		default:
			return DefaultServerURL, nil
		}
	}
	return normalizeURL(serverURL)
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
