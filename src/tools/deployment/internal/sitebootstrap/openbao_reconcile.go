package sitebootstrap

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

const (
	openBaoRemoteAddr          = "127.0.0.1:8200"
	openBaoCACertPath          = "/etc/openbao/tls/cert.pem"
	openBaoTemporaryRootPath   = "/run/verself/bootstrap/openbao-root.token"
	openBaoRuntimeSecretPrefix = "secret/org/"
)

type openBaoClient struct {
	baseURL string
	token   string
	http    *http.Client
}

func waitRemoteOpenBaoReady(ctx context.Context, target inventoryTarget) error {
	deadline := time.Now().Add(90 * time.Second)
	var last error
	for {
		command := "curl -ksS -o /dev/null -w '%{http_code}' https://127.0.0.1:8200/v1/sys/health?standbyok=true"
		out, err := sshCommand(ctx, target, command).CombinedOutput()
		status := strings.TrimSpace(string(out))
		if err == nil && status == "200" {
			return nil
		}
		if err != nil {
			last = fmt.Errorf("%w: %s", err, status)
		} else {
			last = fmt.Errorf("status %s", status)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("OpenBao did not become ready: %w", last)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("OpenBao readiness: %w: %v", ctx.Err(), last)
		case <-time.After(time.Second):
		}
	}
}

func reconcileOpenBaoRuntime(ctx context.Context, target inventoryTarget, siteRootTokenFile string, secrets runtimeSecretCatalog, access runtimeAccessCatalog) (map[string]string, error) {
	rootToken, caCert, err := temporaryOpenBaoRootToken(ctx, target, siteRootTokenFile)
	if err != nil {
		return nil, err
	}
	port, err := freeLoopbackPort()
	if err != nil {
		return nil, err
	}
	tunnelCtx, cancel := context.WithCancel(ctx)
	tunnel := startTCPTunnel(tunnelCtx, target, port, openBaoRemoteAddr)
	proc, err := startChildProcess(tunnel)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("start OpenBao recovery tunnel: %w", err)
	}
	defer stopChildProcess(cancel, proc)

	client, err := newOpenBaoClient("https://127.0.0.1:"+fmt.Sprint(port), rootToken, caCert)
	if err != nil {
		return nil, err
	}
	if err := waitOpenBaoAPI(tunnelCtx, client, proc.done); err != nil {
		return nil, err
	}
	values, reconcileErr := client.reconcileRuntime(tunnelCtx, secrets, access)
	revokeErr := client.revokeSelf(tunnelCtx)
	clearBytes(rootToken)
	if reconcileErr != nil {
		return nil, reconcileErr
	}
	if revokeErr != nil {
		return nil, revokeErr
	}
	return values, nil
}

func temporaryOpenBaoRootToken(ctx context.Context, target inventoryTarget, siteRootTokenFile string) ([]byte, []byte, error) {
	if err := stageRemoteOpenBaoSiteRootToken(ctx, target, siteRootTokenFile); err != nil {
		return nil, nil, err
	}
	command := strings.Join([]string{
		"set -euo pipefail",
		"site=" + shellQuote(openBaoSiteRootTokenPath),
		"root=" + shellQuote(openBaoTemporaryRootPath),
		"trap 'sudo -n rm -f \"$root\" \"$site\"' EXIT",
		"tool=$(sudo -n find /var/lib/nomad/alloc -path '*/local/bin/openbao-bootstrap' -type f -printf '%T@ %p\\n' | sort -nr | awk 'NR==1 { $1=\"\"; sub(/^ /, \"\"); print; exit }')",
		"test -n \"$tool\"",
		"bao=\"$(dirname \"$tool\")/bao\"",
		"sudo -n \"$tool\" --action=generate-root-token --bao=\"$bao\" --state-dir=/var/lib/verself/bootstrap/openbao --site-root-token-file=\"$site\" --addr=https://127.0.0.1:8200 --ca-cert=/etc/openbao/tls/cert.pem --root-token-output-file=\"$root\" >/dev/null",
		"sudo -n cat \"$root\"",
	}, "; ")
	out, err := sshCommand(ctx, target, command).Output()
	if err != nil {
		return nil, nil, fmt.Errorf("generate temporary OpenBao root token: %w", err)
	}
	token := bytes.TrimSpace(out)
	if len(token) == 0 {
		return nil, nil, errors.New("temporary OpenBao root token was empty")
	}
	caCert, err := readRemoteRootFile(ctx, target, openBaoCACertPath)
	if err != nil {
		clearBytes(token)
		return nil, nil, err
	}
	return token, caCert, nil
}

