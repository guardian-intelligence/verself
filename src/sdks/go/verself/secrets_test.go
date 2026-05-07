package verself

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSecretsPutResolveAndDeleteUseBearerScopeAndIdempotency(t *testing.T) {
	var putAuth string
	var putKey string
	var putTraceparent string
	var putBody map[string]any
	var resolveAuth string
	var resolveTraceparent string
	var resolveBody map[string]any
	var deleteKey string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPut && r.URL.Path == "/api/v1/secrets/API_TOKEN":
			putAuth = r.Header.Get("Authorization")
			putKey = r.Header.Get("Idempotency-Key")
			putTraceparent = r.Header.Get("Traceparent")
			if err := json.NewDecoder(r.Body).Decode(&putBody); err != nil {
				t.Fatal(err)
			}
			_, _ = w.Write([]byte(`{"secret_id":"sec_1","kind":"secret","name":"API_TOKEN","scope_level":"environment","source_id":"proj_1","env_id":"production","current_version":"1","created_at":"2026-05-07T00:00:00Z","updated_at":"2026-05-07T00:00:00Z"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/secrets:resolve":
			resolveAuth = r.Header.Get("Authorization")
			resolveTraceparent = r.Header.Get("Traceparent")
			if err := json.NewDecoder(r.Body).Decode(&resolveBody); err != nil {
				t.Fatal(err)
			}
			_, _ = w.Write([]byte(`{"values":[{"secret_id":"sec_1","kind":"secret","name":"API_TOKEN","scope_level":"environment","source_id":"proj_1","env_id":"production","current_version":"1","created_at":"2026-05-07T00:00:00Z","updated_at":"2026-05-07T00:00:00Z","value":"tok_live"}]}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/secrets/API_TOKEN":
			deleteKey = r.Header.Get("Idempotency-Key")
			if r.URL.Query().Get("scope_level") != "environment" || r.URL.Query().Get("source_id") != "proj_1" || r.URL.Query().Get("env_id") != "production" {
				t.Fatalf("unexpected delete query: %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"secret_id":"sec_1","kind":"secret","name":"API_TOKEN","scope_level":"environment","source_id":"proj_1","env_id":"production","current_version":"1","created_at":"2026-05-07T00:00:00Z","updated_at":"2026-05-07T00:00:00Z"}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	traceparent := "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	client, err := New(Options{BearerToken: "tok_test", SecretsURL: server.URL, Traceparent: traceparent})
	if err != nil {
		t.Fatal(err)
	}
	scope := SecretScope{Level: SecretScopeEnvironment, SourceID: "proj_1", EnvID: "production"}
	secret, err := client.Secrets.Put(ctxBackground(), "API_TOKEN", PutSecretInput{
		Scope:          scope,
		Value:          "tok_live",
		IdempotencyKey: "secret:test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if secret.Name != "API_TOKEN" || secret.Scope.Level != SecretScopeEnvironment {
		t.Fatalf("unexpected secret: %#v", secret)
	}
	if putAuth != "Bearer tok_test" || putKey != "secret:test" || putTraceparent != traceparent {
		t.Fatalf("unexpected put headers: auth=%q key=%q trace=%q", putAuth, putKey, putTraceparent)
	}
	if putBody["scope_level"] != "environment" || putBody["source_id"] != "proj_1" || putBody["env_id"] != "production" || putBody["value"] != "tok_live" {
		t.Fatalf("unexpected put body: %#v", putBody)
	}

	values, err := client.Secrets.Resolve(ctxBackground(), ResolveSecretsInput{
		Scope: scope,
		Names: []string{"API_TOKEN"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resolveAuth != "Bearer tok_test" || resolveTraceparent != traceparent {
		t.Fatalf("unexpected resolve headers: auth=%q trace=%q", resolveAuth, resolveTraceparent)
	}
	names, ok := resolveBody["names"].([]any)
	if !ok || len(names) != 1 || names[0] != "API_TOKEN" || resolveBody["scope_level"] != "environment" {
		t.Fatalf("unexpected resolve body: %#v", resolveBody)
	}
	if len(values.Values) != 1 || values.Values[0].Value != "tok_live" {
		t.Fatalf("unexpected resolved values: %#v", values)
	}

	_, err = client.Secrets.Delete(ctxBackground(), "API_TOKEN", DeleteSecretInput{
		Scope:          scope,
		IdempotencyKey: "secret:delete",
	})
	if err != nil {
		t.Fatal(err)
	}
	if deleteKey != "secret:delete" {
		t.Fatalf("unexpected delete idempotency key: %q", deleteKey)
	}
}

func TestSecretsNormalizeProblemDetails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"type":"urn:verself:problem:secrets:forbidden","title":"Forbidden","status":403,"detail":"secret read denied"}`))
	}))
	defer server.Close()

	client, err := New(Options{BearerToken: "tok_test", SecretsURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Secrets.Resolve(context.Background(), ResolveSecretsInput{
		Scope: SecretScope{Level: SecretScopeOrg},
		Names: []string{"API_TOKEN"},
	})
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("error = %#v, want *APIError", err)
	}
	if apiErr.StatusCode != http.StatusForbidden || apiErr.Title != "Forbidden" || !strings.Contains(apiErr.Error(), "secret read denied") {
		t.Fatalf("unexpected api error: %#v", apiErr)
	}
}

func ctxBackground() context.Context {
	return context.Background()
}
