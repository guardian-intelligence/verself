package notifications

import (
	"context"
	"fmt"
	"strings"

	emailinternalclient "github.com/verself/email-service/internalclient"
)

type EmailMessage struct {
	OrgID          string
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

type EmailServiceSender struct {
	client      *emailinternalclient.Client
	fromAddress string
	fromOrgID   string
}

func NewEmailServiceSender(client *emailinternalclient.Client, fromAddress string, fromOrgID string) (*EmailServiceSender, error) {
	if client == nil {
		return nil, fmt.Errorf("%w: email-service client is required", ErrInvalidInput)
	}
	fromAddress = strings.TrimSpace(strings.ToLower(fromAddress))
	if fromAddress == "" {
		return nil, fmt.Errorf("%w: email-service from address is required", ErrInvalidInput)
	}
	fromOrgID = strings.TrimSpace(fromOrgID)
	if fromOrgID == "" {
		return nil, fmt.Errorf("%w: email-service from org id is required", ErrInvalidInput)
	}
	return &EmailServiceSender{client: client, fromAddress: fromAddress, fromOrgID: fromOrgID}, nil
}

func (s *EmailServiceSender) Send(ctx context.Context, msg EmailMessage) (ProviderMessage, error) {
	result, err := s.client.Send(ctx, emailinternalclient.SendRequest{
		OrgID:          s.fromOrgID,
		FromAddress:    s.fromAddress,
		ToAddress:      strings.TrimSpace(msg.To),
		Subject:        strings.TrimSpace(msg.Subject),
		TextBody:       strings.TrimSpace(msg.Text),
		IdempotencyKey: strings.TrimSpace(msg.IdempotencyKey),
		WorkflowKey:    strings.TrimSpace(msg.WorkflowKey),
		WorkflowRunID:  strings.TrimSpace(msg.WorkflowRunID),
		ActorSubject:   ServiceName,
	})
	if err != nil {
		return ProviderMessage{}, err
	}
	return ProviderMessage{ID: strings.TrimSpace(result.ProviderMessageID)}, nil
}
