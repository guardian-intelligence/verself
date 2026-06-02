package r2control

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const CloudflareAPIBase = "https://api.cloudflare.com/client/v4"

var cloudflareAPIBase = CloudflareAPIBase

type CloudflareAPIClient struct {
	apiBase string
	token   string
	http    *http.Client
}

type tokenVerifyResponse struct {
	Success bool `json:"success"`
	Result  struct {
		ID        string `json:"id"`
		Status    string `json:"status"`
		ExpiresOn string `json:"expires_on"`
	} `json:"result"`
	Errors []cloudflareMessage `json:"errors"`
}

type TokenVerification struct {
	ID        string
	Status    string
	ExpiresOn string
}

type temporaryCredentialsResponse struct {
	Success bool `json:"success"`
	Result  struct {
		AccessKeyID     string `json:"accessKeyId"`
		SecretAccessKey string `json:"secretAccessKey"`
		SessionToken    string `json:"sessionToken"`
	} `json:"result"`
	Errors []cloudflareMessage `json:"errors"`
}

type cloudflareMessage struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func NewCloudflareAPIClient(apiToken string, timeout time.Duration) (*CloudflareAPIClient, error) {
	if strings.TrimSpace(apiToken) == "" {
		return nil, fmt.Errorf("cloudflare API token value is required")
	}
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	return &CloudflareAPIClient{
		apiBase: cloudflareAPIBase,
		token:   strings.TrimSpace(apiToken),
		http:    &http.Client{Timeout: timeout},
	}, nil
}

func (c *CloudflareAPIClient) VerifyAccountToken(ctx context.Context, accountID string) (TokenVerification, error) {
	accountID = strings.ToLower(strings.TrimSpace(accountID))
	if !IsCloudflareAccountID(accountID) {
		return TokenVerification{}, fmt.Errorf("account ID must be a 32-character lowercase hex Cloudflare account ID")
	}
	var response tokenVerifyResponse
	if err := c.doJSON(ctx, http.MethodGet, "/accounts/"+url.PathEscape(accountID)+"/tokens/verify", nil, &response); err != nil {
		return TokenVerification{}, err
	}
	if !response.Success {
		return TokenVerification{}, fmt.Errorf("cloudflare token verify failed: %s", cloudflareErrors(response.Errors))
	}
	if response.Result.ID == "" {
		return TokenVerification{}, fmt.Errorf("cloudflare token verify returned no token ID")
	}
	return TokenVerification{
		ID:        response.Result.ID,
		Status:    response.Result.Status,
		ExpiresOn: response.Result.ExpiresOn,
	}, nil
}

func (c *CloudflareAPIClient) CreateTemporaryCredentials(ctx context.Context, accountID string, req TemporaryCredentialRequest) (TemporaryCredentials, error) {
	accountID = strings.ToLower(strings.TrimSpace(accountID))
	if !IsCloudflareAccountID(accountID) {
		return TemporaryCredentials{}, fmt.Errorf("account ID must be a 32-character lowercase hex Cloudflare account ID")
	}
	req, err := req.validate()
	if err != nil {
		return TemporaryCredentials{}, err
	}
	body := map[string]any{
		"bucket":            req.Bucket,
		"parentAccessKeyId": req.ParentAccessKeyID,
		"permission":        req.Permission,
		"ttlSeconds":        int64(req.TTL / time.Second),
	}
	if len(req.Prefixes) > 0 {
		body["prefixes"] = req.Prefixes
	}
	if len(req.Objects) > 0 {
		body["objects"] = req.Objects
	}
	var response temporaryCredentialsResponse
	if err := c.doJSON(ctx, http.MethodPost, "/accounts/"+url.PathEscape(accountID)+"/r2/temp-access-credentials", body, &response); err != nil {
		return TemporaryCredentials{}, err
	}
	if !response.Success {
		return TemporaryCredentials{}, fmt.Errorf("cloudflare temporary credential create failed: %s", cloudflareErrors(response.Errors))
	}
	if strings.TrimSpace(response.Result.AccessKeyID) == "" || strings.TrimSpace(response.Result.SecretAccessKey) == "" || strings.TrimSpace(response.Result.SessionToken) == "" {
		return TemporaryCredentials{}, fmt.Errorf("cloudflare temporary credential create returned incomplete credentials")
	}
	return TemporaryCredentials{
		AccessKeyID:     strings.TrimSpace(response.Result.AccessKeyID),
		SecretAccessKey: strings.TrimSpace(response.Result.SecretAccessKey),
		SessionToken:    strings.TrimSpace(response.Result.SessionToken),
	}, nil
}

func (c *CloudflareAPIClient) doJSON(ctx context.Context, method, path string, body any, out any) error {
	var reader io.Reader = http.NoBody
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode Cloudflare API request: %w", err)
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.apiBase+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("cloudflare API %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("read Cloudflare API response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("cloudflare API %s %s status %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("decode Cloudflare API response: %w", err)
		}
	}
	return nil
}

func cloudflareErrors(messages []cloudflareMessage) string {
	if len(messages) == 0 {
		return "unknown error"
	}
	out := make([]string, 0, len(messages))
	for _, msg := range messages {
		if msg.Code == 0 {
			out = append(out, msg.Message)
			continue
		}
		out = append(out, fmt.Sprintf("%d: %s", msg.Code, msg.Message))
	}
	return strings.Join(out, "; ")
}
