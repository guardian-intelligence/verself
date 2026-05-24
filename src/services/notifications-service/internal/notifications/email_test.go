package notifications

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	emailinternalclient "github.com/verself/email-service/internalclient"
)

func TestEmailServiceSenderUsesConfiguredSenderOrg(t *testing.T) {
	const senderOrgID = "org_sender"
	const targetOrgID = "org_target"
	var request emailinternalclient.SendRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/v1/email/send" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"message_id":"email_msg_1","provider":"resend","provider_message_id":"resend_1","status":"sent"}`))
	}))
	defer server.Close()

	client, err := emailinternalclient.New(server.URL, server.Client())
	if err != nil {
		t.Fatalf("email client: %v", err)
	}
	sender, err := NewEmailServiceSender(client, "noreply@notify.verself.sh", senderOrgID)
	if err != nil {
		t.Fatalf("NewEmailServiceSender: %v", err)
	}

	if _, err := sender.Send(context.Background(), EmailMessage{
		OrgID:          targetOrgID,
		To:             "operator@example.test",
		Subject:        "Verify",
		Text:           "Verify signup",
		IdempotencyKey: "delivery-1",
		WorkflowKey:    "iam.signup.verify",
		WorkflowRunID:  "workflow-1",
	}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if request.OrgID != senderOrgID {
		t.Fatalf("email send org_id = %q, want sender org %q", request.OrgID, senderOrgID)
	}
}

func TestNextEmailDeliveryAttemptAtBacksOffProviderQuota(t *testing.T) {
	now := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)

	if got := nextEmailDeliveryAttemptAt(now, 1, errors.New("provider unavailable")); got != now.Add(time.Minute) {
		t.Fatalf("first retry = %s", got)
	}
	if got := nextEmailDeliveryAttemptAt(now, 3, errors.New("provider unavailable")); got != now.Add(15*time.Minute) {
		t.Fatalf("third retry = %s", got)
	}
	if got := nextEmailDeliveryAttemptAt(now, 4, errors.New("daily_quota_exceeded")); got != now.Add(24*time.Hour) {
		t.Fatalf("quota retry = %s", got)
	}
	if got := nextEmailDeliveryAttemptAt(now, maxEmailDeliveryAttempts, errors.New("provider unavailable")); got != now.Add(24*time.Hour) {
		t.Fatalf("max attempt retry = %s", got)
	}
}
