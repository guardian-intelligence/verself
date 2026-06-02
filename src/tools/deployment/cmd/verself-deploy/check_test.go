package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDeploymentServiceReachableDoesNotSubmit(t *testing.T) {
	var posted bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			posted = true
		}
		if r.URL.Path != "/healthz" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte("ok\n"))
	}))
	t.Cleanup(srv.Close)

	if err := deploymentServiceReachable(context.Background(), srv.URL); err != nil {
		t.Fatal(err)
	}
	if posted {
		t.Fatal("health-only check submitted a deployment")
	}
}

func TestDeploymentServiceReachabilityErrorClassifiesDNS(t *testing.T) {
	err := deploymentServiceReachabilityError("https://deployments.api.gamma.verself.sh", fmt.Errorf("lookup failed: %w", &net.DNSError{
		Err:        "no such host",
		Name:       "deployments.api.gamma.verself.sh",
		IsNotFound: true,
	}))
	text := err.Error()
	for _, want := range []string{"deployment.bootstrap.s7.deployment_service_dns", "deployments.api.gamma.verself.sh", "normal aspect deploy does not SSH"} {
		if !strings.Contains(text, want) {
			t.Fatalf("error %q missing %q", text, want)
		}
	}
}

func TestBootstrapCheckIsReadOnly(t *testing.T) {
	seenBootstrap := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v1/deployments/bootstrap:check":
			seenBootstrap = true
			if r.Header.Get("Authorization") != "Bearer token" {
				t.Fatalf("authorization header was not propagated")
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ready":true,"failed_count":0,"checks":[{"stage":"S0","name":"site","ok":true,"code":"ok","detail":"ok"}]}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)

	if _, err := bootstrapCheck(context.Background(), srv.URL, "token"); err != nil {
		t.Fatal(err)
	}
	if !seenBootstrap {
		t.Fatal("bootstrap check was not called")
	}
}

func TestCheckRunsBootstrapReadinessByDefault(t *testing.T) {
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

	seen := map[string]int{}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen[r.Method+" "+r.URL.Path]++
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v1/deployments/bootstrap:check":
			if r.Header.Get("Authorization") != "Bearer deploy-token" {
				t.Fatalf("bootstrap check missing bearer token")
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ready":true,"failed_count":0,"checks":[{"stage":"S7","name":"deployment_service","ok":true,"code":"ok","detail":"ok"}]}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)
	restoreTransport := routeAllHTTPSToTestServer(t, srv)
	t.Cleanup(restoreTransport)

	if err := check(context.Background(), checkOptions{Site: "gamma", RepoRoot: repoRoot}); err != nil {
		t.Fatal(err)
	}
	if seen["GET /api/v1/deployments/bootstrap:check"] != 1 {
		t.Fatalf("bootstrap check count = %d, want 1; all requests = %#v", seen["GET /api/v1/deployments/bootstrap:check"], seen)
	}
	if len(seen) != 1 {
		t.Fatalf("unexpected request set: %#v", seen)
	}
}

func TestCheckRequiresDeploymentAuthBeforeReadinessRequest(t *testing.T) {
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
	t.Setenv("VERSELF_DEPLOY_BEARER_TOKEN", "")
	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_URL", "")
	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_TOKEN", "")

	requested := false
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = true
		t.Fatalf("unexpected request without auth %s %s", r.Method, r.URL.Path)
	}))
	t.Cleanup(srv.Close)
	restoreTransport := routeAllHTTPSToTestServer(t, srv)
	t.Cleanup(restoreTransport)

	err := check(context.Background(), checkOptions{Site: "gamma", RepoRoot: repoRoot})
	if err == nil || !strings.Contains(err.Error(), "deployment_auth_unavailable") {
		t.Fatalf("check error = %v, want deployment auth failure", err)
	}
	if requested {
		t.Fatal("check called deployment-service before deployment auth was available")
	}
}
