package ci

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Fixture struct {
	Name     string
	Path     string
	Manifest *Manifest
}

type E2EOptions struct {
	FixturesRoot string
	ForgejoURL   string
	Owner        string
	Token        string
	Username     string
	Email        string
}

type preparedFixture struct {
	Fixture Fixture
	RepoURL string
	PushURL string
}

type triggeredFixture struct {
	Prepared  preparedFixture
	CommitSHA string
}

func LoadFixtures(root string) ([]Fixture, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("read fixtures root %s: %w", root, err)
	}

	var fixtures []Fixture
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		fixturePath := filepath.Join(root, entry.Name())
		manifest, err := LoadManifest(fixturePath)
		if err != nil {
			return nil, fmt.Errorf("load fixture %s: %w", entry.Name(), err)
		}
		name := manifest.RepoName
		if name == "" {
			name = entry.Name()
		}
		fixtures = append(fixtures, Fixture{
			Name:     name,
			Path:     fixturePath,
			Manifest: manifest,
		})
	}
	if len(fixtures) == 0 {
		return nil, fmt.Errorf("no fixtures found in %s", root)
	}
	return fixtures, nil
}

func RunFixturesE2E(ctx context.Context, logger *slog.Logger, mgr *Manager, client *ForgejoClient, opts E2EOptions) error {
	if opts.Owner == "" {
		return fmt.Errorf("owner is required")
	}
	if opts.Username == "" {
		opts.Username = opts.Owner
	}
	if opts.Email == "" {
		opts.Email = "forge-metal-fixtures@local"
	}

	fixtures, err := LoadFixtures(opts.FixturesRoot)
	if err != nil {
		return err
	}
	runStamp := time.Now().UTC().Format("20060102-150405")
	runID := "fixtures-e2e-" + runStamp

	prepared := make([]preparedFixture, 0, len(fixtures))
	for i := range fixtures {
		manifestCopy := *fixtures[i].Manifest
		manifestCopy.PRBranch = uniquePRBranch(manifestCopy.PRBranch, fixtures[i].Name, runStamp)
		fixtures[i].Manifest = &manifestCopy
		fixture := fixtures[i]

		repo := Repository{
			Name:        fixture.Name,
			Description: fixture.Manifest.Description,
			Private:     false,
		}
		if err := client.EnsureRepository(ctx, opts.Owner, repo); err != nil {
			return err
		}
		repoURL := forgejoRepoURL(opts.ForgejoURL, opts.Owner, fixture.Name)
		pushURL := forgejoAuthenticatedPushURL(opts.ForgejoURL, opts.Username, opts.Token, opts.Owner, fixture.Name)

		logger.Info("seeding fixture main branch", "repo", fixture.Name, "run_id", runID)
		if err := pushFixtureMain(fixture, pushURL, opts.Username, opts.Email); err != nil {
			return err
		}

		logger.Info("warming repo golden", "repo", fixture.Name, "run_id", runID)
		if err := mgr.Warm(ctx, WarmRequest{
			Repo:          fmt.Sprintf("%s/%s", opts.Owner, fixture.Name),
			RepoURL:       repoURL,
			DefaultBranch: fixture.Manifest.DefaultBranch,
		}); err != nil {
			return err
		}

		logger.Info("installing workflow on main", "repo", fixture.Name, "run_id", runID)
		if err := addWorkflowToMain(opts, fixture, pushURL, runID); err != nil {
			return err
		}

		prepared = append(prepared, preparedFixture{
			Fixture: fixture,
			RepoURL: repoURL,
			PushURL: pushURL,
		})
	}

	witness := startLeaseWitness(mgr.firecrackerConfig.NetworkLeaseDir)
	defer func() {
		logLeaseWitnessSummary(logger, runID, witness.Stop())
	}()

	triggered := make([]triggeredFixture, 0, len(prepared))
	for _, item := range prepared {
		logger.Info("creating PR trigger branch", "repo", item.Fixture.Name, "run_id", runID)
		commitSHA, err := createTriggerPR(ctx, client, opts, item.Fixture, item.PushURL, item.RepoURL)
		if err != nil {
			return err
		}
		triggered = append(triggered, triggeredFixture{
			Prepared:  item,
			CommitSHA: commitSHA,
		})
	}

	for _, item := range triggered {
		logger.Info("waiting for CI run", "repo", item.Prepared.Fixture.Name, "run_id", runID)
		if err := waitForCommitRun(ctx, client, opts.Owner, item.Prepared.Fixture.Name, item.CommitSHA); err != nil {
			return err
		}
	}

	return nil
}

