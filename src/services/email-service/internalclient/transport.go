package emailinternalclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const ServiceName = "email-service"

type Client struct {
	baseURL string
	client  *http.Client
}

type SendRequest struct {
	OrgID          string `json:"org_id"`
	FromAddress    string `json:"from_address"`
	ToAddress      string `json:"to_address"`
	Subject        string `json:"subject"`
	TextBody       string `json:"text_body"`
	HTMLBody       string `json:"html_body"`
	IdempotencyKey string `json:"idempotency_key"`
	WorkflowKey    string `json:"workflow_key"`
	WorkflowRunID  string `json:"workflow_run_id"`
	ActorSubject   string `json:"actor_subject"`
}

type SendResult struct {
	MessageID         string `json:"message_id"`
	Provider          string `json:"provider"`
	ProviderMessageID string `json:"provider_message_id"`
	Status            string `json:"status"`
}

func New(baseURL string, client *http.Client) (*Client, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("email internal client base URL is required")
	}
	if client == nil {
		client = http.DefaultClient
	}
	return &Client{baseURL: baseURL, client: client}, nil
}

func (c *Client) Send(ctx context.Context, input SendRequest) (SendResult, error) {
	raw, err := json.Marshal(input)
	if err != nil {
		return SendResult{}, fmt.Errorf("marshal email send request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/internal/v1/email/send", bytes.NewReader(raw))
	if err != nil {
		return SendResult{}, fmt.Errorf("build email send request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return SendResult{}, fmt.Errorf("email send: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return SendResult{}, fmt.Errorf("email send status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var result SendResult
	if err := json.Unmarshal(body, &result); err != nil {
		return SendResult{}, fmt.Errorf("decode email send response: %w", err)
	}
	return result, nil
}
