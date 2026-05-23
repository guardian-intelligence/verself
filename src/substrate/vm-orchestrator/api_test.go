package vmorchestrator

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestLoadSnapshotEnablesClockRealtime(t *testing.T) {
	var got snapshotLoadReq
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
	if !got.ClockRealtime {
		t.Fatal("clock_realtime was false")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestSnapshotLoadReqJSONName(t *testing.T) {
	data, err := json.Marshal(snapshotLoadReq{ClockRealtime: true})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(data), `"clock_realtime":true`) {
		t.Fatalf("snapshot load json = %s, want clock_realtime", data)
	}
}
