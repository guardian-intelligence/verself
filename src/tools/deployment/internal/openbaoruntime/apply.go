package openbaoruntime

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"gopkg.in/yaml.v3"

	"github.com/verself/deployment-tools/internal/sshtun"
)

const (
	openBaoRemotePort = 8200
	openBaoRootToken  = "/etc/credstore/openbao/root-token"
	openBaoCACert     = "/etc/openbao/tls/cert.pem"
	nomadJWKSURL      = "http://127.0.0.1:4646/.well-known/jwks.json"
)

type ApplyOptions struct {
	RepoRoot      string
	Site          string
	SSH           *sshtun.Client
	Tracer        trace.Tracer
	SpiffeSubject string
	SeedVarsPath  string
}

type Result struct {
	SecretsCreated  int
	SecretsUpdated  int
	SecretsKept     int
	PoliciesWritten int
	RolesWritten    int
}

type client struct {
	addr  string
	token string
	http  *http.Client
}

type baoResponse struct {
	Data   map[string]any `json:"data"`
	Errors []string       `json:"errors"`
}

func Apply(ctx context.Context, opts ApplyOptions, plan Plan) (Result, error) {
	if opts.SSH == nil {
		return Result{}, fmt.Errorf("ssh client is required")
	}
	if opts.Tracer == nil {
		return Result{}, fmt.Errorf("tracer is required")
	}
	ctx, span := opts.Tracer.Start(ctx, "verself_deploy.openbao.runtime_reconcile",
		trace.WithAttributes(
			attribute.String("verself.site", opts.Site),
			attribute.Int("verself.openbao.runtime_secret_count", len(plan.Secrets)),
			attribute.Int("verself.openbao.nomad_role_count", len(plan.NomadRoles)),
		),
	)
	defer span.End()
	if len(plan.Secrets) == 0 {
		span.SetStatus(codes.Ok, "")
		return Result{}, nil
	}

	c, closeForward, err := dial(ctx, opts.SSH)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return Result{}, err
	}
	defer closeForward()

	seedValues, err := readSeedVars(opts.SeedVarsPath)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return Result{}, err
	}
	result, err := apply(ctx, span, c, opts, plan, seedValues)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return result, err
	}
	span.SetAttributes(
		attribute.Int("verself.openbao.secrets_created", result.SecretsCreated),
		attribute.Int("verself.openbao.secrets_updated", result.SecretsUpdated),
		attribute.Int("verself.openbao.secrets_kept", result.SecretsKept),
		attribute.Int("verself.openbao.policies_written", result.PoliciesWritten),
		attribute.Int("verself.openbao.roles_written", result.RolesWritten),
	)
	span.SetStatus(codes.Ok, "")
	return result, nil
}

