package jmap

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Config struct {
	BaseURL  string
	Username string
	Password string
}

type Client struct {
	baseURL      string
	authHeader   string
	httpClient   *http.Client
	streamClient *http.Client
}

func New(cfg Config, httpClient, streamClient *http.Client) (*Client, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("jmap base URL is empty")
	}
	if strings.TrimSpace(cfg.Username) == "" {
		return nil, fmt.Errorf("jmap username is empty")
	}
	if strings.TrimSpace(cfg.Password) == "" {
		return nil, fmt.Errorf("jmap password is empty")
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	if streamClient == nil {
		streamClient = httpClient
	}
	token := cfg.Username + ":" + cfg.Password
	return &Client{
		baseURL:      baseURL,
		authHeader:   "Basic " + base64.StdEncoding.EncodeToString([]byte(token)),
		httpClient:   httpClient,
		streamClient: streamClient,
	}, nil
}

func (c *Client) Session(ctx context.Context) (Session, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/jmap/session", nil)
	if err != nil {
		return Session{}, fmt.Errorf("build session request: %w", err)
	}
	req.Header.Set("Authorization", c.authHeader)

	var session Session
	if err := c.doJSON(c.httpClient, req, &session); err != nil {
		return Session{}, fmt.Errorf("session request: %w", err)
	}
	return session, nil
}

func (c *Client) AccountID(ctx context.Context) (string, error) {
	session, err := c.Session(ctx)
	if err != nil {
		return "", err
	}
	for accountID := range session.Accounts {
		return accountID, nil
	}
	return "", fmt.Errorf("no JMAP accounts returned")
}

func (c *Client) Call(ctx context.Context, request Request) (Response, error) {
	raw, err := json.Marshal(request)
	if err != nil {
		return Response{}, fmt.Errorf("marshal jmap request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/jmap", bytes.NewReader(raw))
	if err != nil {
		return Response{}, fmt.Errorf("build jmap request: %w", err)
	}
	req.Header.Set("Authorization", c.authHeader)
	req.Header.Set("Content-Type", "application/json")

	var response Response
	if err := c.doJSON(c.httpClient, req, &response); err != nil {
		return Response{}, err
	}
	return response, nil
}

func (c *Client) doJSON(client *http.Client, req *http.Request, out any) error {
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response body: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("unexpected status %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode json: %w", err)
	}
	return nil
}
