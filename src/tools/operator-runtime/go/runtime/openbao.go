package runtime

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const (
	SiteConfigKVMount        = "site-config"
	SiteConfigSecretPrefix   = "config"
	openBaoRootTokenPath     = "/etc/credstore/openbao/root-token"
	openBaoCACertPath        = "/etc/openbao/tls/cert.pem"
	openBaoRemoteAPIAddress  = "127.0.0.1:8200"
	openBaoHTTPResponseLimit = 1 << 20
)

type SiteConfigClient struct {
	baseURL string
	token   string
	http    *http.Client
}

func OpenSiteConfig(ctx context.Context, rt *Runtime) (*SiteConfigClient, error) {
	if rt == nil || rt.SSH == nil {
		return nil, errors.New("openbao site config: operator runtime with SSH is required")
	}
	tokenRaw, err := rt.SSH.Exec(ctx, "sudo /bin/cat -- "+mustShellWord(openBaoRootTokenPath))
	if err != nil {
		return nil, fmt.Errorf("openbao site config: read root token: %w", err)
	}
	token := strings.TrimSpace(string(tokenRaw))
	if token == "" {
		return nil, errors.New("openbao site config: root token is empty")
	}
	caPEM, err := ReadRemoteFile(ctx, rt.SSH, openBaoCACertPath)
	if err != nil {
		return nil, fmt.Errorf("openbao site config: read CA certificate: %w", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		return nil, errors.New("openbao site config: CA certificate is not PEM")
	}
	forward, err := rt.SSH.Forward(ctx, "openbao-api", openBaoRemoteAPIAddress)
	if err != nil {
		return nil, fmt.Errorf("openbao site config: open API forward: %w", err)
	}
	return &SiteConfigClient{
		baseURL: "https://" + forward.ListenAddr,
		token:   token,
		http: &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS13,
			RootCAs:    roots,
		}}},
	}, nil
}

func ReadSiteConfigValue(ctx context.Context, rt *Runtime, name string) (string, error) {
	client, err := OpenSiteConfig(ctx, rt)
	if err != nil {
		return "", err
	}
	return client.ReadValue(ctx, name)
}

func (c *SiteConfigClient) EnsureMount(ctx context.Context) error {
	var mounts struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, "/v1/sys/mounts", nil, http.StatusOK, &mounts); err != nil {
		return err
	}
	if _, ok := mounts.Data[SiteConfigKVMount+"/"]; ok {
		return nil
	}
	body := map[string]any{
		"type":        "kv",
		"description": "Verself site configuration",
		"options": map[string]string{
			"version": "2",
		},
	}
	return c.do(ctx, http.MethodPost, "/v1/sys/mounts/"+url.PathEscape(SiteConfigKVMount), body, http.StatusNoContent, nil)
}

func (c *SiteConfigClient) WriteValue(ctx context.Context, name, value string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("openbao site config: secret name is required")
	}
	if value == "" {
		return errors.New("openbao site config: secret value is empty")
	}
	body := map[string]any{"data": map[string]any{"value": value}}
	return c.do(ctx, http.MethodPost, "/v1/"+url.PathEscape(SiteConfigKVMount)+"/data/"+escapeOpenBaoPath(SiteConfigSecretPrefix+"/"+name), body, http.StatusOK, nil)
}

func (c *SiteConfigClient) ReadValue(ctx context.Context, name string) (string, error) {
	if strings.TrimSpace(name) == "" {
		return "", errors.New("openbao site config: secret name is required")
	}
	var out struct {
		Data struct {
			Data map[string]any `json:"data"`
		} `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, "/v1/"+url.PathEscape(SiteConfigKVMount)+"/data/"+escapeOpenBaoPath(SiteConfigSecretPrefix+"/"+name), nil, http.StatusOK, &out); err != nil {
		return "", err
	}
	value, ok := out.Data.Data["value"].(string)
	if !ok || value == "" {
		return "", fmt.Errorf("openbao site config: %s has no string value", name)
	}
	return value, nil
}

func (c *SiteConfigClient) do(ctx context.Context, method, path string, body any, wantStatus int, out any) error {
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("X-Vault-Token", c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, openBaoHTTPResponseLimit))
	if err != nil {
		return err
	}
	if resp.StatusCode != wantStatus {
		return fmt.Errorf("openbao %s %s: HTTP %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	if out != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("decode openbao %s %s: %w", method, path, err)
		}
	}
	return nil
}

func escapeOpenBaoPath(path string) string {
	parts := strings.Split(path, "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	return strings.Join(parts, "/")
}

func mustShellWord(value string) string {
	word, err := ShellWord(value)
	if err != nil {
		panic(err)
	}
	return word
}