func dial(ctx context.Context, sshClient *sshtun.Client) (*client, func(), error) {
	forward, err := sshClient.Forward(ctx, "openbao", openBaoRemotePort)
	if err != nil {
		return nil, func() {}, err
	}
	closeForward := func() { _ = forward.Close() }
	token, err := sudoCatTrim(ctx, sshClient, openBaoRootToken)
	if err != nil {
		closeForward()
		return nil, func() {}, err
	}
	caPEM, err := sudoCat(ctx, sshClient, openBaoCACert)
	if err != nil {
		closeForward()
		return nil, func() {}, err
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		closeForward()
		return nil, func() {}, fmt.Errorf("parse OpenBao CA bundle")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	return &client{
		addr:  "https://" + forward.ListenAddr,
		token: token,
		http:  &http.Client{Transport: transport, Timeout: 5 * time.Second},
	}, closeForward, nil
}

func apply(ctx context.Context, span trace.Span, c *client, opts ApplyOptions, plan Plan, seedValues map[string]string) (Result, error) {
	result := Result{}
	if err := c.ready(ctx); err != nil {
		return result, err
	}
	written, err := c.ensureMount(ctx, RuntimeMount)
	if err != nil {
		return result, err
	}
	if written {
		result.PoliciesWritten++
	}
	if err := c.ensureJWTAuth(ctx, NomadJWTMount, "Verself Nomad workload identity auth"); err != nil {
		return result, err
	}
	if err := c.configureJWTAuth(ctx, NomadJWTMount, map[string]any{
		"jwks_url":           nomadJWKSURL,
		"jwt_supported_algs": []string{"RS256", "EdDSA"},
	}); err != nil {
		return result, err
	}
	if opts.SpiffeSubject != "" {
		if err := c.ensureJWTAuth(ctx, SpiffeJWTMount, "Verself SPIRE JWT-SVID workload auth"); err != nil {
			return result, err
		}
	}
	wrote, err := c.ensurePolicy(ctx, "runtime-secrets-runtime-read", runtimeReadAllPolicy())
	if err != nil {
		return result, err
	}
	if wrote {
		result.PoliciesWritten++
	}
	wrote, err = c.ensurePolicy(ctx, "runtime-secrets-runtime-write", runtimeWriteAllPolicy())
	if err != nil {
		return result, err
	}
	if wrote {
		result.PoliciesWritten++
	}
	if opts.SpiffeSubject != "" {
		wrote, err = c.ensureSpiffeRole(ctx, "runtime-secrets-runtime-read", opts.SpiffeSubject, []string{"runtime-secrets-runtime-read"})
		if err != nil {
			return result, err
		}
		if wrote {
			result.RolesWritten++
		}
		wrote, err = c.ensureSpiffeRole(ctx, "runtime-secrets-runtime-write", opts.SpiffeSubject, []string{"runtime-secrets-runtime-read", "runtime-secrets-runtime-write"})
		if err != nil {
			return result, err
		}
		if wrote {
			result.RolesWritten++
		}
	}
	for _, role := range plan.NomadRoles {
		policy := nomadPolicy(role)
		wrote, err = c.ensurePolicy(ctx, role.Policy, policy)
		if err != nil {
			return result, err
		}
		if wrote {
			result.PoliciesWritten++
		}
		wrote, err = c.ensureNomadRole(ctx, role)
		if err != nil {
			return result, err
		}
		if wrote {
			result.RolesWritten++
		}
		span.AddEvent("verself.openbao.nomad_role", trace.WithAttributes(
			attribute.String("verself.component", role.Component),
			attribute.String("nomad.job_id", role.JobID),
			attribute.String("openbao.auth_mount", NomadJWTMount),
			attribute.String("openbao.role", role.Name),
			attribute.String("openbao.policy", role.Policy),
			attribute.Int("openbao.secret_count", len(role.Secrets)),
		))
	}
	for _, secret := range plan.Secrets {
		status, fingerprint, err := reconcileSecret(ctx, c, opts.SSH, secret, seedValues)
		if err != nil {
			return result, err
		}
		switch status {
		case "created":
			result.SecretsCreated++
		case "updated":
			result.SecretsUpdated++
		case "kept":
			result.SecretsKept++
		}
		span.AddEvent("verself.openbao.runtime_secret", trace.WithAttributes(
			attribute.String("verself.component", secret.Component),
			attribute.String("nomad.job_id", secret.JobID),
			attribute.String("openbao.mount", RuntimeMount),
			attribute.String("openbao.secret_name", secret.Name),
			attribute.String("openbao.secret_sha256", fingerprint),
			attribute.String("openbao.action", status),
		))
	}
	return result, nil
}

func reconcileSecret(ctx context.Context, c *client, sshClient *sshtun.Client, secret Secret, seedValues map[string]string) (string, string, error) {
	existing, found, err := c.readRuntimeSecret(ctx, secret.Name)
	if err != nil {
		return "", "", err
	}
	if secret.Generated.Bytes != 0 {
		if found {
			return "kept", fingerprint(existing), nil
		}
		value, err := generateSecretValue(secret.Generated.Bytes)
		if err != nil {
			return "", "", fmt.Errorf("%s: generate value: %w", secret.Name, err)
		}
		if err := c.writeRuntimeSecret(ctx, secret.Name, value); err != nil {
			return "", "", err
		}
		return "created", fingerprint(value), nil
	}
	source, sourceFound, err := sourceValue(ctx, sshClient, secret, seedValues)
	if err != nil {
		if found {
			return "kept", fingerprint(existing), nil
		}
		return "", "", err
	}
	if !sourceFound {
		if found {
			return "kept", fingerprint(existing), nil
		}
		return "", "", fmt.Errorf("%s: no seed value available", secret.Name)
	}
	if found && existing == source {
		return "kept", fingerprint(existing), nil
	}
	if err := c.writeRuntimeSecret(ctx, secret.Name, source); err != nil {
		return "", "", err
	}
	if found {
		return "updated", fingerprint(source), nil
	}
	return "created", fingerprint(source), nil
}

func generateSecretValue(bytes int) (string, error) {
	raw := make([]byte, bytes)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func sourceValue(ctx context.Context, sshClient *sshtun.Client, secret Secret, seedValues map[string]string) (string, bool, error) {
	if secret.SiteSecret != "" {
		value := strings.TrimSpace(seedValues[secret.SiteSecret])
		return value, value != "", nil
	}
	if secret.File != "" {
		value, err := sudoCatTrim(ctx, sshClient, secret.File)
		if err != nil {
			return "", false, fmt.Errorf("%s: read source file %s: %w", secret.Name, secret.File, err)
		}
		return value, value != "", nil
	}
	return "", false, nil
}

func readSeedVars(path string) (map[string]string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return map[string]string{}, nil
	}
	body, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, fmt.Errorf("read seed vars %s: %w", path, err)
	}
	values := map[string]string{}
	switch filepath.Ext(path) {
	case ".json":
		if err := json.Unmarshal(body, &values); err != nil {
			return nil, fmt.Errorf("decode seed vars %s: %w", path, err)
		}
	default:
		if err := yaml.Unmarshal(body, &values); err != nil {
			return nil, fmt.Errorf("decode seed vars %s: %w", path, err)
		}
	}
	return values, nil
}

