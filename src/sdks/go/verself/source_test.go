package verself

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSourceRepositoryCreateUsesBearerAndIdempotency(t *testing.T) {
	const projectID = "11111111-1111-1111-1111-111111111111"
	const repoID = "22222222-2222-2222-2222-222222222222"
	var authHeader string
	var idempotencyHeader string
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/repos" || r.Method != http.MethodPost {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		authHeader = r.Header.Get("Authorization")
		idempotencyHeader = r.Header.Get("Idempotency-Key")
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"repo_id":"` + repoID + `","org_id":"org_00000000000000000000000000","org_slug":"guardian","project_id":"` + projectID + `","project_slug":"api","name":"runner","description":"Builds","default_branch":"main","visibility":"private","state":"active","version":1,"backend":"forgejo","git_http_url":"https://git.example/guardian/api-runner.git","created_at":"2026-05-06T00:00:00Z","updated_at":"2026-05-06T00:00:00Z"}`))
	}))
	defer server.Close()

	client, err := New(Options{BearerToken: "tok_test", SourceURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	repo, err := client.Source.CreateRepository(context.Background(), CreateSourceRepositoryInput{
		ProjectID:      projectID,
		Description:    "Builds",
		DefaultBranch:  "main",
		IdempotencyKey: "source:test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if authHeader != "Bearer tok_test" {
		t.Fatalf("Authorization header = %q", authHeader)
	}
	if idempotencyHeader != "source:test" {
		t.Fatalf("idempotency header = %q", idempotencyHeader)
	}
	if body["project_id"] != projectID || body["description"] != "Builds" || body["default_branch"] != "main" {
		t.Fatalf("unexpected request body: %#v", body)
	}
	if repo.RepoID != repoID || repo.ProjectSlug != "api" || repo.Name != "runner" {
		t.Fatalf("unexpected repository: %#v", repo)
	}
}

func TestSourceReadOperationsParsePublicShapes(t *testing.T) {
	const projectID = "11111111-1111-1111-1111-111111111111"
	const repoID = "22222222-2222-2222-2222-222222222222"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos":
			if r.URL.Query().Get("project_id") != projectID {
				t.Fatalf("unexpected list query: %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"repositories":[{"repo_id":"` + repoID + `","org_id":"org_00000000000000000000000000","org_slug":"guardian","project_id":"` + projectID + `","project_slug":"api","name":"runner","description":"","default_branch":"main","visibility":"private","state":"active","version":1,"backend":"forgejo","git_http_url":"https://git.example/guardian/api-runner.git","created_at":"2026-05-06T00:00:00Z","updated_at":"2026-05-06T00:00:00Z"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/"+repoID+"/refs":
			_, _ = w.Write([]byte(`{"refs":[{"name":"refs/heads/main","commit":"abc123"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/"+repoID+"/tree":
			if r.URL.Query().Get("ref") != "main" || r.URL.Query().Get("path") != ".github" {
				t.Fatalf("unexpected tree query: %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"entries":[{"path":".github/workflows/build.yml","type":"blob","sha":"abc123","size":12}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/"+repoID+"/blob":
			if r.URL.Query().Get("path") != "README.md" {
				t.Fatalf("unexpected blob query: %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"name":"README.md","path":"README.md","sha":"abc123","size":4,"encoding":"base64","content":"SGk="}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := New(Options{BearerToken: "tok_test", SourceURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	repositories, err := client.Source.ListRepositories(context.Background(), ListSourceRepositoriesOptions{ProjectID: projectID})
	if err != nil {
		t.Fatal(err)
	}
	if len(repositories.Repositories) != 1 || repositories.Repositories[0].RepoID != repoID {
		t.Fatalf("unexpected repositories: %#v", repositories)
	}
	refs, err := client.Source.ListRefs(context.Background(), repoID)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs.Refs) != 1 || refs.Refs[0].Commit != "abc123" {
		t.Fatalf("unexpected refs: %#v", refs)
	}
	tree, err := client.Source.GetTree(context.Background(), GetSourceTreeOptions{RepoID: repoID, Ref: "main", Path: ".github"})
	if err != nil {
		t.Fatal(err)
	}
	if len(tree.Entries) != 1 || tree.Entries[0].Path != ".github/workflows/build.yml" {
		t.Fatalf("unexpected tree: %#v", tree)
	}
	blob, err := client.Source.GetBlob(context.Background(), GetSourceBlobOptions{RepoID: repoID, Path: "README.md"})
	if err != nil {
		t.Fatal(err)
	}
	if blob.Content != "SGk=" {
		t.Fatalf("unexpected blob: %#v", blob)
	}
}

func TestSourceMutationsUsePublicAPI(t *testing.T) {
	const projectID = "11111111-1111-1111-1111-111111111111"
	const repoID = "22222222-2222-2222-2222-222222222222"
	const workflowRunID = "33333333-3333-3333-3333-333333333333"
	var credentialKey string
	var credentialBody map[string]any
	var checkoutKey string
	var checkoutBody map[string]any
	var workflowKey string
	var workflowBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/git-credentials":
			credentialKey = r.Header.Get("Idempotency-Key")
			if err := json.NewDecoder(r.Body).Decode(&credentialBody); err != nil {
				t.Fatal(err)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"credential_id":"cred_1","org_id":"org_00000000000000000000000000","username":"x-access-token","token":"vsrc_test","token_prefix":"vsrc_","scopes":["repo:read"],"expires_at":"2026-05-06T01:00:00Z","created_at":"2026-05-06T00:00:00Z"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/repos/"+repoID+"/checkout-grants":
			checkoutKey = r.Header.Get("Idempotency-Key")
			if err := json.NewDecoder(r.Body).Decode(&checkoutBody); err != nil {
				t.Fatal(err)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"grant_id":"grant_1","repo_id":"` + repoID + `","ref":"main","token":"checkout_test","expires_at":"2026-05-06T00:15:00Z"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/repos/"+repoID+"/workflow-runs":
			workflowKey = r.Header.Get("Idempotency-Key")
			if err := json.NewDecoder(r.Body).Decode(&workflowBody); err != nil {
				t.Fatal(err)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"workflow_run_id":"` + workflowRunID + `","org_id":"org_00000000000000000000000000","project_id":"` + projectID + `","repo_id":"` + repoID + `","actor_id":"user_1","backend":"forgejo","workflow_path":".github/workflows/build.yml","ref":"main","inputs":{"target":"linux"},"state":"queued","created_at":"2026-05-06T00:00:00Z","updated_at":"2026-05-06T00:00:00Z"}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := New(Options{BearerToken: "tok_test", SourceURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	credential, err := client.Source.CreateGitCredential(context.Background(), CreateSourceGitCredentialInput{
		Scopes:         []string{"repo:read"},
		IdempotencyKey: "source:credential",
	})
	if err != nil {
		t.Fatal(err)
	}
	if credentialKey != "source:credential" || credential.Token != "vsrc_test" {
		t.Fatalf("unexpected credential key/response: %q %#v", credentialKey, credential)
	}
	scopes, ok := credentialBody["scopes"].([]any)
	if !ok || len(scopes) != 1 || scopes[0] != "repo:read" {
		t.Fatalf("unexpected credential body: %#v", credentialBody)
	}
	grant, err := client.Source.CreateCheckoutGrant(context.Background(), CreateSourceCheckoutGrantInput{
		RepoID:         repoID,
		Ref:            "main",
		IdempotencyKey: "source:checkout",
	})
	if err != nil {
		t.Fatal(err)
	}
	if checkoutKey != "source:checkout" || checkoutBody["ref"] != "main" || grant.Token != "checkout_test" {
		t.Fatalf("unexpected checkout mutation: %q %#v %#v", checkoutKey, checkoutBody, grant)
	}
	if _, ok := checkoutBody["path_prefix"]; ok {
		t.Fatalf("checkout body must not send path_prefix: %#v", checkoutBody)
	}
	run, err := client.Source.CreateWorkflowRun(context.Background(), CreateSourceWorkflowRunInput{
		RepoID:         repoID,
		ProjectID:      projectID,
		WorkflowPath:   ".github/workflows/build.yml",
		Ref:            "main",
		Inputs:         map[string]string{"target": "linux"},
		IdempotencyKey: "source:workflow",
	})
	if err != nil {
		t.Fatal(err)
	}
	if workflowKey != "source:workflow" || workflowBody["workflow_path"] != ".github/workflows/build.yml" || run.WorkflowRunID != workflowRunID {
		t.Fatalf("unexpected workflow mutation: %q %#v %#v", workflowKey, workflowBody, run)
	}
	inputs, ok := workflowBody["inputs"].(map[string]any)
	if !ok || inputs["target"] != "linux" {
		t.Fatalf("unexpected workflow inputs: %#v", workflowBody)
	}
}

func TestSourceErrorsNormalizeProblemDetails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"type":"urn:verself:problem:source:conflict","title":"Repository conflict","status":409,"detail":"Repository already exists."}`))
	}))
	defer server.Close()

	client, err := New(Options{BearerToken: "tok_test", SourceURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Source.CreateRepository(context.Background(), CreateSourceRepositoryInput{
		ProjectID: "11111111-1111-1111-1111-111111111111",
	})
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("error = %#v, want *APIError", err)
	}
	if apiErr.StatusCode != http.StatusConflict || apiErr.Title != "Repository conflict" || !strings.Contains(apiErr.Error(), "Repository already exists") {
		t.Fatalf("unexpected api error: %#v", apiErr)
	}
}
