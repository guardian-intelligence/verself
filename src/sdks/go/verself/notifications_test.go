package verself

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNotificationsUseBearerTraceAndIdempotency(t *testing.T) {
	const notificationID = "11111111-1111-1111-1111-111111111111"
	var listAuth string
	var listTraceparent string
	var preferencesKey string
	var preferencesBody map[string]any
	var readCursorKey string
	var readCursorBody map[string]any
	var readKey string
	var dismissKey string
	var clearKey string
	var testKey string
	var testBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/notifications":
			listAuth = r.Header.Get("Authorization")
			listTraceparent = r.Header.Get("Traceparent")
			if r.URL.Query().Get("limit") != "10" {
				t.Fatalf("unexpected list query: %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"summary":` + notificationSummaryJSON(notificationID, "0", "1") + `,"notifications":[` + notificationJSON(notificationID, "1") + `]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/notifications/summary":
			_, _ = w.Write([]byte(notificationSummaryJSON(notificationID, "0", "1")))
		case r.Method == http.MethodPut && r.URL.Path == "/api/v1/notifications/preferences":
			preferencesKey = r.Header.Get("Idempotency-Key")
			if err := json.NewDecoder(r.Body).Decode(&preferencesBody); err != nil {
				t.Fatal(err)
			}
			_, _ = w.Write([]byte(notificationSummaryJSON(notificationID, "0", "1")))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/notifications/read-cursor":
			readCursorKey = r.Header.Get("Idempotency-Key")
			if err := json.NewDecoder(r.Body).Decode(&readCursorBody); err != nil {
				t.Fatal(err)
			}
			_, _ = w.Write([]byte(notificationSummaryJSON(notificationID, "1", "1")))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/notifications/"+notificationID+"/read":
			readKey = r.Header.Get("Idempotency-Key")
			_, _ = w.Write([]byte(notificationSummaryJSON(notificationID, "1", "1")))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/notifications/"+notificationID+"/dismiss":
			dismissKey = r.Header.Get("Idempotency-Key")
			_, _ = w.Write([]byte(notificationSummaryJSON(notificationID, "1", "1")))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/notifications/clear":
			clearKey = r.Header.Get("Idempotency-Key")
			_, _ = w.Write([]byte(notificationSummaryJSON(notificationID, "1", "1")))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/notifications/test":
			testKey = r.Header.Get("Idempotency-Key")
			if err := json.NewDecoder(r.Body).Decode(&testBody); err != nil {
				t.Fatal(err)
			}
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"event_id":"evt_1","traceparent":"trace-child"}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	traceparent := "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	client, err := New(Options{BearerToken: "tok_test", NotificationsURL: server.URL, Traceparent: traceparent})
	if err != nil {
		t.Fatal(err)
	}
	list, err := client.Notifications.List(context.Background(), ListNotificationsOptions{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if listAuth != "Bearer tok_test" || listTraceparent != traceparent {
		t.Fatalf("unexpected list headers: auth=%q trace=%q", listAuth, listTraceparent)
	}
	if len(list.Notifications) != 1 || list.Notifications[0].RecipientSequence != 1 {
		t.Fatalf("unexpected notifications: %#v", list)
	}
	summary, err := client.Notifications.Summary(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if summary.LatestNotification == nil || summary.LatestNotification.NotificationID != notificationID {
		t.Fatalf("unexpected summary: %#v", summary)
	}
	_, err = client.Notifications.PutPreferences(context.Background(), PutNotificationPreferencesInput{
		Version:        1,
		Enabled:        false,
		IdempotencyKey: "notifications:preferences",
	})
	if err != nil {
		t.Fatal(err)
	}
	if preferencesKey != "notifications:preferences" || preferencesBody["enabled"] != false || preferencesBody["version"] != float64(1) {
		t.Fatalf("unexpected preferences mutation: key=%q body=%#v", preferencesKey, preferencesBody)
	}
	_, err = client.Notifications.MarkRead(context.Background(), MarkNotificationsReadInput{
		ReadUpToSequence: 1,
		IdempotencyKey:   "notifications:cursor",
	})
	if err != nil {
		t.Fatal(err)
	}
	if readCursorKey != "notifications:cursor" || readCursorBody["read_up_to_sequence"] != "1" {
		t.Fatalf("unexpected cursor mutation: key=%q body=%#v", readCursorKey, readCursorBody)
	}
	_, err = client.Notifications.MarkNotificationRead(context.Background(), NotificationIDMutationInput{
		NotificationID: notificationID,
		IdempotencyKey: "notifications:read",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Notifications.Dismiss(context.Background(), NotificationIDMutationInput{
		NotificationID: notificationID,
		IdempotencyKey: "notifications:dismiss",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Notifications.Clear(context.Background(), NotificationsMutationOptions{IdempotencyKey: "notifications:clear"})
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := client.Notifications.PublishTest(context.Background(), PublishTestNotificationInput{
		Title:          "Smoke",
		Body:           "Body",
		ActionURL:      "https://verself.sh/runs/1",
		IdempotencyKey: "notifications:test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if readKey != "notifications:read" || dismissKey != "notifications:dismiss" || clearKey != "notifications:clear" || testKey != "notifications:test" {
		t.Fatalf("unexpected mutation keys: read=%q dismiss=%q clear=%q test=%q", readKey, dismissKey, clearKey, testKey)
	}
	if testBody["title"] != "Smoke" || testBody["body"] != "Body" || testBody["action_url"] != "https://verself.sh/runs/1" {
		t.Fatalf("unexpected test body: %#v", testBody)
	}
	if accepted.EventID != "evt_1" || accepted.Traceparent != "trace-child" {
		t.Fatalf("unexpected accepted response: %#v", accepted)
	}
}

func TestNotificationsNormalizeProblemDetails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"type":"urn:verself:problem:notifications:forbidden","title":"Forbidden","status":403,"detail":"notification inbox denied"}`))
	}))
	defer server.Close()

	client, err := New(Options{BearerToken: "tok_test", NotificationsURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Notifications.Summary(context.Background())
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("error = %#v, want *APIError", err)
	}
	if apiErr.StatusCode != http.StatusForbidden || apiErr.Title != "Forbidden" || !strings.Contains(apiErr.Error(), "notification inbox denied") {
		t.Fatalf("unexpected api error: %#v", apiErr)
	}
}

func notificationJSON(notificationID, sequence string) string {
	return `{"notification_id":"` + notificationID + `","org_id":"org_00000000000000000000000000","recipient_subject_id":"user_1","recipient_sequence":"` + sequence + `","kind":"test","priority":"normal","title":"Smoke","body":"Body","action_url":"https://verself.sh","resource_kind":"run","resource_id":"run_1","created_at":"2026-05-07T00:00:00Z"}`
}

func notificationSummaryJSON(notificationID, readUpTo, latest string) string {
	return `{"org_id":"org_00000000000000000000000000","subject_id":"user_1","unread_count":1,"latest_sequence":"` + latest + `","read_up_to_sequence":"` + readUpTo + `","preferences":{"enabled":true,"web_enabled":true,"email_enabled":true,"push_enabled":false,"sms_enabled":false,"version":1,"updated_at":"2026-05-07T00:00:00Z","updated_by":"user_1"},"latest_notification":` + notificationJSON(notificationID, latest) + `}`
}
