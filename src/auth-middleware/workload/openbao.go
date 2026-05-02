package workload

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"github.com/spiffe/go-spiffe/v2/workloadapi"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const (
	openBaoDefaultTimeout = 3 * time.Second
	openBaoMaxAttempts    = 5
)

type OpenBaoClientConfig struct {
	Address  string
	CACert   string
	AuthPath string
	Role     string
	Audience string
	Subject  spiffeid.ID
	Mount    string
	Timeout  time.Duration
}

type OpenBaoClient struct {
	address  string
	authPath string
	role     string
	audience string
	subject  spiffeid.ID
	mount    string
	source   *workloadapi.JWTSource
	client   *http.Client

	mu        sync.Mutex
	token     string
	expiresAt time.Time
}

type openBaoLoginResponse struct {
	RequestID string `json:"request_id"`
	Auth      struct {
		ClientToken   string `json:"client_token"`
		Accessor      string `json:"accessor"`
		LeaseDuration int64  `json:"lease_duration"`
	} `json:"auth"`
}

type openBaoKVResponse struct {
	RequestID string `json:"request_id"`
	Data      struct {
		Data map[string]any `json:"data"`
	} `json:"data"`
}

func NewOpenBaoClient(source *workloadapi.JWTSource, cfg OpenBaoClientConfig) (*OpenBaoClient, error) {
	if source == nil {
		return nil, errors.New("spiffe jwt source is required")
	}
	address := strings.TrimRight(strings.TrimSpace(cfg.Address), "/")
	if address == "" {
		return nil, errors.New("openbao address is required")
	}
	parsed, err := url.Parse(address)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return nil, fmt.Errorf("openbao address must be an https URL: %q", address)
	}
	authPath := trimPath(firstNonEmpty(cfg.AuthPath, "spiffe-jwt"))
	role := strings.TrimSpace(cfg.Role)
	if role == "" {
		return nil, errors.New("openbao role is required")
	}
	audience := strings.TrimSpace(cfg.Audience)
	if audience == "" {
		return nil, errors.New("openbao workload audience is required")
	}
	mount := trimPath(firstNonEmpty(cfg.Mount, "platform"))
	if mount == "" {
		return nil, errors.New("openbao mount is required")
	}
	transport, err := openBaoTransport(strings.TrimSpace(cfg.CACert))
	if err != nil {
		return nil, err
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = openBaoDefaultTimeout
	}
	return &OpenBaoClient{
		address:  address,
		authPath: authPath,
		role:     role,
		audience: audience,
		subject:  cfg.Subject,
		mount:    mount,
		source:   source,
		client:   &http.Client{Transport: transport, Timeout: timeout},
	}, nil
}

func (c *OpenBaoClient) ReadKVV2(ctx context.Context, path string) (map[string]string, error) {
	path = trimPath(path)
	if path == "" {
		return nil, errors.New("openbao kv path is required")
	}
	ctx, span := tracer.Start(ctx, "workload.openbao.kv.get", trace.WithSpanKind(trace.SpanKindClient))
	defer span.End()
	span.SetAttributes(
		attribute.String("bao.mount", c.mount),
		attribute.String("bao.path_hash", hashPath(path)),
		attribute.String("bao.role", c.role),
	)
	token, err := c.clientToken(ctx)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint(c.mount, "data", path), nil)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	req.Header.Set("X-Vault-Token", token)
	var (
		body []byte
		resp *http.Response
	)
	for attempt := 1; attempt <= openBaoMaxAttempts; attempt++ {
		resp, err = c.client.Do(req.Clone(ctx))
		if err != nil {
			if attempt < openBaoMaxAttempts && sleepWithBackoff(ctx, attempt) == nil {
				continue
			}
			err = fmt.Errorf("openbao kv get %s: %w", hashPath(path), err)
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return nil, err
		}
		body, _ = io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			break
		}
		if retryableOpenBaoStatus(resp.StatusCode) && attempt < openBaoMaxAttempts && sleepWithBackoff(ctx, attempt) == nil {
			continue
		}
		err = fmt.Errorf("openbao kv get %s status %d", hashPath(path), resp.StatusCode)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	var payload openBaoKVResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		err := fmt.Errorf("decode openbao kv get %s: %w", hashPath(path), err)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	span.SetAttributes(
		attribute.String("bao.request_id", firstNonEmpty(
			resp.Header.Get("X-Vault-Request-Id"),
			resp.Header.Get("X-OpenBao-Request-Id"),
			payload.RequestID,
		)),
		attribute.Int("bao.http_status", resp.StatusCode),
	)
	values := make(map[string]string, len(payload.Data.Data))
	for key, value := range payload.Data.Data {
		text, ok := value.(string)
		if !ok {
			err := fmt.Errorf("openbao kv get %s returned non-string field %q", hashPath(path), key)
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return nil, err
		}
		values[key] = text
	}
	return values, nil
}

