package deployengine

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"go.opentelemetry.io/otel"

	r2controlplane "github.com/verself/integrations/cloudflare/r2-control-plane/client"

	"github.com/verself/deployment-service/internal/deploymodel"
)

func TestUploadArtifactStreamsLocalFile(t *testing.T) {
	path, digest, size := writeDeployEngineArtifactFixture(t, 3<<20)
	var uploadedBytes int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("method = %s, want PUT", r.Method)
		}
		if r.Header.Get("X-Test") != "stream" {
			t.Errorf("X-Test = %q, want stream", r.Header.Get("X-Test"))
		}
		if r.ContentLength != size {
			t.Errorf("content length = %d, want %d", r.ContentLength, size)
		}
		n, err := io.Copy(io.Discard, r.Body)
		if err != nil {
			t.Errorf("read upload body: %v", err)
		}
		atomic.StoreInt64(&uploadedBytes, n)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	err := uploadArtifact(context.Background(), testExecution(), uploadCandidate{
		Artifact: deploymodel.Artifact{
			Output:       "large-runtime",
			SHA256:       digest,
			Bucket:       "artifacts",
			Key:          "gamma/deploy/" + digest + "/large-runtime.tar",
			GetterSource: "https://artifacts.example.test/large-runtime.tar",
		},
		LocalPath: path,
		SizeBytes: size,
		Label:     "large runtime",
	}, r2controlplane.UploadObject{
		PutURL:  server.URL,
		Headers: map[string]string{"X-Test": "stream"},
	})
	if err != nil {
		t.Fatalf("upload artifact: %v", err)
	}
	if got := atomic.LoadInt64(&uploadedBytes); got != size {
		t.Fatalf("uploaded bytes = %d, want %d", got, size)
	}
}

func TestUploadArtifactRejectsDigestMismatchBeforePut(t *testing.T) {
	path, digest, size := writeDeployEngineArtifactFixture(t, 64<<10)
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	err := uploadArtifact(context.Background(), testExecution(), uploadCandidate{
		Artifact: deploymodel.Artifact{
			Output:       "large-runtime",
			SHA256:       strings.Repeat("0", len(digest)),
			Bucket:       "artifacts",
			Key:          "gamma/deploy/" + digest + "/large-runtime.tar",
			GetterSource: "https://artifacts.example.test/large-runtime.tar",
		},
		LocalPath: path,
		SizeBytes: size,
		Label:     "large runtime",
	}, r2controlplane.UploadObject{PutURL: server.URL})
	if err == nil || !strings.Contains(err.Error(), "does not match descriptor") {
		t.Fatalf("error = %v, want digest mismatch", err)
	}
	if got := atomic.LoadInt32(&calls); got != 0 {
		t.Fatalf("PUT calls = %d, want 0", got)
	}
}

func TestApplyCompletedArtifactSourcesBindsDownloadURLs(t *testing.T) {
	inputs := &deployInputs{
		Bindings: map[string]artifactBinding{
			"deployment-service": {
				Artifact: deploymodel.Artifact{
					Output: "deployment-service",
					Bucket: "verself-deployment-artifacts",
					Key:    "gamma/sha256/service.tar",
				},
			},
		},
		ControlPlaneObject: objectArtifact{
			Artifact: deploymodel.Artifact{
				Output: controlPlaneBundleOutput,
				Bucket: "verself-deployment-artifacts",
				Key:    "gamma/sha256/control-plane.tar",
			},
		},
	}
	err := applyCompletedArtifactSources(inputs, []r2controlplane.UploadObject{
		{
			Output:       "deployment-service",
			Bucket:       "verself-deployment-artifacts",
			Key:          "gamma/sha256/service.tar",
			GetterSource: "https://downloads.example.test/service.tar?X-Amz-Signature=abc",
		},
		{
			Output:       controlPlaneBundleOutput,
			Bucket:       "verself-deployment-artifacts",
			Key:          "gamma/sha256/control-plane.tar",
			GetterSource: "https://downloads.example.test/control-plane.tar?X-Amz-Signature=def",
		},
	})
	if err != nil {
		t.Fatalf("apply completed artifact sources: %v", err)
	}
	if got := inputs.Bindings["deployment-service"].Artifact.GetterSource; got != "https://downloads.example.test/service.tar?X-Amz-Signature=abc" {
		t.Fatalf("service download source = %q", got)
	}
	if got := inputs.ControlPlaneObject.Artifact.GetterSource; got != "https://downloads.example.test/control-plane.tar?X-Amz-Signature=def" {
		t.Fatalf("control-plane download source = %q", got)
	}
}

func testExecution() execution {
	return execution{Options: Options{Site: "gamma", Tracer: otel.Tracer("deployengine-test")}}
}

func writeDeployEngineArtifactFixture(t *testing.T, size int64) (string, string, int64) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "artifact.tar")
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create fixture: %v", err)
	}
	buf := make([]byte, 8192)
	for i := range buf {
		buf[i] = byte((i * 31) % 251)
	}
	remaining := size
	for remaining > 0 {
		chunk := int64(len(buf))
		if remaining < chunk {
			chunk = remaining
		}
		if _, err := file.Write(buf[:chunk]); err != nil {
			_ = file.Close()
			t.Fatalf("write fixture: %v", err)
		}
		remaining -= chunk
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close fixture: %v", err)
	}
	digest, err := fileSHA256(path)
	if err != nil {
		t.Fatalf("hash fixture: %v", err)
	}
	return path, digest, size
}
