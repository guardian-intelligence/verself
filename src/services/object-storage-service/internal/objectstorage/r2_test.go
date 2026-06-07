package objectstorage

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestR2HealthUsesDeclaredBucket(t *testing.T) {
	var gotMethod, gotPath string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	provider, err := NewR2BucketProvider(server.URL, "access-key", "secret-key", "auto", "verself-deployment-artifacts", server.Client())
	if err != nil {
		t.Fatalf("NewR2BucketProvider: %v", err)
	}
	if err := provider.Health(context.Background()); err != nil {
		t.Fatalf("Health: %v", err)
	}
	if gotMethod != http.MethodHead {
		t.Fatalf("health method = %s, want HEAD", gotMethod)
	}
	if gotPath != "/verself-deployment-artifacts" {
		t.Fatalf("health path = %s, want /verself-deployment-artifacts", gotPath)
	}
}
