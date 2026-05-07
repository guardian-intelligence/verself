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

func TestProjectsMutationsUseVersionAndIdempotency(t *testing.T) {
	const projectID = "11111111-1111-1111-1111-111111111111"
	var updateBody map[string]any
	var archiveBody map[string]any
	var restoreBody map[string]any
	var updateIDKey string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/projects/"+projectID:
			updateIDKey = r.Header.Get("Idempotency-Key")
			if err := json.NewDecoder(r.Body).Decode(&updateBody); err != nil {
				t.Fatal(err)
			}
			_, _ = w.Write([]byte(`{"project_id":"11111111-1111-1111-1111-111111111111","org_id":"370200542594579812","slug":"api-core","display_name":"API Core","description":"Core","state":"active","version":"2","created_by":"user","updated_by":"user","created_at":"2026-05-06T00:00:00Z","updated_at":"2026-05-06T00:00:00Z"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/projects/"+projectID+"/archive":
			if err := json.NewDecoder(r.Body).Decode(&archiveBody); err != nil {
				t.Fatal(err)
			}
			_, _ = w.Write([]byte(`{"project_id":"11111111-1111-1111-1111-111111111111","org_id":"370200542594579812","slug":"api-core","display_name":"API Core","description":"Core","state":"archived","version":"3","created_by":"user","updated_by":"user","created_at":"2026-05-06T00:00:00Z","updated_at":"2026-05-06T00:00:00Z"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/projects/"+projectID+"/restore":
			if err := json.NewDecoder(r.Body).Decode(&restoreBody); err != nil {
				t.Fatal(err)
			}
			_, _ = w.Write([]byte(`{"project_id":"11111111-1111-1111-1111-111111111111","org_id":"370200542594579812","slug":"api-core","display_name":"API Core","description":"Core","state":"active","version":"4","created_by":"user","updated_by":"user","created_at":"2026-05-06T00:00:00Z","updated_at":"2026-05-06T00:00:00Z"}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := New(Options{BearerToken: "tok_test", ProjectsURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	slug := "api-core"
	displayName := "API Core"
	description := "Core"
	project, err := client.Projects.Update(context.Background(), UpdateProjectInput{
		ProjectID:      projectID,
		Version:        "1",
		Slug:           &slug,
		DisplayName:    &displayName,
		Description:    &description,
		IdempotencyKey: "project:update",
	})
	if err != nil {
		t.Fatal(err)
	}
	if updateIDKey != "project:update" {
		t.Fatalf("update idempotency key = %q", updateIDKey)
	}
	if updateBody["version"] != "1" || updateBody["slug"] != "api-core" || project.Version != "2" {
		t.Fatalf("unexpected update body/project: %#v %#v", updateBody, project)
	}
	if _, err := client.Projects.Archive(context.Background(), ProjectLifecycleInput{
		ProjectID: projectID,
		Version:   "2",
	}); err != nil {
		t.Fatal(err)
	}
	if archiveBody["version"] != "2" {
		t.Fatalf("unexpected archive body: %#v", archiveBody)
	}
	if _, err := client.Projects.Restore(context.Background(), ProjectLifecycleInput{
		ProjectID: projectID,
		Version:   "3",
	}); err != nil {
		t.Fatal(err)
	}
	if restoreBody["version"] != "3" {
		t.Fatalf("unexpected restore body: %#v", restoreBody)
	}
}

func TestProjectEnvironmentsUsePublicAPI(t *testing.T) {
	const projectID = "11111111-1111-1111-1111-111111111111"
	const environmentID = "22222222-2222-2222-2222-222222222222"
	var createBody map[string]any
	var updateBody map[string]any
	var archiveBody map[string]any
	var createIDKey string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		environmentJSON := `{"environment_id":"22222222-2222-2222-2222-222222222222","project_id":"11111111-1111-1111-1111-111111111111","org_id":"370200542594579812","slug":"staging","display_name":"Staging","kind":"custom","state":"active","version":"1","created_by":"user","updated_by":"user","created_at":"2026-05-06T00:00:00Z","updated_at":"2026-05-06T00:00:00Z"}`
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/projects/"+projectID+"/environments":
			_, _ = w.Write([]byte(`{"environments":[` + environmentJSON + `]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/projects/"+projectID+"/environments":
			createIDKey = r.Header.Get("Idempotency-Key")
			if err := json.NewDecoder(r.Body).Decode(&createBody); err != nil {
				t.Fatal(err)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(environmentJSON))
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/projects/"+projectID+"/environments/"+environmentID:
			if err := json.NewDecoder(r.Body).Decode(&updateBody); err != nil {
				t.Fatal(err)
			}
			_, _ = w.Write([]byte(environmentJSON))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/projects/"+projectID+"/environments/"+environmentID+"/archive":
			if err := json.NewDecoder(r.Body).Decode(&archiveBody); err != nil {
				t.Fatal(err)
			}
			_, _ = w.Write([]byte(environmentJSON))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := New(Options{BearerToken: "tok_test", ProjectsURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	page, err := client.Projects.ListEnvironments(context.Background(), projectID)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Environments) != 1 || page.Environments[0].EnvironmentID != environmentID {
		t.Fatalf("unexpected environments: %#v", page)
	}
	environment, err := client.Projects.CreateEnvironment(context.Background(), CreateProjectEnvironmentInput{
		ProjectID:        projectID,
		Slug:             "staging",
		DisplayName:      "Staging",
		Kind:             ProjectEnvironmentKindCustom,
		ProtectionPolicy: map[string]string{"deploy": "manual"},
		IdempotencyKey:   "project-environment:create",
	})
	if err != nil {
		t.Fatal(err)
	}
	if createIDKey != "project-environment:create" || createBody["slug"] != "staging" || environment.EnvironmentID != environmentID {
		t.Fatalf("unexpected create request/environment: %q %#v %#v", createIDKey, createBody, environment)
	}
	policy, ok := createBody["protection_policy"].(map[string]any)
	if !ok || policy["deploy"] != "manual" {
		t.Fatalf("unexpected create policy: %#v", createBody)
	}
	displayName := "Staging 2"
	if _, err := client.Projects.UpdateEnvironment(context.Background(), UpdateProjectEnvironmentInput{
		ProjectID:        projectID,
		EnvironmentID:    environmentID,
		Version:          "1",
		DisplayName:      &displayName,
		ProtectionPolicy: map[string]string{"deploy": "automatic"},
	}); err != nil {
		t.Fatal(err)
	}
	if updateBody["version"] != "1" || updateBody["display_name"] != "Staging 2" {
		t.Fatalf("unexpected update body: %#v", updateBody)
	}
	if _, err := client.Projects.ArchiveEnvironment(context.Background(), ProjectEnvironmentLifecycleInput{
		ProjectID:     projectID,
		EnvironmentID: environmentID,
		Version:       "2",
	}); err != nil {
		t.Fatal(err)
	}
	if archiveBody["version"] != "2" {
		t.Fatalf("unexpected archive body: %#v", archiveBody)
	}
}
