package artifactpublish

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/verself/deployment-service/deployengine"
	objectstorageclient "github.com/verself/object-storage-service/client"
)

func TestPublisherUsesObjectStorageWriteSession(t *testing.T) {
	root := t.TempDir()
	body := []byte("artifact bytes")
	artifactPath := filepath.Join(root, "service.tar")
	if err := os.WriteFile(artifactPath, body, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	digest := hex.EncodeToString(sum[:])

	var srv *httptest.Server
	var uploaded []byte
	createSeen := false
	completeSeen := false
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "POST /internal/v1/object-storage/sites/gamma/write-sessions":
			createSeen = true
			if got := r.Header.Get("Idempotency-Key"); got != "deploy_12345678" {
				t.Fatalf("idempotency key = %q", got)
			}
			var request struct {
				Capability string `json:"capability"`
				Namespace  string `json:"namespace"`
				Objects    []struct {
					Name      string `json:"name"`
					SHA256    string `json:"sha256"`
					SizeBytes int64  `json:"size_bytes"`
				} `json:"objects"`
				TTLSeconds int `json:"ttl_seconds"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatal(err)
			}
			if request.Capability != "deployment_artifacts" || request.Namespace != "0123456789abcdef" || len(request.Objects) != 1 {
				t.Fatalf("create request = %+v", request)
			}
			writeJSON(t, w, http.StatusCreated, map[string]any{
				"session_id": "objws_test",
				"expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano),
				"objects": []map[string]any{{
					"name":   "service.tar",
					"key":    "deployment-artifacts/gamma/0123456789abcdef/sha256/" + digest + "/service.tar",
					"action": "put",
					"write": map[string]any{
						"method":     "PUT",
						"url":        srv.URL + "/upload/service.tar",
						"headers":    map[string]string{"X-Test-Header": "expected"},
						"expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano),
					},
				}},
			})
		case "PUT /upload/service.tar":
			if got := r.Header.Get("X-Test-Header"); got != "expected" {
				t.Fatalf("upload header = %q", got)
			}
			var err error
			uploaded, err = io.ReadAll(r.Body)
			if err != nil {
				t.Fatal(err)
			}
			w.WriteHeader(http.StatusNoContent)
		case "POST /internal/v1/object-storage/sites/gamma/write-sessions/objws_test/complete":
			completeSeen = true
			writeJSON(t, w, http.StatusOK, map[string]any{
				"session_id":   "objws_test",
				"completed_at": time.Now().UTC().Format(time.RFC3339Nano),
				"objects": []map[string]any{{
					"name":          "service.tar",
					"key":           "deployment-artifacts/gamma/0123456789abcdef/sha256/" + digest + "/service.tar",
					"action":        "put",
					"getter_source": srv.URL + "/download/service.tar",
				}},
			})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)

	client, err := objectstorageclient.NewClient(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	result, err := (Publisher{Client: client}).PublishDeploymentArtifacts(context.Background(), deployengine.ArtifactPublishRequest{
		Site:              "gamma",
		ArtifactNamespace: "0123456789abcdef",
		DeployRunKey:      "deploy_12345678",
		Artifacts: []deployengine.ArtifactPublishCandidate{{
			Output:    "service.tar",
			LocalPath: artifactPath,
			SHA256:    digest,
			SizeBytes: int64(len(body)),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !createSeen || !completeSeen {
		t.Fatalf("create=%v complete=%v", createSeen, completeSeen)
	}
	if string(uploaded) != string(body) {
		t.Fatalf("uploaded body = %q", string(uploaded))
	}
	if got := result.GetterSources["service.tar"]; got != srv.URL+"/download/service.tar" {
		t.Fatalf("getter source = %q", got)
	}
}

func TestPublisherRejectsDigestMismatchBeforeCreatingSession(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))
	t.Cleanup(srv.Close)
	client, err := objectstorageclient.NewClient(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	_, err = (Publisher{Client: client}).PublishDeploymentArtifacts(context.Background(), deployengine.ArtifactPublishRequest{
		Site:              "gamma",
		ArtifactNamespace: "sha",
		DeployRunKey:      "deploy_12345678",
		Artifacts: []deployengine.ArtifactPublishCandidate{{
			Output:    "service.tar",
			Body:      []byte("actual"),
			SHA256:    "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			SizeBytes: 6,
		}},
	})
	if err == nil {
		t.Fatal("expected digest mismatch")
	}
	if called {
		t.Fatal("publisher called object-storage after local digest mismatch")
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, status int, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatal(err)
	}
}
