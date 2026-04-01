package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"
)

func TestFetchMMDSJobConfig(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest/api/token":
			if r.Method != http.MethodPut {
				t.Fatalf("token method: got %s want PUT", r.Method)
			}
			if got := r.Header.Get("X-metadata-token-ttl-seconds"); got != mmdsTokenTTLSeconds {
				t.Fatalf("token TTL header: got %q want %q", got, mmdsTokenTTLSeconds)
			}
			fmt.Fprint(w, "token-123")
		case "/forge_metal":
			if got := r.Header.Get("X-metadata-token"); got != "token-123" {
				t.Fatalf("metadata token: got %q want %q", got, "token-123")
			}
			if got := r.Header.Get("Accept"); got != "application/json" {
				t.Fatalf("accept header: got %q want %q", got, "application/json")
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"schema_version":1,"job":{"prepare_command":["npm","install"],"prepare_work_dir":"/workspace","run_command":["npm","test"],"run_work_dir":"/workspace/apps/web","services":["postgres"],"env":{"CI":"true"}}}`)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	cfg, err := fetchMMDSJobConfig(server.URL, server.Client())
	if err != nil {
		t.Fatalf("fetchMMDSJobConfig: %v", err)
	}

	if !reflect.DeepEqual(cfg.PrepareCommand, []string{"npm", "install"}) {
		t.Fatalf("prepare command: got %+v", cfg.PrepareCommand)
	}
	if cfg.RunWorkDir != "/workspace/apps/web" {
		t.Fatalf("run workdir: got %q", cfg.RunWorkDir)
	}
	if !reflect.DeepEqual(cfg.Services, []string{"postgres"}) {
		t.Fatalf("services: got %+v", cfg.Services)
	}
	if cfg.Env["CI"] != "true" {
		t.Fatalf("env CI: got %q", cfg.Env["CI"])
	}
}

func TestDecodeMMDSJobConfigRejectsUnsupportedSchema(t *testing.T) {
	t.Parallel()

	_, err := decodeMMDSJobConfig([]byte(`{"schema_version":2,"job":{"run_command":["npm","test"]}}`))
	if err == nil {
		t.Fatal("expected decodeMMDSJobConfig to fail")
	}
}

func TestFetchMMDSJobConfigRetriesTransientFailures(t *testing.T) {
	t.Parallel()

	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		switch r.URL.Path {
		case "/latest/api/token":
			if attempts < 2 {
				http.Error(w, "not ready", http.StatusServiceUnavailable)
				return
			}
			fmt.Fprint(w, "token-123")
		case "/forge_metal":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"schema_version":1,"job":{"run_command":["npm","test"],"run_work_dir":"/workspace"}}`)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	deadline := time.Now().Add(750 * time.Millisecond)
	var (
		cfg *jobConfig
		err error
	)
	for {
		cfg, err = fetchMMDSJobConfig(server.URL, &http.Client{Timeout: 100 * time.Millisecond})
		if err == nil || time.Now().After(deadline) {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("fetchMMDSJobConfig retry: %v", err)
	}
	if !reflect.DeepEqual(cfg.RunCommand, []string{"npm", "test"}) {
		t.Fatalf("run command: got %+v", cfg.RunCommand)
	}
}
