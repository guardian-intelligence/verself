package secretsclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const platformRuntimeResolvePath = "/internal/v1/platform-secrets/resolve"

const (
	BillingStripeSecretKeyName       = "billing-service.stripe.secret_key"
	BillingStripeWebhookSecretName   = "billing-service.stripe.webhook_secret"
	SandboxGitHubPrivateKeyName      = "sandbox-rental-service.github.private_key"
	SandboxGitHubWebhookSecretName   = "sandbox-rental-service.github.webhook_secret"
	SandboxGitHubClientSecretName    = "sandbox-rental-service.github.client_secret"
	MailboxResendAPIKeyName          = "mailbox-service.resend.api_key"
	MailboxStalwartAdminPasswordName = "mailbox-service.stalwart.admin_password"
)

var (
	ErrForbidden  = errors.New("secrets-client: forbidden")
	ErrUnexpected = errors.New("secrets-client: unexpected response")

	tracer = otel.Tracer("secrets-service/client")
)

type RuntimeSecretClient struct {
	baseURL    *url.URL
	httpClient *http.Client
}

type ClientOption func(*RuntimeSecretClient) error

type HTTPError struct {
	Op         string
	StatusCode int
	Body       []byte
	Cause      error
}

func (e *HTTPError) Error() string {
	if e == nil {
		return "secrets-client: nil error"
	}
	if e.Cause != nil {
		return e.Op + ": " + e.Cause.Error()
	}
	return fmt.Sprintf("%s: http %d", e.Op, e.StatusCode)
}

func (e *HTTPError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func New(baseURL string, opts ...ClientOption) (*RuntimeSecretClient, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return nil, fmt.Errorf("secrets-client: parse base url: %w", err)
	}
	client := &RuntimeSecretClient{
		baseURL:    parsed,
		httpClient: http.DefaultClient,
	}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(client); err != nil {
			return nil, err
		}
	}
	if client.baseURL == nil {
		return nil, fmt.Errorf("secrets-client: base url is required")
	}
	if client.httpClient == nil {
		client.httpClient = http.DefaultClient
	}
	return client, nil
}

func WithHTTPClient(httpClient *http.Client) ClientOption {
	return func(c *RuntimeSecretClient) error {
		c.httpClient = httpClient
		return nil
	}
}

func (c *RuntimeSecretClient) ResolvePlatformRuntimeSecrets(ctx context.Context, secretNames []string) (map[string]string, error) {
	ctx, span := tracer.Start(ctx, "secrets.runtime.resolve", trace.WithSpanKind(trace.SpanKindClient))
	defer span.End()

	secretNames, err := normalizeSecretNames(secretNames)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	if len(secretNames) == 0 {
		return map[string]string{}, nil
	}
	span.SetAttributes(attribute.Int("forge_metal.secret_count", len(secretNames)))

	payload, err := json.Marshal(runtimeSecretResolveRequest{SecretNames: secretNames})
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("secrets-client: encode runtime secret request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.resolveURL(platformRuntimeResolvePath), bytes.NewReader(payload))
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("secrets-client: build runtime secret request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("secrets-client: resolve runtime secrets: %w", err)
	}
	defer resp.Body.Close()

	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if readErr != nil {
		span.RecordError(readErr)
		span.SetStatus(codes.Error, readErr.Error())
		return nil, fmt.Errorf("secrets-client: read runtime secret response: %w", readErr)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		err := &HTTPError{
			Op:         "secrets-client: resolve runtime secrets",
			StatusCode: resp.StatusCode,
			Body:       body,
			Cause:      classifyHTTPStatus(resp.StatusCode),
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	var decoded runtimeSecretResolveResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("secrets-client: decode runtime secret response: %w", err)
	}
	out := make(map[string]string, len(decoded.Secrets))
	for _, item := range decoded.Secrets {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			err := fmt.Errorf("secrets-client: runtime secret response omitted name")
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return nil, err
		}
		out[name] = item.Value
	}
	span.SetAttributes(attribute.Int("forge_metal.secret_count_resolved", len(out)))
	return out, nil
}

func (c *RuntimeSecretClient) resolveURL(path string) string {
	if c == nil || c.baseURL == nil {
		return path
	}
	resolved := *c.baseURL
	resolved.Path = strings.TrimRight(resolved.Path, "/") + path
	resolved.RawPath = ""
	return resolved.String()
}

func normalizeSecretNames(secretNames []string) ([]string, error) {
	if len(secretNames) == 0 {
		return nil, nil
	}
	if len(secretNames) > 32 {
		return nil, fmt.Errorf("secrets-client: at most 32 runtime secrets are allowed")
	}
	out := make([]string, 0, len(secretNames))
	seen := make(map[string]struct{}, len(secretNames))
	for _, name := range secretNames {
		name = strings.TrimSpace(name)
		if name == "" {
			return nil, fmt.Errorf("secrets-client: runtime secret name is required")
		}
		if _, ok := seen[name]; ok {
			return nil, fmt.Errorf("secrets-client: duplicate runtime secret %q", name)
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out, nil
}

func classifyHTTPStatus(statusCode int) error {
	switch statusCode {
	case http.StatusForbidden, http.StatusUnauthorized:
		return ErrForbidden
	default:
		return ErrUnexpected
	}
}

type runtimeSecretResolveRequest struct {
	SecretNames []string `json:"secret_names"`
}

type runtimeSecretResolveResponse struct {
	Secrets []runtimeSecretValue `json:"secrets"`
}

type runtimeSecretValue struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}