func readRemoteRootFile(ctx context.Context, target inventoryTarget, path string) ([]byte, error) {
	out, err := sshCommand(ctx, target, "sudo -n cat "+shellQuote(path)).Output()
	if err != nil {
		return nil, fmt.Errorf("read remote file %s: %w", path, err)
	}
	if len(bytes.TrimSpace(out)) == 0 {
		return nil, fmt.Errorf("remote file %s is empty", path)
	}
	return out, nil
}

func newOpenBaoClient(baseURL string, token, caCert []byte) (*openBaoClient, error) {
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caCert) {
		return nil, errors.New("OpenBao CA certificate did not contain a PEM certificate")
	}
	return &openBaoClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   string(bytes.TrimSpace(token)),
		http: &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS13},
			},
		},
	}, nil
}

func waitOpenBaoAPI(ctx context.Context, client *openBaoClient, processDone <-chan error) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	var last error
	for {
		_, status, err := client.request(ctx, http.MethodGet, "sys/health?standbyok=true", nil)
		if err == nil && status == http.StatusOK {
			return nil
		}
		if err != nil {
			last = err
		} else {
			last = fmt.Errorf("status %d", status)
		}
		select {
		case err, ok := <-processDone:
			if ok && err != nil {
				return fmt.Errorf("OpenBao tunnel exited before readiness: %w", err)
			}
			return errors.New("OpenBao tunnel exited before readiness")
		case <-ctx.Done():
			return fmt.Errorf("OpenBao API readiness: %w: %v", ctx.Err(), last)
		case <-ticker.C:
		}
	}
}

func (c *openBaoClient) reconcileRuntime(ctx context.Context, secrets runtimeSecretCatalog, access runtimeAccessCatalog) (map[string]string, error) {
	values := map[string]string{}
	activeSecrets := activeRuntimeSecrets(secrets, access)
	missingExternal := []string{}
	for _, name := range activeSecrets {
		declaration := secrets.ByName[name]
		switch {
		case declaration.GeneratedBytes > 0:
			value, err := c.ensureGeneratedSecret(ctx, declaration)
			if err != nil {
				return nil, err
			}
			values[name] = value
		case declaration.ExternalOpenBao:
			if _, ok, err := c.getRuntimeSecret(ctx, name); err != nil {
				return nil, err
			} else if !ok {
				missingExternal = append(missingExternal, name)
			}
		case strings.TrimSpace(declaration.ProducedByJob) != "":
			continue
		default:
			return nil, fmt.Errorf("OpenBao runtime secret %s has no source", name)
		}
	}
	if len(missingExternal) > 0 {
		return nil, externalRuntimeSecretImportError(missingExternal)
	}
	for _, role := range sortedRuntimeRoles(access) {
		policy := openBaoPolicyName(role)
		if err := c.putPolicy(ctx, policy, openBaoRolePolicy(role, access)); err != nil {
			return nil, err
		}
		if err := c.putJWTRole(ctx, role, policy); err != nil {
			return nil, err
		}
	}
	return values, nil
}

func externalRuntimeSecretImportError(names []string) error {
	names = append([]string(nil), names...)
	sort.Strings(names)
	return fmt.Errorf("external OpenBao runtime secrets are not imported: %s", strings.Join(names, ", "))
}

