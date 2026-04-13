package jobs

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGitHubRunnerVerifyWebhookSignature(t *testing.T) {
	t.Parallel()

	runner := &GitHubRunner{webhookSecret: "secret", privateKey: mustTestRSAPrivateKey(t), appSlug: "forge-metal", clientID: "client", clientSecret: "client-secret"}
	body := []byte(`{"action":"queued"}`)
	signature := "sha256=" + hmacSHA256Hex([]byte("secret"), body)

	if !runner.VerifyWebhookSignature(body, signature) {
		t.Fatal("VerifyWebhookSignature rejected a valid signature")
	}
	if runner.VerifyWebhookSignature(body, signature[:len(signature)-1]+"0") {
		t.Fatal("VerifyWebhookSignature accepted an invalid signature")
	}
}

func TestGitHubRunnerCreateJITConfig(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/app/installations/42/access_tokens":
			if r.Method != http.MethodPost {
				t.Fatalf("installation token method = %s, want POST", r.Method)
			}
			if got := r.Header.Get("X-GitHub-Api-Version"); got != "2026-03-10" {
				t.Fatalf("api version = %q", got)
			}
			if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
				t.Fatal("missing app JWT authorization header")
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"token":"installation-token","expires_at":"2099-01-01T00:00:00Z"}`))
		case "/orgs/acme/actions/runners/generate-jitconfig":
			if got := r.Header.Get("Authorization"); got != "Bearer installation-token" {
				t.Fatalf("authorization = %q, want installation token", got)
			}
			var body struct {
				Name          string   `json:"name"`
				RunnerGroupID int64    `json:"runner_group_id"`
				Labels        []string `json:"labels"`
				WorkFolder    string   `json:"work_folder"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode jit request: %v", err)
			}
			if body.Name != "forge-metal-99" || body.RunnerGroupID != 7 || body.WorkFolder != "_work" {
				t.Fatalf("jit request body = %#v", body)
			}
			wantLabels := []string{"self-hosted", "linux", "x64", DefaultRunnerClassLabel}
			if len(body.Labels) != len(wantLabels) {
				t.Fatalf("jit labels = %#v", body.Labels)
			}
			for i, want := range wantLabels {
				if body.Labels[i] != want {
					t.Fatalf("jit labels = %#v, want %#v", body.Labels, wantLabels)
				}
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"encoded_jit_config":"encoded-jit"}`))
		default:
			t.Fatalf("unexpected github API path %s", r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)

	runner, err := NewGitHubRunner(&Service{}, GitHubRunnerConfig{
		AppID:         123,
		AppSlug:       "forge-metal",
		ClientID:      "client",
		ClientSecret:  "client-secret",
		PrivateKeyPEM: mustTestRSAPrivateKeyPEM(t),
		WebhookSecret: "secret",
		APIBaseURL:    server.URL,
		WebBaseURL:    "https://github.test",
		RunnerGroupID: 7,
	}, server.Client())
	if err != nil {
		t.Fatalf("NewGitHubRunner: %v", err)
	}
	jit, err := runner.createJITConfig(context.Background(), 42, "acme", 99, DefaultRunnerClassLabel)
	if err != nil {
		t.Fatalf("createJITConfig: %v", err)
	}
	if jit != "encoded-jit" {
		t.Fatalf("jit = %q", jit)
	}
}

func TestGitHubRunnerFetchInstallationRejectsSuspendedInstallation(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/app/installations/42" {
			t.Fatalf("unexpected github API path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": 42,
			"account": {"login": "acme", "type": "Organization"},
			"suspended_at": "2026-04-13T00:00:00Z"
		}`))
	}))
	t.Cleanup(server.Close)

	runner, err := NewGitHubRunner(&Service{}, GitHubRunnerConfig{
		AppID:         123,
		AppSlug:       "forge-metal",
		ClientID:      "client",
		ClientSecret:  "client-secret",
		PrivateKeyPEM: mustTestRSAPrivateKeyPEM(t),
		WebhookSecret: "secret",
		APIBaseURL:    server.URL,
		WebBaseURL:    "https://github.test",
	}, server.Client())
	if err != nil {
		t.Fatalf("NewGitHubRunner: %v", err)
	}
	_, err = runner.fetchInstallation(context.Background(), 42)
	if err != ErrGitHubInstallationInvalid {
		t.Fatalf("fetchInstallation error = %v, want ErrGitHubInstallationInvalid", err)
	}
}

func TestGitHubRunnerFetchInstallationRejectsPersonalAccountInstallation(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/app/installations/42" {
			t.Fatalf("unexpected github API path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": 42,
			"account": {"login": "octocat", "type": "User"},
			"suspended_at": null
		}`))
	}))
	t.Cleanup(server.Close)

	runner, err := NewGitHubRunner(&Service{}, GitHubRunnerConfig{
		AppID:         123,
		AppSlug:       "forge-metal",
		ClientID:      "client",
		ClientSecret:  "client-secret",
		PrivateKeyPEM: mustTestRSAPrivateKeyPEM(t),
		WebhookSecret: "secret",
		APIBaseURL:    server.URL,
		WebBaseURL:    "https://github.test",
	}, server.Client())
	if err != nil {
		t.Fatalf("NewGitHubRunner: %v", err)
	}
	_, err = runner.fetchInstallation(context.Background(), 42)
	if err != ErrGitHubInstallationInvalid {
		t.Fatalf("fetchInstallation error = %v, want ErrGitHubInstallationInvalid", err)
	}
}

func mustTestRSAPrivateKeyPEM(t *testing.T) string {
	t.Helper()
	return string(pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(mustTestRSAPrivateKey(t)),
	}))
}

func mustTestRSAPrivateKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	return key
}
