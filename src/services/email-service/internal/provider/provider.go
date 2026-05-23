package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	NameCloudflare = "cloudflare"
	NameResend     = "resend"
)

type Message struct {
	From           string
	To             string
	Subject        string
	Text           string
	HTML           string
	IdempotencyKey string
	WorkflowKey    string
	WorkflowRunID  string
}

type Accepted struct {
	ProviderMessageID string
	Metadata          map[string]string
}

type Sender interface {
	Name() string
	Send(context.Context, Message) (Accepted, error)
}

type Resend struct {
	apiKey     string
	fromName   string
	endpoint   string
	httpClient *http.Client
}

type resendRequest struct {
	From    string            `json:"from"`
	To      []string          `json:"to"`
	Subject string            `json:"subject"`
	Text    string            `json:"text,omitempty"`
	HTML    string            `json:"html,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

type resendResponse struct {
	ID string `json:"id"`
}

func NewResend(apiKey, fromName string, client *http.Client) (*Resend, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, fmt.Errorf("resend api key is required")
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &Resend{
		apiKey:     apiKey,
		fromName:   strings.TrimSpace(fromName),
		endpoint:   "https://api.resend.com/emails",
		httpClient: client,
	}, nil
}

func (s *Resend) Name() string { return NameResend }

func (s *Resend) Send(ctx context.Context, msg Message) (Accepted, error) {
	payload := resendRequest{
		From:    formatFrom(s.fromName, msg.From),
		To:      []string{strings.TrimSpace(msg.To)},
		Subject: strings.TrimSpace(msg.Subject),
		Text:    msg.Text,
		HTML:    msg.HTML,
		Headers: workflowHeaders(msg),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return Accepted{}, fmt.Errorf("marshal resend payload: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, bytes.NewReader(body))
	if err != nil {
		return Accepted{}, fmt.Errorf("build resend request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", strings.TrimSpace(msg.IdempotencyKey))
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return Accepted{}, fmt.Errorf("resend send: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Accepted{}, providerStatusError{NameResend, resp.StatusCode, raw}
	}
	var decoded resendResponse
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return Accepted{}, fmt.Errorf("decode resend response: %w", err)
	}
	return Accepted{
		ProviderMessageID: strings.TrimSpace(decoded.ID),
		Metadata:          rateMetadata(resp.Header),
	}, nil
}

type Cloudflare struct {
	accountID  string
	apiToken   string
	endpoint   string
	httpClient *http.Client
}

type cloudflareRequest struct {
	From    string            `json:"from"`
	To      []string          `json:"to"`
	Subject string            `json:"subject"`
	Text    string            `json:"text,omitempty"`
	HTML    string            `json:"html,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

type cloudflareResponse struct {
	Success bool `json:"success"`
	Result  struct {
		ID string `json:"id"`
	} `json:"result"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

func NewCloudflare(accountID, apiToken string, client *http.Client) (*Cloudflare, error) {
	accountID = strings.TrimSpace(accountID)
	apiToken = strings.TrimSpace(apiToken)
	if accountID == "" {
		return nil, fmt.Errorf("cloudflare account id is required")
	}
	if apiToken == "" {
		return nil, fmt.Errorf("cloudflare api token is required")
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &Cloudflare{
		accountID:  accountID,
		apiToken:   apiToken,
		endpoint:   "https://api.cloudflare.com/client/v4/accounts/" + accountID + "/email/sending/send",
		httpClient: client,
	}, nil
}

func (s *Cloudflare) Name() string { return NameCloudflare }

func (s *Cloudflare) Send(ctx context.Context, msg Message) (Accepted, error) {
	payload := cloudflareRequest{
		From:    strings.TrimSpace(msg.From),
		To:      []string{strings.TrimSpace(msg.To)},
		Subject: strings.TrimSpace(msg.Subject),
		Text:    msg.Text,
		HTML:    msg.HTML,
		Headers: workflowHeaders(msg),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return Accepted{}, fmt.Errorf("marshal cloudflare payload: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, bytes.NewReader(body))
	if err != nil {
		return Accepted{}, fmt.Errorf("build cloudflare request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.apiToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return Accepted{}, fmt.Errorf("cloudflare send: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Accepted{}, providerStatusError{NameCloudflare, resp.StatusCode, raw}
	}
	var decoded cloudflareResponse
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return Accepted{}, fmt.Errorf("decode cloudflare response: %w", err)
	}
	if !decoded.Success {
		return Accepted{}, fmt.Errorf("cloudflare send failed: %s", cloudflareErrors(decoded))
	}
	return Accepted{
		ProviderMessageID: strings.TrimSpace(decoded.Result.ID),
		Metadata:          rateMetadata(resp.Header),
	}, nil
}

type providerStatusError struct {
	provider string
	status   int
	body     []byte
}

func (e providerStatusError) Error() string {
	return fmt.Sprintf("%s send status %d: %s", e.provider, e.status, strings.TrimSpace(string(e.body)))
}

func formatFrom(name, address string) string {
	name = strings.TrimSpace(name)
	address = strings.TrimSpace(address)
	if name == "" {
		return address
	}
	return fmt.Sprintf("%s <%s>", name, address)
}

func workflowHeaders(msg Message) map[string]string {
	headers := map[string]string{}
	if workflow := strings.TrimSpace(msg.WorkflowKey); workflow != "" {
		headers["X-Verself-Workflow"] = workflow
	}
	if runID := strings.TrimSpace(msg.WorkflowRunID); runID != "" {
		headers["X-Verself-Workflow-Run"] = runID
	}
	return headers
}

func rateMetadata(headers http.Header) map[string]string {
	out := map[string]string{}
	for _, key := range []string{"ratelimit-limit", "ratelimit-remaining", "ratelimit-reset", "retry-after"} {
		if value := strings.TrimSpace(headers.Get(key)); value != "" {
			out[key] = value
		}
	}
	return out
}

func cloudflareErrors(resp cloudflareResponse) string {
	if len(resp.Errors) == 0 {
		return "unknown cloudflare error"
	}
	parts := make([]string, 0, len(resp.Errors))
	for _, item := range resp.Errors {
		if msg := strings.TrimSpace(item.Message); msg != "" {
			parts = append(parts, msg)
		}
	}
	if len(parts) == 0 {
		return "unknown cloudflare error"
	}
	return strings.Join(parts, "; ")
}
