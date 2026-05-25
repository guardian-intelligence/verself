package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	iamapi "github.com/verself/iam-service/internal/api"
	notificationsinternalclient "github.com/verself/notifications-service/internalclient"
)

func TestNotificationSignupSenderIncludesProvisionalOrgID(t *testing.T) {
	capture := &captureNotificationWorkflowDoer{t: t}
	client, err := notificationsinternalclient.NewClient("https://notifications.internal", notificationsinternalclient.WithHTTPClient(capture))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	const signupIntentID = "signup_0123456789ABCDEFGHJKMNPQRS"
	const orgID = "org_0123456789ABCDEFGHJKMNPQRS"

	err = (notificationSignupSender{client: client}).SendSignupVerification(context.Background(), iamapi.SignupVerificationNotification{
		SignupIntentID:          signupIntentID,
		OrgID:                   orgID,
		Email:                   "operator@example.test",
		OrganizationDisplayName: "Operator",
		VerificationFingerprint: "sha256:verification",
		ActionURL:               "https://verself.sh/signup/verify?signup_intent_id=" + signupIntentID,
		ResourceName:            "urn:verself:inst_01J8QJ4P1R7S9W2X5M6N8P0Q2A:signup-intents/" + signupIntentID,
	})
	if err != nil {
		t.Fatalf("SendSignupVerification: %v", err)
	}

	if capture.path != "/internal/v1/workflows/iam.signup.verify/trigger" {
		t.Fatalf("notification path = %q", capture.path)
	}
	if capture.idempotencyKey != "iam:signup_verify:"+signupIntentID+":sha256:verification" {
		t.Fatalf("idempotency key = %q", capture.idempotencyKey)
	}
	if got, _ := capture.body["org_id"].(string); got != orgID {
		t.Fatalf("body org_id = %q", got)
	}
	data, _ := capture.body["data"].(map[string]any)
	if got, _ := data["org_id"].(string); got != orgID {
		t.Fatalf("data org_id = %q", got)
	}
}

type captureNotificationWorkflowDoer struct {
	t              *testing.T
	path           string
	idempotencyKey string
	body           map[string]any
}

func (d *captureNotificationWorkflowDoer) Do(req *http.Request) (*http.Response, error) {
	d.t.Helper()
	d.path = req.URL.Path
	d.idempotencyKey = req.Header.Get("Idempotency-Key")
	raw, err := io.ReadAll(req.Body)
	if err != nil {
		d.t.Fatalf("read request body: %v", err)
	}
	if err := json.Unmarshal(raw, &d.body); err != nil {
		d.t.Fatalf("decode request body: %v", err)
	}
	body := `{"workflow_run_id":"workflow_1","workflow_key":"iam.signup.verify","org_id":"org_0123456789ABCDEFGHJKMNPQRS","accepted_at":"2026-05-24T20:00:00Z","recipient_count":1,"web_notifications_accepted":0,"email_deliveries_queued":1,"suppressed_count":0}`
	return &http.Response{
		StatusCode: http.StatusAccepted,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}, nil
}