func (c *client) ready(ctx context.Context) error {
	var response baoResponse
	status, err := c.do(ctx, http.MethodGet, "v1/sys/health", nil, &response, http.StatusOK, http.StatusTooManyRequests, http.StatusServiceUnavailable)
	if err != nil {
		return err
	}
	if status != http.StatusOK && status != http.StatusTooManyRequests {
		return fmt.Errorf("openbao is not active: status %d", status)
	}
	return nil
}

func (c *client) ensureMount(ctx context.Context, mount string) (bool, error) {
	var response baoResponse
	if _, err := c.do(ctx, http.MethodGet, "v1/sys/mounts", nil, &response, http.StatusOK); err != nil {
		return false, err
	}
	if _, ok := response.Data[mount+"/"]; ok {
		return false, nil
	}
	body := map[string]any{
		"type":        "kv",
		"description": "Verself runtime secrets",
		"options": map[string]string{
			"version": "2",
		},
	}
	if _, err := c.do(ctx, http.MethodPost, "v1/sys/mounts/"+url.PathEscape(mount), body, nil, http.StatusNoContent); err != nil {
		return false, err
	}
	return true, nil
}

func (c *client) ensureJWTAuth(ctx context.Context, mount, description string) error {
	var response baoResponse
	if _, err := c.do(ctx, http.MethodGet, "v1/sys/auth", nil, &response, http.StatusOK); err != nil {
		return err
	}
	if _, ok := response.Data[mount+"/"]; ok {
		return nil
	}
	body := map[string]any{"type": "jwt", "description": description}
	_, err := c.do(ctx, http.MethodPost, "v1/sys/auth/"+url.PathEscape(mount), body, nil, http.StatusNoContent)
	return err
}

