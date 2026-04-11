package e2e_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func createWebhookEndpointForE2E(t *testing.T, ctx context.Context, baseURL, token, providerHost string) (string, string) {
	t.Helper()

	body, err := json.Marshal(map[string]string{
		"provider":      "forgejo",
		"provider_host": providerHost,
		"label":         "e2e-forgejo",
	})
	if err != nil {
		t.Fatalf("marshal webhook endpoint request: %v", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/v1/webhook-endpoints", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build webhook endpoint request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "e2e-create-webhook-endpoint")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create webhook endpoint: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 201 creating webhook endpoint, got %d body=%s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var created struct {
		WebhookURL string `json:"webhook_url"`
		Secret     string `json:"secret"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode webhook endpoint response: %v", err)
	}
	if created.WebhookURL == "" || created.Secret == "" {
		t.Fatalf("expected webhook_url and secret, got %#v", created)
	}
	if strings.HasPrefix(created.WebhookURL, "/") {
		created.WebhookURL = strings.TrimRight(baseURL, "/") + created.WebhookURL
	}
	return created.WebhookURL, created.Secret
}

type webhookDeliveryView struct {
	DeliveryID   string
	State        string
	AttemptCount int
	LastError    string
}

func waitForWebhookDeliveryState(t *testing.T, ctx context.Context, db *sql.DB, providerDeliveryID, terminalState string) webhookDeliveryView {
	t.Helper()

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		var delivery webhookDeliveryView
		err := db.QueryRowContext(ctx, `
			SELECT delivery_id::text, state, attempt_count, last_error
			FROM webhook_deliveries
			WHERE provider_delivery_id = $1
			ORDER BY received_at DESC
			LIMIT 1
		`, providerDeliveryID).Scan(&delivery.DeliveryID, &delivery.State, &delivery.AttemptCount, &delivery.LastError)
		if err == nil {
			if delivery.State == terminalState {
				return delivery
			}
			if delivery.State == "failed" && terminalState != "failed" {
				t.Fatalf("webhook delivery %s failed unexpectedly: %s", providerDeliveryID, delivery.LastError)
			}
		} else if err != sql.ErrNoRows {
			t.Fatalf("query webhook delivery: %v", err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("webhook delivery %s did not reach %s before timeout", providerDeliveryID, terminalState)
	return webhookDeliveryView{}
}

func assertWebhookDeliveryClickHouse(t *testing.T, ctx context.Context, queryCHConn anyQueryRower, providerDeliveryID, state string) {
	t.Helper()

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		var eventCount uint64
		if err := queryCHConn.QueryRow(ctx, `
			SELECT count()
			FROM forge_metal.webhook_delivery_events
			WHERE provider_delivery_id = $1 AND state = $2
		`, providerDeliveryID, state).Scan(&eventCount); err != nil {
			t.Fatalf("query webhook_delivery_events: %v", err)
		}
		if eventCount > 0 {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("webhook_delivery_events missing provider_delivery_id=%s state=%s", providerDeliveryID, state)
}
