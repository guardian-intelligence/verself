package client

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestClientCreateAndCompleteUploadSession(t *testing.T) {
	var sawTrace bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Test-Trace") == "r2-client" {
			sawTrace = true
		}
		switch r.URL.Path {
		case "/v1/sites/gamma/artifact-upload-sessions":
			var req CreateUploadSessionRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode create request: %v", err)
			}
			if req.Site != "gamma" || req.DeployRunKey != "run-1" || len(req.Artifacts) != 1 {
				t.Fatalf("unexpected create request: %+v", req)
			}
			_ = json.NewEncoder(w).Encode(CreateUploadSessionResponse{
				SessionID: "session-1",
				ExpiresAt: time.Unix(10, 0).UTC(),
				Objects: []UploadObject{{
					Output: "svc",
					Bucket: "verself-deployment-artifacts",
					Key:    "gamma/sha256/abc/svc.tar",
					Action: UploadActionPut,
				}},
			})
		case "/v1/sites/gamma/artifact-upload-sessions/session-1/complete":
			_ = json.NewEncoder(w).Encode(CompleteUploadSessionResponse{
				SessionID:   "session-1",
				Site:        "gamma",
				CompletedAt: time.Unix(11, 0).UTC(),
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	httpClient := server.Client()
	httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		req.Header.Set("X-Test-Trace", "r2-client")
		return http.DefaultTransport.RoundTrip(req)
	})
	client, err := New(Config{Address: server.URL, HTTPClient: httpClient})
	if err != nil {
		t.Fatal(err)
	}
	created, err := client.CreateUploadSession(t.Context(), CreateUploadSessionRequest{
		Site:         "gamma",
		DeployRunKey: "run-1",
		SHA:          "abc",
		Artifacts: []ArtifactUpload{{
			Output:    "svc",
			SHA256:    "abc",
			SizeBytes: 1,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.SessionID != "session-1" {
		t.Fatalf("session id = %q", created.SessionID)
	}
	completed, err := client.CompleteUploadSession(t.Context(), "gamma", created.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Site != "gamma" {
		t.Fatalf("completed site = %q", completed.Site)
	}
	if !sawTrace {
		t.Fatal("client did not use configured transport")
	}
}

func TestClientBootstrapBearerToken(t *testing.T) {
	var got string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(CreateUploadSessionResponse{SessionID: "session-1"})
	}))
	defer server.Close()

	client, err := New(Config{Address: server.URL, BootstrapBearerToken: "bootstrap-token"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.CreateUploadSession(t.Context(), CreateUploadSessionRequest{
		Site:         "gamma",
		DeployRunKey: "run-1",
		SHA:          strings.Repeat("a", 40),
		Artifacts:    []ArtifactUpload{{Output: "svc", SHA256: strings.Repeat("b", 64), SizeBytes: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != "Bearer bootstrap-token" {
		t.Fatalf("authorization = %q", got)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}
