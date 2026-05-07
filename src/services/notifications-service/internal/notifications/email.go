package notifications

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

const resendEndpoint = "https://api.resend.com/emails"

type EmailMessage struct {
	To             string
	Subject        string
	Text           string
	IdempotencyKey string
	WorkflowKey    string
	WorkflowRunID  string
}

type ProviderMessage struct {
	ID string
}

type EmailSender interface {
	Send(ctx context.Context, msg EmailMessage) (ProviderMessage, error)
}

type ResendSender struct {
	apiKey      string
	fromAddress string
	fromName    string
	client      *http.Client
	endpoint    string
}

type resendSendRequest struct {
	From    string            `json:"from"`
	To      []string          `json:"to"`
	Subject string            `json:"subject"`
	Text    string            `json:"text"`
	Headers map[string]string `json:"headers,omitempty"`
}

type resendSendResponse struct {
	ID string `json:"id"`
}

func NewResendSender(apiKey, fromAddress, fromName string, client *http.Client) (*ResendSender, error) {
	apiKey = strings.TrimSpace(apiKey)
	fromAddress = strings.TrimSpace(fromAddress)
	fromName = strings.TrimSpace(fromName)
	if apiKey == "" {
		return nil, fmt.Errorf("%w: resend api key is required", ErrInvalidInput)
	}
	if fromAddress == "" {
		return nil, fmt.Errorf("%w: resend from address is required", ErrInvalidInput)
	}
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	return &ResendSender{
		apiKey:      apiKey,
		fromAddress: fromAddress,
		fromName:    fromName,
		client:      client,
		endpoint:    resendEndpoint,
	}, nil
}

func (s *ResendSender) Send(ctx context.Context, msg EmailMessage) (ProviderMessage, error) {
	payload := resendSendRequest{
		From:    formatSender(s.fromName, s.fromAddress),
		To:      []string{strings.TrimSpace(msg.To)},
		Subject: strings.TrimSpace(msg.Subject),
		Text:    strings.TrimSpace(msg.Text),
		Headers: map[string]string{
			"X-Verself-Workflow":     strings.TrimSpace(msg.WorkflowKey),
			"X-Verself-Workflow-Run": strings.TrimSpace(msg.WorkflowRunID),
		},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return ProviderMessage{}, fmt.Errorf("marshal resend payload: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, bytes.NewReader(raw))
	if err != nil {
		return ProviderMessage{}, fmt.Errorf("build resend request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", strings.TrimSpace(msg.IdempotencyKey))
	resp, err := s.client.Do(req)
	if err != nil {
		return ProviderMessage{}, fmt.Errorf("resend send: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ProviderMessage{}, fmt.Errorf("resend send status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var decoded resendSendResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return ProviderMessage{}, fmt.Errorf("decode resend response: %w", err)
	}
	return ProviderMessage{ID: strings.TrimSpace(decoded.ID)}, nil
}

func formatSender(name, address string) string {
	name = strings.TrimSpace(name)
	address = strings.TrimSpace(address)
	if name == "" {
		return address
	}
	return fmt.Sprintf("%s <%s>", name, address)
}