func activeRuntimeSecrets(secrets runtimeSecretCatalog, access runtimeAccessCatalog) []string {
	seen := map[string]struct{}{}
	for name := range access.Referenced {
		if _, ok := secrets.ByName[name]; ok {
			seen[name] = struct{}{}
		}
	}
	for _, declaration := range secrets.Declarations {
		if len(access.SecretReaders[declaration.Name]) > 0 || len(access.SecretWriters[declaration.Name]) > 0 {
			seen[declaration.Name] = struct{}{}
		}
	}
	return sortedSetValues(seen)
}

func sortedRuntimeRoles(access runtimeAccessCatalog) []string {
	roles := map[string]struct{}{}
	for role, secrets := range access.RoleReads {
		if len(secrets) > 0 {
			roles[role] = struct{}{}
		}
	}
	for role, secrets := range access.RoleWrites {
		if len(secrets) > 0 {
			roles[role] = struct{}{}
		}
	}
	return sortedSetValues(roles)
}

func (c *openBaoClient) ensureGeneratedSecret(ctx context.Context, declaration runtimeSecretDeclaration) (string, error) {
	if value, ok, err := c.getRuntimeSecret(ctx, declaration.Name); err != nil {
		return "", err
	} else if ok {
		return value, nil
	}
	value, err := generateRuntimeSecretValue(declaration.GeneratedBytes, declaration.GeneratedFormat)
	if err != nil {
		return "", fmt.Errorf("%s: %w", declaration.Name, err)
	}
	if _, _, err := c.request(ctx, http.MethodPost, "kv-runtime/data/"+openBaoRuntimeSecretPrefix+url.PathEscape(declaration.Name), map[string]any{
		"data": map[string]string{"value": value},
	}); err != nil {
		return "", fmt.Errorf("write generated OpenBao runtime secret %s: %w", declaration.Name, err)
	}
	return value, nil
}

func (c *openBaoClient) getRuntimeSecret(ctx context.Context, name string) (string, bool, error) {
	body, status, err := c.request(ctx, http.MethodGet, "kv-runtime/data/"+openBaoRuntimeSecretPrefix+url.PathEscape(name), nil)
	if status == http.StatusNotFound {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("read OpenBao runtime secret %s: %w", name, err)
	}
	data, ok := body["data"].(map[string]any)
	if !ok {
		return "", false, fmt.Errorf("OpenBao runtime secret %s response missing data", name)
	}
	inner, ok := data["data"].(map[string]any)
	if !ok {
		return "", false, fmt.Errorf("OpenBao runtime secret %s response missing secret data", name)
	}
	value, ok := inner["value"].(string)
	if !ok || strings.TrimSpace(value) == "" {
		return "", false, fmt.Errorf("OpenBao runtime secret %s value is empty or non-string", name)
	}
	return value, true, nil
}

func (c *openBaoClient) putPolicy(ctx context.Context, name, policy string) error {
	if _, _, err := c.request(ctx, http.MethodPut, "sys/policies/acl/"+url.PathEscape(name), map[string]string{
		"policy": policy,
	}); err != nil {
		return fmt.Errorf("write OpenBao policy %s: %w", name, err)
	}
	return nil
}

func (c *openBaoClient) putJWTRole(ctx context.Context, role, policy string) error {
	body := map[string]any{
		"role_type":               "jwt",
		"bound_audiences":         []string{"vault.io"},
		"user_claim":              "sub",
		"token_policies":          []string{policy},
		"token_no_default_policy": true,
		"token_ttl":               "1h",
		"token_max_ttl":           "4h",
		"token_bound_cidrs":       []string{"127.0.0.1/32"},
		"token_type":              "service",
	}
	if _, _, err := c.request(ctx, http.MethodPost, "auth/jwt-nomad/role/"+url.PathEscape(role), body); err != nil {
		return fmt.Errorf("write OpenBao JWT role %s: %w", role, err)
	}
	return nil
}

func (c *openBaoClient) revokeSelf(ctx context.Context) error {
	if _, _, err := c.request(ctx, http.MethodPost, "auth/token/revoke-self", map[string]any{}); err != nil {
		return fmt.Errorf("revoke temporary OpenBao root token: %w", err)
	}
	return nil
}

