package verself

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	projectsinternalclient "github.com/verself/projects-service/internalclient"
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

func TestProjectsInternalCreateUsesOriginAndIdempotency(t *testing.T) {
	var authHeader string
	var orgHeader string
	var subjectHeader string
	var emailHeader string
	var idempotencyHeader string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/projects" || r.Method != http.MethodPost {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		authHeader = r.Header.Get("Authorization")
		orgHeader = r.Header.Get("X-Verself-Origin-Org-ID")
		subjectHeader = r.Header.Get("X-Verself-Origin-Subject")
		emailHeader = r.Header.Get("X-Verself-Origin-Email")
		idempotencyHeader = r.Header.Get("Idempotency-Key")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"project_id":"11111111-1111-1111-1111-111111111111","org_id":"370200542594579812","slug":"api","display_name":"API","description":"Core API","state":"active","version":"1","created_by":"user","updated_by":"user","created_at":"2026-05-06T00:00:00Z","updated_at":"2026-05-06T00:00:00Z"}`))
	}))
	defer server.Close()

	generated, err := projectsinternalclient.NewClientWithResponses(
		server.URL,
		projectsinternalclient.WithHTTPClient(server.Client()),
		projectsinternalclient.WithRequestEditorFn(internalRequestEditor("")),
	)
	if err != nil {
		t.Fatal(err)
	}
	client := &ProjectsClient{
		internal: generated,
		origin: Origin{
			OrgID:   370200542594579812,
			Subject: "user",
			Email:   "user@example.com",
		},
	}
	project, err := client.Create(context.Background(), CreateProjectInput{
		Slug:           "api",
		DisplayName:    "API",
		Description:    "Core API",
		IdempotencyKey: "project:test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if authHeader != "" {
		t.Fatalf("internal client must not send bearer Authorization, got %q", authHeader)
	}
	if orgHeader != "370200542594579812" || subjectHeader != "user" || emailHeader != "user@example.com" {
		t.Fatalf("unexpected origin headers org=%q subject=%q email=%q", orgHeader, subjectHeader, emailHeader)
	}
	if idempotencyHeader != "project:test" {
		t.Fatalf("Idempotency-Key = %q", idempotencyHeader)
	}
	if project.ProjectID != "11111111-1111-1111-1111-111111111111" || project.Slug != "api" {
		t.Fatalf("unexpected project: %#v", project)
	}
}
