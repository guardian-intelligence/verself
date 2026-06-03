package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/verself/integrations/cloudflare/r2-control-plane/internal/r2control"
)

func TestServeValidationAllowsSpiffeMTLSMode(t *testing.T) {
	err := config{
		action:             "serve",
		accountID:          strings.Repeat("a", 32),
		bucket:             "verself-deployment-artifacts",
		keyPrefix:          "sha256",
		region:             "auto",
		credentialSource:   r2control.ParentCredentialSourceFiles,
		tempTTL:            time.Minute,
		uploadSessionTTL:   time.Minute,
		downloadURLTTL:     time.Minute,
		inventoryDepth:     1,
	}.validate()
	if err != nil {
		t.Fatalf("validate serve mode: %v", err)
	}
}

func TestLoadBootstrapAuthTokenReadsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte("from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	token, err := loadBootstrapAuthToken(config{authTokenFile: path})
	if err != nil {
		t.Fatal(err)
	}
	if token != "from-file" {
		t.Fatalf("token = %q, want file value", token)
	}
}

func TestLoadBootstrapAuthTokenRejectsMissingToken(t *testing.T) {
	_, err := loadBootstrapAuthToken(config{})
	if err == nil || !strings.Contains(err.Error(), "auth token file is required") {
		t.Fatalf("error = %v, want missing token refusal", err)
	}
}

func TestUploadServerTemporaryR2ClientUsesLocalSigning(t *testing.T) {
	server := uploadServer{
		cfg: config{timeout: time.Second},
		siteCfg: siteArtifactConfig{
			AccountID: "c3eaeffaadf7d4847684d4775c16d598",
			Bucket:    "verself-deployment-artifacts",
			Region:    "auto",
		},
		publisher: r2control.ParentCredentials{
			AccessKeyID:     "publisher-token-id",
			SecretAccessKey: "publisher-secret",
		},
	}

	client, err := server.temporaryR2Client(context.Background(), r2control.TemporaryPermissionObjectReadOnly, []string{"gamma/sha256/abc/service.tar"}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if client == nil {
		t.Fatal("temporary client was nil")
	}
}
