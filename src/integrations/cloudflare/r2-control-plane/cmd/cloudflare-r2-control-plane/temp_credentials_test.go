package main

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

func TestMintLocalTemporaryCredentials(t *testing.T) {
	creds, err := mintLocalTemporaryCredentials(localTemporaryCredentialInput{
		AccountID:             "c3eaeffaadf7d4847684d4775c16d598",
		Endpoint:              "https://c3eaeffaadf7d4847684d4775c16d598.r2.cloudflarestorage.com",
		ParentAccessKeyID:     "parent-id",
		ParentSecretAccessKey: "parent-secret",
		Bucket:                "nomad-artifacts-gamma",
		Scope:                 "object-read-only",
		Actions:               []string{"GetObject", "HeadObject"},
		PrefixPaths:           []string{"sha256/"},
		TTL:                   15 * time.Minute,
		Now:                   time.Unix(1000, 0).UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if creds.AccessKeyID != "parent-id" {
		t.Fatalf("access key ID = %q", creds.AccessKeyID)
	}
	if len(creds.SecretAccessKey) != 64 {
		t.Fatalf("secret access key length = %d", len(creds.SecretAccessKey))
	}
	rawSession, err := base64.StdEncoding.DecodeString(creds.SessionToken)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(rawSession), "jwt/") {
		t.Fatalf("session token payload = %q", string(rawSession))
	}
}
