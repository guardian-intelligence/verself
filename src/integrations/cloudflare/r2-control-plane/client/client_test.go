package client

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClientCreateAndCompleteUploadSession(t *testing.T) {
	var sawBearer bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer test-token" {
			sawBearer = true
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
					Bucket: "nomad-artifacts-gamma",
					Key:    "sha256/abc/svc.tar",
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

	client, err := New(Config{Address: server.URL, Token: "test-token"})
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
	if !sawBearer {
		t.Fatal("client did not send bearer token")
	}
}