func (c *client) configureJWTAuth(ctx context.Context, mount string, body map[string]any) error {
	_, err := c.do(ctx, http.MethodPost, "v1/auth/"+url.PathEscape(mount)+"/config", body, nil, http.StatusNoContent, http.StatusOK)
	return err
}

func (c *client) ensurePolicy(ctx context.Context, name, policy string) (bool, error) {
	var response baoResponse
	status, err := c.do(ctx, http.MethodGet, "v1/sys/policies/acl/"+url.PathEscape(name), nil, &response, http.StatusOK, http.StatusNotFound)
	if err != nil {
		return false, err
	}
	if status == http.StatusOK && strings.TrimSpace(stringFromMap(response.Data, "policy")) == strings.TrimSpace(policy) {
		return false, nil
	}
	body := map[string]any{"policy": policy}
	if _, err := c.do(ctx, http.MethodPut, "v1/sys/policies/acl/"+url.PathEscape(name), body, nil, http.StatusOK, http.StatusNoContent); err != nil {
		return false, err
	}
	return true, nil
}

func (c *client) ensureSpiffeRole(ctx context.Context, name, subject string, policies []string) (bool, error) {
	body := map[string]any{
		"role_type":       "jwt",
		"user_claim":      "sub",
		"bound_subject":   subject,
		"bound_audiences": []string{SpiffeAudience},
		"token_policies":  policies,
		"token_ttl":       "15m",
		"token_max_ttl":   "1h",
	}
	return c.ensureRole(ctx, SpiffeJWTMount, name, body)
}

func (c *client) ensureNomadRole(ctx context.Context, role NomadRole) (bool, error) {
	body := map[string]any{
		"role_type":               "jwt",
		"user_claim":              "/nomad_job_id",
		"user_claim_json_pointer": true,
		"bound_audiences":         []string{NomadAudience},
		"bound_claims": map[string]string{
			"nomad_namespace": "default",
			"nomad_job_id":    role.JobID,
		},
		"claim_mappings": map[string]string{
			"nomad_namespace": "nomad_namespace",
			"nomad_job_id":    "nomad_job_id",
			"nomad_task":      "nomad_task",
		},
		"token_type":             "service",
		"token_policies":         []string{role.Policy},
		"token_period":           "30m",
		"token_explicit_max_ttl": 0,
	}
	return c.ensureRole(ctx, NomadJWTMount, role.Name, body)
}

func (c *client) ensureRole(ctx context.Context, mount, name string, body map[string]any) (bool, error) {
	var response baoResponse
	status, err := c.do(ctx, http.MethodGet, "v1/auth/"+url.PathEscape(mount)+"/role/"+url.PathEscape(name), nil, &response, http.StatusOK, http.StatusNotFound)
	if err != nil {
		return false, err
	}
	if status == http.StatusOK {
		existing, err := json.Marshal(canonicalizeRole(response.Data))
		if err != nil {
			return false, err
		}
		desiredCanonical, err := json.Marshal(canonicalizeRole(body))
		if err != nil {
			return false, err
		}
		if bytes.Equal(existing, desiredCanonical) {
			return false, nil
		}
	}
	if _, err := c.do(ctx, http.MethodPost, "v1/auth/"+url.PathEscape(mount)+"/role/"+url.PathEscape(name), body, nil, http.StatusOK, http.StatusNoContent); err != nil {
		return false, err
	}
	return true, nil
}

func canonicalizeRole(input map[string]any) map[string]any {
	out := map[string]any{}
	for _, key := range []string{
		"role_type",
		"user_claim",
		"user_claim_json_pointer",
		"bound_subject",
		"bound_audiences",
		"bound_claims",
		"claim_mappings",
		"token_policies",
		"token_ttl",
		"token_max_ttl",
		"token_type",
		"token_period",
		"token_explicit_max_ttl",
	} {
		if value, ok := input[key]; ok {
			out[key] = normalizeJSON(value)
		}
	}
	return out
}

