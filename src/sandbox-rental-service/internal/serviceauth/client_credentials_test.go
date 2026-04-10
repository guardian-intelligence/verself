package serviceauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewBearerTokenRequestEditorUsesClientCredentialsAndCachesToken(t *testing.T) {
	t.Parallel()

	var tokenRequests atomic.Int32
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenRequests.Add(1)

		if r.Host != "auth.example.com" {
			t.Fatalf("unexpected host header %q", r.Host)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method %q", r.Method)
		}
		username, password, ok := r.BasicAuth()
		if !ok {
			t.Fatal("expected basic auth")
		}
		if username != "sandbox-rental-billing" {
			t.Fatalf("unexpected username %q", username)
		}
		if password != "client-secret" {
			t.Fatalf("unexpected password %q", password)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		scope := r.Form.Get("scope")
		if !strings.Contains(scope, "urn:zitadel:iam:org:project:id:billing-audience:aud") {
			t.Fatalf("unexpected scope %q", scope)
		}
		if !strings.Contains(scope, "urn:zitadel:iam:org:projects:roles") {
			t.Fatalf("scope %q is missing Zitadel project role claims", scope)
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"access_token": "token-123",
			"token_type":   "Bearer",
			"expires_in":   3600,
		}); err != nil {
			t.Fatalf("encode response: %v", err)
		}
	}))
	defer tokenServer.Close()

	editor, err := NewBearerTokenRequestEditor(ClientCredentialsConfig{
		IssuerURL:    "https://auth.example.com",
		TokenURL:     tokenServer.URL,
		ClientID:     "sandbox-rental-billing",
		ClientSecret: "client-secret",
		Audience:     "billing-audience",
	})
	if err != nil {
		t.Fatalf("NewBearerTokenRequestEditor: %v", err)
	}

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "http://billing.internal/internal/billing/v1/reserve", nil)
		if err := editor(context.Background(), req); err != nil {
			t.Fatalf("editor call %d: %v", i, err)
		}
		if got := req.Header.Get("Authorization"); got != "Bearer token-123" {
			t.Fatalf("unexpected authorization header %q", got)
		}
	}

	if got := tokenRequests.Load(); got != 1 {
		t.Fatalf("expected one token request, got %d", got)
	}
}

func TestClientCredentialsConfigValidate(t *testing.T) {
	t.Parallel()

	_, err := NewBearerTokenRequestEditor(ClientCredentialsConfig{
		Timeout: time.Second,
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
}