func pushFixtureMain(fixture Fixture, pushURL, username, email string) error {
	tmp, err := os.MkdirTemp("", "forge-metal-fixture-main-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	if err := copyTree(fixture.Path, tmp); err != nil {
		return err
	}
	if err := os.RemoveAll(filepath.Join(tmp, ".forgejo")); err != nil {
		return err
	}

	return initializeAndPushRepo(tmp, pushURL, username, email, fmt.Sprintf("feat: seed %s fixture", fixture.Name), "main")
}

func addWorkflowToMain(opts E2EOptions, fixture Fixture, pushURL, runID string) error {
	tmp, err := os.MkdirTemp("", "forge-metal-fixture-workflow-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	if err := runGit("", nil, "clone", "--depth", "1", pushURL, tmp); err != nil {
		return err
	}
	workflowPath := filepath.Join(tmp, ".forgejo", "workflows", "ci.yml")
	if err := writeFile(workflowPath, fixtureWorkflow(opts.ForgejoURL, runID), 0o644); err != nil {
		return err
	}
	if err := commitAll(tmp, opts.Username, opts.Email, "ci: add forge-metal workflow"); err != nil {
		return err
	}
	return runGit(tmp, []string{"GIT_TERMINAL_PROMPT=0"}, "push", "origin", "HEAD:main")
}

