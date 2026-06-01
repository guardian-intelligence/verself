package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

type Client struct {
	base  *url.URL
	http  *http.Client
	token string
}

type Config struct {
	Address string
	Token   string
	Timeout time.Duration
}

func New(cfg Config) (*Client, error) {
	raw := strings.TrimSpace(cfg.Address)
	if raw == "" {
		return nil, fmt.Errorf("R2 control-plane address is required")
	}
	base, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("parse R2 control-plane address: %w", err)
	}
	if base.Scheme != "http" && base.Scheme != "https" {
		return nil, fmt.Errorf("R2 control-plane address must be http or https")
	}
	if base.Host == "" {
		return nil, fmt.Errorf("R2 control-plane address requires a host")
	}
	if strings.TrimSpace(cfg.Token) == "" {
		return nil, fmt.Errorf("R2 control-plane bearer token is required")
	}
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 2 * time.Minute
	}
	return &Client{
		base:  base,
		http:  &http.Client{Timeout: timeout},
		token: strings.TrimSpace(cfg.Token),
	}, nil
}

func (c *Client) CreateUploadSession(ctx context.Context, req CreateUploadSessionRequest) (CreateUploadSessionResponse, error) {
	var out CreateUploadSessionResponse
	if err := c.doJSON(ctx, http.MethodPost, "/v1/sites/"+url.PathEscape(req.Site)+"/artifact-upload-sessions", req, &out); err != nil {
		return CreateUploadSessionResponse{}, err
	}
	return out, nil
}

func (c *Client) CompleteUploadSession(ctx context.Context, site, sessionID string) (CompleteUploadSessionResponse, error) {
	var out CompleteUploadSessionResponse
	u := "/v1/sites/" + url.PathEscape(site) + "/artifact-upload-sessions/" + url.PathEscape(sessionID) + "/complete"
	if err := c.doJSON(ctx, http.MethodPost, u, map[string]string{"session_id": sessionID}, &out); err != nil {
		return CompleteUploadSessionResponse{}, err
	}
	return out, nil
}

func (c *Client) doJSON(ctx context.Context, method, rel string, in any, out any) error {
	var body io.Reader = http.NoBody
	if in != nil {
		encoded, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("encode R2 control-plane request: %w", err)
		}
		body = bytes.NewReader(encoded)
	}
	u := *c.base
	u.Path = path.Join(strings.TrimRight(c.base.Path, "/"), rel)
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("R2 control-plane %s %s: %w", method, u.Redacted(), err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("read R2 control-plane response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("R2 control-plane %s %s status %d: %s", method, u.Redacted(), resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if out == nil || len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode R2 control-plane response: %w", err)
	}
	return nil
}
