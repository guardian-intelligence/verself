package r2control

import (
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestPresignPutObjectScopesTemporaryCredentialToURL(t *testing.T) {
	client, err := NewR2Client(R2ClientConfig{
		Endpoint:        Endpoint("c3eaeffaadf7d4847684d4775c16d598"),
		Region:          "auto",
		AccessKeyID:     "access-key",
		SecretAccessKey: "secret-key",
		SessionToken:    "session-token",
	})
	if err != nil {
		t.Fatal(err)
	}
	signed, headers, err := client.PresignPutObject(t.Context(), "nomad-artifacts-gamma", "sha256/abc/service.tar", strings.Repeat("a", 64), 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(signed)
	if err != nil {
		t.Fatal(err)
	}
	if u.Query().Get("X-Amz-Security-Token") != "session-token" {
		t.Fatalf("missing session token in presigned URL: %s", signed)
	}
	if u.Query().Get("X-Amz-Expires") != "300" {
		t.Fatalf("expires = %q", u.Query().Get("X-Amz-Expires"))
	}
	if headers.Get("X-Amz-Meta-Sha256") == "" {
		t.Fatalf("required signed headers did not include checksum metadata: %#v", headers)
	}
	if strings.Contains(signed, "secret-key") {
		t.Fatal("presigned URL leaked the secret access key")
	}
}