func createTriggerPR(ctx context.Context, client *ForgejoClient, opts E2EOptions, fixture Fixture, pushURL, repoURL string) (string, error) {
	tmp, err := os.MkdirTemp("", "forge-metal-fixture-pr-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)

	if err := runGit("", nil, "clone", "--depth", "1", pushURL, tmp); err != nil {
		return "", err
	}
	if err := runGit(tmp, nil, "checkout", "-b", fixture.Manifest.PRBranch); err != nil {
		return "", err
	}

	changePath := filepath.Join(tmp, fixture.Manifest.PRChangePath)
	data, err := os.ReadFile(changePath)
	if err != nil {
		return "", err
	}
	updated := strings.Replace(string(data), fixture.Manifest.PRChangeFind, fixture.Manifest.PRChangeReplace, 1)
	if updated == string(data) {
		return "", fmt.Errorf("fixture %s: pr_change_find not found in %s", fixture.Name, fixture.Manifest.PRChangePath)
	}
	if err := os.WriteFile(changePath, []byte(updated), 0o644); err != nil {
		return "", err
	}

	if err := commitAll(tmp, opts.Username, opts.Email, fixture.Manifest.PRCommitMessage); err != nil {
		return "", err
	}
	commitSHA, err := gitHeadSHA(tmp)
	if err != nil {
		return "", err
	}
	if err := runGit(tmp, []string{"GIT_TERMINAL_PROMPT=0"}, "push", "--force", "origin", "HEAD:"+fixture.Manifest.PRBranch); err != nil {
		return "", err
	}
	_, err = client.CreatePullRequest(ctx, opts.Owner, fixture.Name, fixture.Manifest.PRTitle, fixture.Manifest.PRBranch, fixture.Manifest.DefaultBranch)
	if err != nil {
		return "", err
	}
	return commitSHA, nil
}

func waitForCommitRun(ctx context.Context, client *ForgejoClient, owner, repo, commitSHA string) error {
	deadline := time.Now().Add(10 * time.Minute)
	for {
		runs, err := client.ListWorkflowRuns(ctx, owner, repo)
		if err != nil {
			return err
		}
		for _, run := range runs {
			if run.CommitSHA != commitSHA {
				continue
			}
			if run.Status == "success" || run.Conclusion == "success" {
				return nil
			}
			if run.Status == "failure" || run.Conclusion == "failure" {
				return fmt.Errorf("workflow run %d for %s/%s failed", run.ID, owner, repo)
			}
			if run.Status == "completed" && run.Conclusion != "" && run.Conclusion != "success" {
				return fmt.Errorf("workflow run %d for %s/%s completed with %s", run.ID, owner, repo, run.Conclusion)
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for workflow run on %s/%s commit %s", owner, repo, commitSHA)
		}
		time.Sleep(15 * time.Second)
	}
}

func uniquePRBranch(base, repoName, stamp string) string {
	base = strings.TrimSpace(base)
	if base == "" {
		base = "test/forge-metal-warm-path"
	}
	return fmt.Sprintf("%s-%s-%s", strings.TrimRight(base, "/"), sanitizeRepoKey(repoName), stamp)
}

func fixtureWorkflow(forgejoURL, runID string) string {
	return fmt.Sprintf(`name: CI
on:
  pull_request:
jobs:
  ci:
    runs-on: ubuntu-latest
    steps:
      - shell: bash
        run: sudo /opt/forge-metal/profile/bin/forge-metal ci exec --run-id %s --forgejo-url %s --repo "${{ github.repository }}" --ref "${{ github.ref }}"
`, shellQuote(runID), shellQuote(forgejoURL))
}

func logLeaseWitnessSummary(logger *slog.Logger, runID string, summary leaseWitnessSummary) {
	if logger == nil {
		return
	}
	attrs := []any{
		"run_id", runID,
		"max_active_leases", summary.MaxActiveLeases,
		"distinct_job_ids", len(summary.DistinctJobIDs),
		"samples", summary.Samples,
		"read_errors", summary.ReadErrors,
	}
	if !summary.FirstOverlapAt.IsZero() {
		attrs = append(attrs, "first_overlap_at", summary.FirstOverlapAt.Format(time.RFC3339Nano))
	}
	if summary.MaxActiveLeases >= 2 {
		logger.Info("fixture parallelism observed", attrs...)
		return
	}
	logger.Warn("fixture parallelism not observed", attrs...)
}

func forgejoRepoURL(baseURL, owner, repo string) string {
	return strings.TrimRight(baseURL, "/") + "/" + owner + "/" + repo + ".git"
}

func forgejoAuthenticatedPushURL(baseURL, username, token, owner, repo string) string {
	trimmed := strings.TrimRight(baseURL, "/")
	parts := strings.SplitN(trimmed, "://", 2)
	if len(parts) != 2 {
		return trimmed + "/" + owner + "/" + repo + ".git"
	}
	return parts[0] + "://" + username + ":" + token + "@" + parts[1] + "/" + owner + "/" + repo + ".git"
}

func initializeAndPushRepo(dir, pushURL, username, email, message, branch string) error {
	if err := runGit("", nil, "init", "-b", branch, dir); err != nil {
		return err
	}
	if err := runGit(dir, nil, "config", "user.name", username); err != nil {
		return err
	}
	if err := runGit(dir, nil, "config", "user.email", email); err != nil {
		return err
	}
	if err := runGit(dir, nil, "remote", "add", "origin", pushURL); err != nil {
		return err
	}
	return commitAndPush(dir, message, branch)
}

func commitAll(dir, username, email, message string) error {
	if err := runGit(dir, nil, "config", "user.name", username); err != nil {
		return err
	}
	if err := runGit(dir, nil, "config", "user.email", email); err != nil {
		return err
	}
	return commitAndPush(dir, message, "")
}

func commitAndPush(dir, message, branch string) error {
	if err := runGit(dir, nil, "add", "-A"); err != nil {
		return err
	}
	if err := runGit(dir, nil, "commit", "-m", message); err != nil {
		if strings.Contains(err.Error(), "nothing to commit") {
			return nil
		}
		return err
	}
	if branch == "" {
		return nil
	}
	return runGit(dir, []string{"GIT_TERMINAL_PROMPT=0"}, "push", "--force", "origin", "HEAD:"+branch)
}
