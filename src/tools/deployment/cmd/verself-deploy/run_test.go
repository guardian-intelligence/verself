package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunSubmitsOnlyToDeploymentServiceHTTPBoundary(t *testing.T) {
	const sha = "0123456789abcdef0123456789abcdef01234567"
	repoRoot := t.TempDir()
	writeRunTestCloudflareAccountConfig(t, repoRoot)
	writeRunTestFile(t, repoRoot, "src/host/sites/gamma/vars.yml", `
verself_site: gamma
verself_domain: gamma.verself.test
company_domain: gamma.guardianintelligence.test
spire_trust_domain: gamma.verself.test
verself_installation_id: inst_gamma_test
deployment_service_domain: deployments.api.gamma.verself.test
`)
	t.Setenv("VERSELF_DEPLOY_BEARER_TOKEN", "deploy-token")

	for _, env := range []string{
		"BAO_ADDR",
		"BAO_TOKEN",
		"VAULT_ADDR",
		"VAULT_TOKEN",
		"CLOUDFLARE_R2_ADMIN_API_TOKEN",
		"CLOUDFLARE_R2_ADMIN_ACCESS_KEY_ID",
		"CLOUDFLARE_R2_ADMIN_SECRET_ACCESS_KEY",
		"VERSELF_NOMAD_ARTIFACTS_R2_ACCESS_KEY_ID",
		"VERSELF_NOMAD_ARTIFACTS_R2_SECRET_ACCESS_KEY",
	} {
		t.Setenv(env, "normal-deploy-must-not-read-"+env)
	}

	seen := map[string]int{}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen[r.Method+" "+r.URL.Path]++
		if r.Header.Get("Authorization") != "" && r.Header.Get("Authorization") != "Bearer deploy-token" {
			t.Fatalf("unexpected authorization header %q", r.Header.Get("Authorization"))
		}
		switch r.Method + " " + r.URL.Path {
		case "GET /healthz":
			_, _ = w.Write([]byte("ok\n"))
		case "GET /api/v1/deployments/bootstrap:check":
			if r.Header.Get("Authorization") != "Bearer deploy-token" {
				t.Fatalf("bootstrap check missing bearer token")
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ready":true,"failed_count":0,"checks":[{"stage":"S7","name":"deployment_service","ok":true,"code":"deployment.bootstrap.s7.deployment_service","detail":"ok"}]}`))
		case "POST /api/v1/deployments":
			if r.Header.Get("Authorization") != "Bearer deploy-token" {
				t.Fatalf("submit missing bearer token")
			}
			var body submitRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.Site != "gamma" || body.SHA != sha {
				t.Fatalf("submit body = %#v", body)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"deployment":{"deployment_id":"dep_test","state":"queued","sha":"` + sha + `"},"traceparent":"00-00000000000000000000000000000001-0000000000000001-01"}`))
		default:
			t.Fatalf("normal deploy made unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)
	restoreTransport := routeAllHTTPSToTestServer(t, srv)
	t.Cleanup(restoreTransport)

	if err := run(context.Background(), runOptions{Site: "gamma", SHA: sha, RepoRoot: repoRoot}); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"GET /healthz", "GET /api/v1/deployments/bootstrap:check", "POST /api/v1/deployments"} {
		if seen[key] != 1 {
			t.Fatalf("%s count = %d, want 1; all requests = %#v", key, seen[key], seen)
		}
	}
	if len(seen) != 3 {
		t.Fatalf("unexpected request set: %#v", seen)
	}
}

func TestBootstrapCheckPreservesProblemCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/deployments/bootstrap:check" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"status":503,"code":"deployment.bootstrap.s7.r2_control_plane","detail":"r2 control plane unavailable","traceparent":"00-00000000000000000000000000000001-0000000000000001-01"}`))
	}))
	t.Cleanup(srv.Close)

	_, err := bootstrapCheck(context.Background(), srv.URL, "token")
	if err == nil {
		t.Fatal("bootstrap check unexpectedly succeeded")
	}
	text := err.Error()
	for _, want := range []string{"deployment.bootstrap.s7.r2_control_plane", "r2 control plane unavailable", "traceparent=00-00000000000000000000000000000001-0000000000000001-01"} {
		if !strings.Contains(text, want) {
			t.Fatalf("error %q missing %q", text, want)
		}
	}
}

func routeAllHTTPSToTestServer(t *testing.T, srv *httptest.Server) func() {
	t.Helper()
	previousTransport := http.DefaultTransport
	dialer := &net.Dialer{}
	http.DefaultTransport = &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // Test-only transport for httptest TLS.
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, srv.Listener.Addr().String())
		},
	}
	return func() {
		http.DefaultTransport = previousTransport
	}
}

func writeRunTestFile(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeRunTestCloudflareAccountConfig(t *testing.T, root string) {
	t.Helper()
	writeRunTestFile(t, root, "src/integrations/cloudflare/account.json", `{
  "version": "verself.cloudflare.account.v1",
  "control_plane_site": "prod",
  "account_id": "0123456789abcdef0123456789abcdef",
  "r2": {
    "deployment_artifacts_bucket": "verself-deployment-artifacts",
    "recovery_bucket": "verself-recovery"
  }
}`)
}

func TestSubmitDeploymentPreservesAdmissionProblemCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/deployments" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"status":429,"code":"deployment.admission_busy","detail":"too many deployment submissions are being admitted"}`))
	}))
	t.Cleanup(srv.Close)

	_, err := submitDeployment(context.Background(), srv.URL, "token", submitRequest{
		Site: "gamma",
		SHA:  "0123456789abcdef0123456789abcdef01234567",
	})
	if err == nil {
		t.Fatal("deployment submission unexpectedly succeeded")
	}
	text := err.Error()
	for _, want := range []string{"deployment.admission_busy", "too many deployment submissions are being admitted"} {
		if !strings.Contains(text, want) {
			t.Fatalf("error %q missing %q", text, want)
		}
	}
}
