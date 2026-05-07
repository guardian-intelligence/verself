package verself

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProjectsCreateUsesBearerAndIdempotency(t *testing.T) {
	var authHeader string
	var idempotencyHeader string
	var body map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/projects" || r.Method != http.MethodPost {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		authHeader = r.Header.Get("Authorization")
		idempotencyHeader = r.Header.Get("Idempotency-Key")
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"project_id":"11111111-1111-1111-1111-111111111111","org_id":"370200542594579812","slug":"api","display_name":"API","description":"Core API","state":"active","version":"1","created_by":"user","updated_by":"user","created_at":"2026-05-06T00:00:00Z","updated_at":"2026-05-06T00:00:00Z"}`))
	}))
	defer server.Close()

	client, err := New(Options{BearerToken: "tok_test", ProjectsURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	project, err := client.Projects.Create(context.Background(), CreateProjectInput{
		Slug:        "api",
		DisplayName: "API",
		Description: "Core API",
	})
	if err != nil {
		t.Fatal(err)
	}
	if authHeader != "Bearer tok_test" {
		t.Fatalf("Authorization header = %q", authHeader)
	}
	if idempotencyHeader == "" {
		t.Fatal("missing idempotency header")
	}
	if body["slug"] != "api" || body["display_name"] != "API" || body["description"] != "Core API" {
		t.Fatalf("unexpected request body: %#v", body)
	}
	if project.ProjectID != "11111111-1111-1111-1111-111111111111" || project.Slug != "api" {
		t.Fatalf("unexpected project: %#v", project)
	}
}

func TestProjectsListParsesPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != "active" || r.URL.Query().Get("limit") != "10" {
			t.Fatalf("unexpected query: %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"next_cursor":"cursor-1","projects":[{"project_id":"11111111-1111-1111-1111-111111111111","org_id":"370200542594579812","slug":"api","display_name":"API","description":"","state":"active","version":"1","created_by":"user","updated_by":"user","created_at":"2026-05-06T00:00:00Z","updated_at":"2026-05-06T00:00:00Z"}]}`))
	}))
	defer server.Close()

	client, err := New(Options{BearerToken: "tok_test", ProjectsURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	page, err := client.Projects.List(context.Background(), ListProjectsOptions{
		State: ProjectStateActive,
		Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if page.NextCursor != "cursor-1" || len(page.Projects) != 1 || page.Projects[0].Slug != "api" {
		t.Fatalf("unexpected page: %#v", page)
	}
}

func TestProjectsCreateNormalizesProblemDetails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/projects" || r.Method != http.MethodPost {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"type":"urn:verself:problem:projects:slug-conflict","title":"Project slug conflict","status":409,"detail":"Project slug is already in use."}`))
	}))
	defer server.Close()

	client, err := New(Options{BearerToken: "tok_test", ProjectsURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Projects.Create(context.Background(), CreateProjectInput{
		Slug:        "api",
		DisplayName: "API",
	})
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("error = %#v, want *APIError", err)
	}
	if apiErr.StatusCode != http.StatusConflict || apiErr.Title != "Project slug conflict" || apiErr.Detail != "Project slug is already in use." {
		t.Fatalf("unexpected api error: %#v", apiErr)
	}
}