func (c *OpenBaoClient) clientToken(ctx context.Context) (string, error) {
	now := time.Now()
	c.mu.Lock()
	if c.token != "" && c.expiresAt.After(now.Add(30*time.Second)) {
		token := c.token
		c.mu.Unlock()
		return token, nil
	}
	c.mu.Unlock()

	ctx, span := tracer.Start(ctx, "workload.openbao.jwt_svid.login", trace.WithSpanKind(trace.SpanKindClient))
	defer span.End()
	span.SetAttributes(
		attribute.String("jwt.audience", c.audience),
		attribute.String("bao.auth_method", "jwt_svid"),
		attribute.String("bao.role", c.role),
	)
	svid, _, subject, err := FetchJWTSVID(ctx, c.source, c.audience, c.subject)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return "", err
	}
	body, err := json.Marshal(map[string]string{"role": c.role, "jwt": svid})
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return "", err
	}
	var (
		resp     *http.Response
		respBody []byte
	)
	for attempt := 1; attempt <= openBaoMaxAttempts; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint("auth", c.authPath, "login"), bytes.NewReader(body))
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return "", err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err = c.client.Do(req)
		if err != nil {
			if attempt < openBaoMaxAttempts && sleepWithBackoff(ctx, attempt) == nil {
				continue
			}
			err = fmt.Errorf("openbao jwt-svid login role %s: %w", c.role, err)
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return "", err
		}
		respBody, _ = io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			break
		}
		if retryableOpenBaoStatus(resp.StatusCode) && attempt < openBaoMaxAttempts && sleepWithBackoff(ctx, attempt) == nil {
			continue
		}
		err = fmt.Errorf("openbao jwt-svid login role %s status %d", c.role, resp.StatusCode)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return "", err
	}
	var payload openBaoLoginResponse
	if err := json.Unmarshal(respBody, &payload); err != nil {
		err := fmt.Errorf("decode openbao jwt-svid login role %s: %w", c.role, err)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return "", err
	}
	span.SetAttributes(
		attribute.String("spiffe.id", subject.String()),
		attribute.String("bao.request_id", firstNonEmpty(
			resp.Header.Get("X-Vault-Request-Id"),
			resp.Header.Get("X-OpenBao-Request-Id"),
			payload.RequestID,
		)),
		attribute.Int("bao.http_status", resp.StatusCode),
	)
	if strings.TrimSpace(payload.Auth.ClientToken) == "" {
		err := fmt.Errorf("openbao jwt-svid login role %s omitted client_token", c.role)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return "", err
	}
	lease := time.Duration(payload.Auth.LeaseDuration) * time.Second
	if lease <= 0 {
		lease = 5 * time.Minute
	}
	c.mu.Lock()
	c.token = payload.Auth.ClientToken
	c.expiresAt = time.Now().Add(lease / 2)
	token := c.token
	c.mu.Unlock()
	return token, nil
}

func (c *OpenBaoClient) endpoint(parts ...string) string {
	cleaned := make([]string, 0, len(parts)+2)
	cleaned = append(cleaned, c.address, "v1")
	for _, part := range parts {
		if value := trimPath(part); value != "" {
			cleaned = append(cleaned, value)
		}
	}
	return strings.Join(cleaned, "/")
}

func openBaoTransport(caPath string) (*http.Transport, error) {
	pool, err := x509.SystemCertPool()
	if err != nil {
		return nil, fmt.Errorf("load system cert pool: %w", err)
	}
	if pool == nil {
		pool = x509.NewCertPool()
	}
	if caPath != "" {
		pem, err := os.ReadFile(caPath)
		if err != nil {
			return nil, fmt.Errorf("read openbao CA cert: %w", err)
		}
		if !pool.AppendCertsFromPEM(pem) {
			return nil, errors.New("openbao CA cert did not contain a PEM certificate")
		}
	}
	base := http.DefaultTransport.(*http.Transport).Clone()
	base.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	return base, nil
}

func trimPath(value string) string {
	return strings.Trim(strings.TrimSpace(value), "/")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if text := strings.TrimSpace(value); text != "" {
			return text
		}
	}
	return ""
}

func hashPath(path string) string {
	sum := sha256.Sum256([]byte(path))
	return hex.EncodeToString(sum[:])
}

func retryableOpenBaoStatus(status int) bool {
	return status == http.StatusTooManyRequests || status >= http.StatusInternalServerError
}

func sleepWithBackoff(ctx context.Context, attempt int) error {
	delay := 200 * time.Millisecond
	for i := 1; i < attempt; i++ {
		delay *= 2
		if delay >= time.Second {
			delay = time.Second
			break
		}
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
