package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGetDeploymentPreservesProblemCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/deployments/dep_missing" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"status":404,"code":"deployment.not_found","detail":"deployment not found"}`))
	}))
	t.Cleanup(srv.Close)

	_, _, err := getDeployment(context.Background(), srv.URL, "token", "dep_missing")
	if err == nil {
		t.Fatal("get deployment unexpectedly succeeded")
	}
	text := err.Error()
	for _, want := range []string{"deployment.not_found", "deployment not found"} {
		if !strings.Contains(text, want) {
			t.Fatalf("error %q missing %q", text, want)
		}
	}
}

func TestGetDeploymentEscapesDeploymentIDPathSegment(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/api/v1/deployments/dep%2Fwith%20space" {
			t.Fatalf("escaped path = %s, want encoded deployment id", r.URL.EscapedPath())
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"deployment":{"deployment_id":"dep/with space","state":"queued"},"traceparent":""}`))
	}))
	t.Cleanup(srv.Close)

	record, _, err := getDeployment(context.Background(), srv.URL, "token", "dep/with space")
	if err != nil {
		t.Fatal(err)
	}
	if record.DeploymentID != "dep/with space" {
		t.Fatalf("deployment id = %q, want unmodified id", record.DeploymentID)
	}
}

func TestListDeploymentEvents(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/deployments/dep_123/events" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"events":[{"event_id":1,"deployment_id":"dep_123","at":"2026-06-01T00:00:00Z","state":"queued","code":"deployment.queued","detail":""}],"traceparent":"00-00000000000000000000000000000001-0000000000000001-01"}`))
	}))
	t.Cleanup(srv.Close)

	events, traceparent, err := listDeploymentEvents(context.Background(), srv.URL, "token", "dep_123")
	if err != nil {
		t.Fatal(err)
	}
	if traceparent == "" {
		t.Fatal("traceparent was empty")
	}
	if len(events) != 1 || events[0].Code != "deployment.queued" {
		t.Fatalf("events = %#v", events)
	}
}