func (c *openBaoClient) request(ctx context.Context, method, path string, body any) (map[string]any, int, error) {
	var input io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("encode OpenBao request: %w", err)
		}
		input = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+"/v1/"+path, input)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("X-Vault-Token", c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("openbao %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 65536))
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read openbao %s %s response: %w", method, path, err)
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if len(bytes.TrimSpace(raw)) == 0 {
			return map[string]any{}, resp.StatusCode, nil
		}
		var decoded map[string]any
		if err := json.Unmarshal(raw, &decoded); err != nil {
			return nil, resp.StatusCode, fmt.Errorf("decode openbao %s %s response: %w", method, path, err)
		}
		return decoded, resp.StatusCode, nil
	}
	return nil, resp.StatusCode, fmt.Errorf("openbao %s %s status %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(raw)))
}

func openBaoRolePolicy(role string, access runtimeAccessCatalog) string {
	var b strings.Builder
	for _, name := range sortedSetValues(access.RoleReads[role]) {
		writeOpenBaoPolicyBlock(&b, "kv-runtime/data/"+openBaoRuntimeSecretPrefix+name, []string{"read"})
	}
	for _, name := range sortedSetValues(access.RoleWrites[role]) {
		writeOpenBaoPolicyBlock(&b, "kv-runtime/data/"+openBaoRuntimeSecretPrefix+name, []string{"create", "read", "update"})
		writeOpenBaoPolicyBlock(&b, "kv-runtime/metadata/"+openBaoRuntimeSecretPrefix+name, []string{"read"})
	}
	return b.String()
}

func writeOpenBaoPolicyBlock(b *strings.Builder, path string, capabilities []string) {
	quoted := make([]string, 0, len(capabilities))
	for _, capability := range capabilities {
		quoted = append(quoted, strconvQuote(capability))
	}
	b.WriteString("path ")
	b.WriteString(strconvQuote(path))
	b.WriteString(" {\n  capabilities = [")
	b.WriteString(strings.Join(quoted, ", "))
	b.WriteString("]\n}\n\n")
}

func openBaoPolicyName(role string) string {
	return "verself-" + safeRemoteArtifactName(role)
}

func generateRuntimeSecretValue(bytesCount int, encoding string) (string, error) {
	if bytesCount <= 0 {
		return "", errors.New("generated secret byte count must be positive")
	}
	raw := make([]byte, bytesCount)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate secret bytes: %w", err)
	}
	switch strings.TrimSpace(encoding) {
	case "", "base64url":
		return base64.RawURLEncoding.EncodeToString(raw), nil
	case "hex":
		return hex.EncodeToString(raw), nil
	case "alphanumeric":
		return randomAlphanumeric(bytesCount)
	default:
		return "", fmt.Errorf("unsupported generated secret encoding %q", encoding)
	}
}

func randomAlphanumeric(length int) (string, error) {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	out := make([]byte, length)
	max := big.NewInt(int64(len(alphabet)))
	for i := range out {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", fmt.Errorf("generate alphanumeric secret: %w", err)
		}
		out[i] = alphabet[n.Int64()]
	}
	return string(out), nil
}

func clearBytes(value []byte) {
	for i := range value {
		value[i] = 0
	}
}

func strconvQuote(value string) string {
	raw, _ := json.Marshal(value)
	return string(raw)
}

func (d runtimeSecretDeclaration) activeIn(access runtimeAccessCatalog) bool {
	if len(access.SecretReaders[d.Name]) > 0 || len(access.SecretWriters[d.Name]) > 0 {
		return true
	}
	for _, principal := range append(d.readerPrincipals(), d.writerPrincipals()...) {
		if len(access.PrincipalRoles[strings.TrimSpace(principal)]) > 0 {
			return true
		}
	}
	return false
}

func sortedRuntimeSecretDeclarations(values map[string]runtimeSecretDeclaration) []runtimeSecretDeclaration {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]runtimeSecretDeclaration, 0, len(keys))
	for _, key := range keys {
		out = append(out, values[key])
	}
	return out
}
