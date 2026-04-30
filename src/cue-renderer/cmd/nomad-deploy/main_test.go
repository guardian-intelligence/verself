package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestAwaitHealthyIgnoresStaleSuccessfulDeployment(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/job/sandbox-rental/deployment" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		jobModifyIndex := int64(99)
		if calls.Add(1) == 1 {
			jobModifyIndex = 98
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ID":             "deployment-id",
			"Status":         "successful",
			"JobModifyIndex": jobModifyIndex,
		})
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := awaitHealthyWithPoll(ctx, server.URL, "sandbox-rental", 99, time.Millisecond); err != nil {
		t.Fatalf("awaitHealthyWithPoll: %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("poll count: got %d want 2", got)
	}
}

func TestAwaitHealthyRejectsNewerDeployment(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ID":             "deployment-id",
			"Status":         "successful",
			"JobModifyIndex": int64(100),
		})
	}))
	defer server.Close()

	err := awaitHealthyWithPoll(context.Background(), server.URL, "sandbox-rental", 99, time.Millisecond)
	if err == nil {
		t.Fatal("expected newer-deployment error")
	}
}

func TestSpecMetaReadsResolvedJobMetadata(t *testing.T) {
	spec := map[string]any{
		"Job": map[string]any{
			"ID": "sandbox-rental",
			"Meta": map[string]any{
				"artifact_sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				"spec_sha256":     "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			},
		},
	}

	jobID, artifactDigest, specDigest, err := specMeta(spec)
	if err != nil {
		t.Fatalf("specMeta: %v", err)
	}
	if jobID != "sandbox-rental" {
		t.Fatalf("jobID: got %q", jobID)
	}
	if artifactDigest != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("artifactDigest: got %q", artifactDigest)
	}
	if specDigest != "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" {
		t.Fatalf("specDigest: got %q", specDigest)
	}
}

func TestSpecMetaRequiresBuildDigests(t *testing.T) {
	_, _, _, err := specMeta(map[string]any{
		"Job": map[string]any{"ID": "sandbox-rental", "Meta": map[string]any{}},
	})
	if err == nil {
		t.Fatal("expected missing digest error")
	}
}

func TestCurrentJobStateReadsStoppedFlag(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/job/billing" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"Meta": map[string]string{
				"artifact_sha256": "aaaaaaaa",
				"spec_sha256":     "bbbbbbbb",
			},
			"Version": int64(6),
			"Stop":    true,
		})
	}))
	defer server.Close()

	current, err := currentJobState(context.Background(), server.URL, "billing")
	if err != nil {
		t.Fatalf("currentJobState: %v", err)
	}
	if current.ArtifactDigest != "aaaaaaaa" || current.SpecDigest != "bbbbbbbb" {
		t.Fatalf("digests: got artifact=%q spec=%q", current.ArtifactDigest, current.SpecDigest)
	}
	if current.Version != 6 {
		t.Fatalf("version: got %d want 6", current.Version)
	}
	if !current.Stopped {
		t.Fatal("expected stopped flag")
	}
}
