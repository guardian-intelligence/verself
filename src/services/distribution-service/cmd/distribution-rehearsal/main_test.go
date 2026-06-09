package main

import "testing"

func TestIdempotencyKeyIncludesPackageVersion(t *testing.T) {
	cfg := config{
		packageName:    "analytics-service",
		packageVersion: "commit-a",
		channelName:    "deployment",
		platformOS:     "linux",
		platformArch:   "amd64",
		flavor:         "gamma",
		ociDigest:      "sha256:186d41a15301fb372d01338b9c9ef2eed5f7424865588666887dbaed295e048b",
	}
	first := idempotencyKey("admit", cfg)
	if first != idempotencyKey("admit", cfg) {
		t.Fatalf("idempotency key is not stable")
	}
	cfg.packageVersion = "commit-b"
	if second := idempotencyKey("admit", cfg); second == first {
		t.Fatalf("idempotency key did not change across package versions: %q", first)
	}
}
