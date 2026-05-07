package verself

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/spiffe/go-spiffe/v2/workloadapi"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"

	projectsclient "github.com/verself/projects-service/client"
	projectsinternalclient "github.com/verself/projects-service/internalclient"
	workloadauth "github.com/verself/service-runtime/workload"
)

const (
	DefaultServerURL   = "https://verself.sh"
	DefaultProjectsURL = "https://projects.api.verself.sh"
)

type Options struct {
	BearerToken         string
	ServerURL           string
	ProjectsURL         string
	ProjectsInternalURL string
	HTTPClient          *http.Client
	WorkloadIdentity    *WorkloadIdentityOptions
	Traceparent         string
}

type WorkloadIdentityOptions struct {
	Source        *workloadapi.X509Source
	CallerService string
	Origin        Origin
}

type Origin struct {
	OrgID   uint64
	Subject string
	Email   string
}

type Client struct {
	Projects *ProjectsClient
}

func New(options Options) (*Client, error) {
	token := strings.TrimSpace(options.BearerToken)
	if options.WorkloadIdentity != nil {
		if token != "" {
			return nil, errors.New("verself sdk: bearer token and workload identity are mutually exclusive")
		}
		return newWorkloadClient(options)
	}
	if token == "" {
		return nil, errors.New("verself sdk: bearer token or workload identity is required")
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
	generatedProjects, err := projectsclient.NewClientWithResponses(
		projectsURL,
		projectsclient.WithHTTPClient(httpClient),
		projectsclient.WithRequestEditorFn(editor),
	)
	if err != nil {
		return nil, err
	}
	return &Client{
		Projects: &ProjectsClient{client: generatedProjects},
	}, nil
}

func newWorkloadClient(options Options) (*Client, error) {
	workload := options.WorkloadIdentity
	if workload.Source == nil {
		return nil, errors.New("verself sdk: workload identity source is required")
	}
	if strings.TrimSpace(workload.CallerService) == "" {
		return nil, errors.New("verself sdk: workload caller service is required")
	}
	if _, err := workloadauth.CurrentIDForService(workload.Source, workload.CallerService); err != nil {
		return nil, err
	}
	if err := validateOrigin(workload.Origin); err != nil {
		return nil, err
	}
	if options.HTTPClient != nil {
		return nil, errors.New("verself sdk: workload identity owns the mTLS HTTP client")
	}
	if strings.TrimSpace(options.ProjectsInternalURL) == "" {
		return nil, errors.New("verself sdk: projects internal URL is required for workload identity")
	}
	projectsURL, err := normalizeURL(options.ProjectsInternalURL)
	if err != nil {
		return nil, err
	}
	httpClient, err := workloadauth.MTLSClientForService(workload.Source, workloadauth.ServiceProjects, nil)
	if err != nil {
		return nil, err
	}
	generatedProjects, err := projectsinternalclient.NewClientWithResponses(
		projectsURL,
		projectsinternalclient.WithHTTPClient(httpClient),
		projectsinternalclient.WithRequestEditorFn(internalRequestEditor(options.Traceparent)),
	)
	if err != nil {
		return nil, err
	}
	return &Client{
		Projects: &ProjectsClient{internal: generatedProjects, origin: workload.Origin},
	}, nil
}

func requestEditor(token, traceparent string) projectsclient.RequestEditorFn {
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

func internalRequestEditor(traceparent string) projectsinternalclient.RequestEditorFn {
	return func(ctx context.Context, req *http.Request) error {
		req.Header.Set("Accept", "application/json")
		otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))
		if strings.TrimSpace(traceparent) != "" {
			req.Header.Set("Traceparent", strings.TrimSpace(traceparent))
		}
		return nil
	}
}

func validateOrigin(origin Origin) error {
	if origin.OrgID == 0 {
		return errors.New("verself sdk: origin org id is required for workload identity")
	}
	if strings.TrimSpace(origin.Subject) == "" {
		return errors.New("verself sdk: origin subject is required for workload identity")
	}
	return nil
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
