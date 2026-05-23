package vmorchestrator

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestLoadSnapshotUsesPinnedFirecrackerSchema(t *testing.T) {
	var got map[string]any
	client := &apiClient{
		base: "http://localhost",
		client: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.Method != http.MethodPut {
				t.Fatalf("method = %s, want PUT", req.Method)
			}
			if req.URL.Path != "/snapshot/load" {
				t.Fatalf("path = %s, want /snapshot/load", req.URL.Path)
			}
			if err := json.NewDecoder(req.Body).Decode(&got); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			return &http.Response{
				StatusCode: http.StatusNoContent,
				Body:       http.NoBody,
				Header:     make(http.Header),
			}, nil
		})},
	}

	err := client.loadSnapshot(context.Background(), "/snapshots/state", "/snapshots/mem", false, []networkOverrideReq{{
		IfaceID:     "eth0",
		HostDevName: "fc-tap-a",
	}})
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	if _, ok := got["clock_realtime"]; ok {
		t.Fatal("snapshot load request included unsupported clock_realtime")
	}
	if got["snapshot_path"] != "/snapshots/state" {
		t.Fatalf("snapshot_path = %#v, want /snapshots/state", got["snapshot_path"])
	}
	memBackend, ok := got["mem_backend"].(map[string]any)
	if !ok {
		t.Fatalf("mem_backend = %#v, want object", got["mem_backend"])
	}
	if memBackend["backend_path"] != "/snapshots/mem" || memBackend["backend_type"] != "File" {
		t.Fatalf("mem_backend = %#v, want file backend", memBackend)
	}
	overrides, ok := got["network_overrides"].([]any)
	if !ok || len(overrides) != 1 {
		t.Fatalf("network_overrides = %#v, want one override", got["network_overrides"])
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
