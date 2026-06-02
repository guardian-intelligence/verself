package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/verself/integrations/cloudflare/control-plane/internal/r2control"
)

func TestWriteBootstrapPublisherEnvFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "site-bootstrap", "gamma", "r2-publisher.env")
	publisher := r2control.CreatedAPIToken{
		ID:    "token-id",
		Value: "publisher-token",
	}

	if err := writeBootstrapPublisherEnvFile(path, publisher); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o, want 0600", info.Mode().Perm())
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	values, err := r2control.ParseEnvFile(body)
	if err != nil {
		t.Fatal(err)
	}
	wantSecret := r2control.SHA256Hex([]byte("publisher-token"))
	if values["CLOUDFLARE_R2_PUBLISHER_TOKEN_ID"] != "token-id" ||
		values["CLOUDFLARE_R2_PUBLISHER_API_TOKEN"] != "publisher-token" ||
		values["CLOUDFLARE_R2_PUBLISHER_SECRET_ACCESS_KEY"] != wantSecret {
		t.Fatalf("unexpected env values: %#v", values)
	}
}