func normalizeJSON(value any) any {
	switch v := value.(type) {
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			out = append(out, fmt.Sprint(item))
		}
		sort.Strings(out)
		return out
	case []string:
		out := append([]string(nil), v...)
		sort.Strings(out)
		return out
	case map[string]any:
		out := map[string]string{}
		for key, item := range v {
			out[key] = fmt.Sprint(item)
		}
		return out
	case map[string]string:
		return v
	default:
		return value
	}
}

func (c *client) readRuntimeSecret(ctx context.Context, name string) (string, bool, error) {
	var response baoResponse
	status, err := c.do(ctx, http.MethodGet, runtimeSecretAPIPath(name), nil, &response, http.StatusOK, http.StatusNotFound)
	if err != nil {
		return "", false, err
	}
	if status == http.StatusNotFound {
		return "", false, nil
	}
	data, _ := response.Data["data"].(map[string]any)
	return strings.TrimSpace(fmt.Sprint(data["value"])), true, nil
}

func (c *client) writeRuntimeSecret(ctx context.Context, name, value string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	body := map[string]any{"data": map[string]any{
		"org_id":      RuntimeNamespace,
		"kind":        "runtime_secret",
		"name":        name,
		"scope_level": "org",
		"source_id":   "",
		"env_id":      "",
		"branch":      "",
		"value":       value,
		"updated_at":  now,
		"created_at":  now,
	}}
	_, err := c.do(ctx, http.MethodPost, runtimeSecretAPIPath(name), body, nil, http.StatusOK, http.StatusNoContent)
	return err
}

func runtimeSecretAPIPath(name string) string {
	return "v1/" + RuntimeMount + "/data/secret/org/" + url.PathEscape(name)
}

func runtimeReadAllPolicy() string {
	return fmt.Sprintf(`path "%s/data/secret/org/*" { capabilities = ["read"] }
path "%s/metadata/secret/org/*" { capabilities = ["read", "list"] }
`, RuntimeMount, RuntimeMount)
}

func runtimeWriteAllPolicy() string {
	return fmt.Sprintf(`path "%s/data/secret/org/*" { capabilities = ["create", "update", "read", "delete"] }
path "%s/metadata/secret/org/*" { capabilities = ["read", "list", "delete"] }
`, RuntimeMount, RuntimeMount)
}

func nomadPolicy(role NomadRole) string {
	var b strings.Builder
	for _, secret := range role.Secrets {
		fmt.Fprintf(&b, "path %q { capabilities = [\"read\"] }\n", RuntimeMount+"/data/secret/org/"+secret)
	}
	return b.String()
}

func (c *client) do(ctx context.Context, method, path string, body any, out any, expected ...int) (int, error) {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return 0, fmt.Errorf("encode openbao request %s %s: %w", method, path, err)
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.addr, "/")+"/"+path, reader)
	if err != nil {
		return 0, err
	}
	req.Header.Set("X-Vault-Token", c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("openbao %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	for _, code := range expected {
		if resp.StatusCode == code {
			if out != nil && len(raw) > 0 {
				if err := json.Unmarshal(raw, out); err != nil {
					return resp.StatusCode, fmt.Errorf("decode openbao %s %s: %w", method, path, err)
				}
			}
			return resp.StatusCode, nil
		}
	}
	return resp.StatusCode, fmt.Errorf("openbao %s %s status %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(raw)))
}

func sudoCat(ctx context.Context, sshClient *sshtun.Client, path string) ([]byte, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("path is required")
	}
	return sshClient.Exec(ctx, "sudo /bin/cat -- "+strconv.Quote(path))
}

func sudoCatTrim(ctx context.Context, sshClient *sshtun.Client, path string) (string, error) {
	body, err := sudoCat(ctx, sshClient, path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(body)), nil
}

func stringFromMap(values map[string]any, key string) string {
	value, ok := values[key]
	if !ok || value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

func fingerprint(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
