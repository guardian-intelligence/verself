package e2e_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	rentaltestharness "github.com/forge-metal/sandbox-rental-service/testharness"
)

func TestImportRepoAPI_UnsupportedWorkflowLabel(t *testing.T) {
	repoPath := createWorkflowRepo(t, map[string]string{
		".github/workflows/ci.yml": `
name: ci
on:
  push:
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo nope
`,
	})

	repo := importRepoViaAPI(t, repoPath)
	if repo.State != "action_required" {
		t.Fatalf("repo state: got %q", repo.State)
	}
	if repo.CompatibilityStatus != "action_required" {
		t.Fatalf("compatibility_status: got %q", repo.CompatibilityStatus)
	}
	if !strings.Contains(string(repo.CompatibilitySummary), "ubuntu-latest") {
		t.Fatalf("compatibility_summary: got %s", repo.CompatibilitySummary)
	}
}

func TestImportRepoAPI_AllowsSameProviderRepoAcrossOrgs(t *testing.T) {
	repoPath := createWorkflowRepo(t, map[string]string{
		".github/workflows/ci.yml": `
name: ci
on:
  push:
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo nope
`,
	})

	ctx := context.Background()
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	pg := startPostgresForE2E(t)
	authProvider := newTestAuthProvider(t)
	defer authProvider.Close()

	rentalServer := rentaltestharness.NewServer(rentaltestharness.Config{
		PG:      pg.rentalDB,
		AuthCfg: authProvider.authConfig(testAudience),
		Logger:  logger,
	})
	defer rentalServer.Close()

	orgOneToken := authProvider.signToken(t, jwt.MapClaims{
		"iss":                                   authProvider.URL,
		"sub":                                   testUserID,
		"aud":                                   []string{testAudience},
		"exp":                                   time.Now().Add(time.Hour).Unix(),
		"urn:zitadel:iam:user:resourceowner:id": strconv.FormatUint(testOrgID, 10),
	})
	orgTwoToken := authProvider.signToken(t, jwt.MapClaims{
		"iss":                                   authProvider.URL,
		"sub":                                   "user-2",
		"aud":                                   []string{testAudience},
		"exp":                                   time.Now().Add(time.Hour).Unix(),
		"urn:zitadel:iam:user:resourceowner:id": strconv.FormatUint(testOrgID+1, 10),
	})

	first := importRepoViaAPIWithToken(t, ctx, rentalServer.URL, orgOneToken, repoPath)
	second := importRepoViaAPIWithToken(t, ctx, rentalServer.URL, orgTwoToken, repoPath)
	if first.RepoID == second.RepoID {
		t.Fatalf("expected distinct repo ids across orgs, got %s", first.RepoID)
	}
}

type importedRepoView struct {
	RepoID               string          `json:"repo_id"`
	State                string          `json:"state"`
	CompatibilityStatus  string          `json:"compatibility_status"`
	CompatibilitySummary json.RawMessage `json:"compatibility_summary"`
}

func importRepoViaAPI(t *testing.T, repoPath string) importedRepoView {
	t.Helper()

	ctx := context.Background()
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	pg := startPostgresForE2E(t)
	authProvider := newTestAuthProvider(t)
	defer authProvider.Close()

	rentalServer := rentaltestharness.NewServer(rentaltestharness.Config{
		PG:      pg.rentalDB,
		AuthCfg: authProvider.authConfig(testAudience),
		Logger:  logger,
	})
	defer rentalServer.Close()

	token := authProvider.signToken(t, jwt.MapClaims{
		"iss":                                   authProvider.URL,
		"sub":                                   testUserID,
		"aud":                                   []string{testAudience},
		"exp":                                   time.Now().Add(time.Hour).Unix(),
		"urn:zitadel:iam:user:resourceowner:id": strconv.FormatUint(testOrgID, 10),
	})

	return importRepoViaAPIWithToken(t, ctx, rentalServer.URL, token, repoPath)
}

func importRepoViaAPIWithToken(t *testing.T, ctx context.Context, baseURL, token, repoPath string) importedRepoView {
	t.Helper()

	body := map[string]any{
		"provider":         "forgejo",
		"provider_repo_id": "acme/example",
		"owner":            "acme",
		"name":             "example",
		"full_name":        "acme/example",
		"clone_url":        repoPath,
		"default_branch":   "main",
	}
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal import body: %v", err)
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/v1/repos", strings.NewReader(string(data)))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("import repo request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}

	var repo importedRepoView
	if err := json.NewDecoder(resp.Body).Decode(&repo); err != nil {
		t.Fatalf("decode import repo response: %v", err)
	}
	if repo.RepoID == "" {
		t.Fatal("expected repo_id")
	}
	return repo
}

type workflowRepoFixture struct {
	CloneURL string
	Head     string
}

func createWorkflowRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	return createWorkflowRepoFixture(t, files).CloneURL
}

func createWorkflowRepoFixture(t *testing.T, files map[string]string) workflowRepoFixture {
	t.Helper()

	root := t.TempDir()
	git := mustLookPath(t, "git")
	runCmd(t, exec.Command(git, "init", "--initial-branch=main", root))
	runCmd(t, exec.Command(git, "-C", root, "config", "user.name", "Forge Metal Repo Import"))
	runCmd(t, exec.Command(git, "-C", root, "config", "user.email", "repo-import@forge-metal.local"))

	for relPath, body := range files {
		absPath := filepath.Join(root, relPath)
		if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", absPath, err)
		}
		if err := os.WriteFile(absPath, []byte(strings.TrimSpace(body)+"\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", absPath, err)
		}
	}

	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# repo\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}

	runCmd(t, exec.Command(git, "-C", root, "add", "."))
	runCmd(t, exec.Command(git, "-C", root, "commit", "-m", "fixture"))
	out, err := exec.Command(git, "-C", root, "rev-parse", "HEAD").CombinedOutput()
	if err != nil {
		t.Fatalf("rev-parse HEAD: %s", strings.TrimSpace(string(out)))
	}
	return workflowRepoFixture{
		CloneURL: publicGitCloneURLForTestRepo(t, root, "acme/example.git"),
		Head:     strings.TrimSpace(string(out)),
	}
}
