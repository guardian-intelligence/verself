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
	Metadata FixtureMetadata
}

type FixtureSuite string

const (
	FixtureSuitePass FixtureSuite = "pass"
	FixtureSuiteFail FixtureSuite = "fail"
)

type FixtureMetadata struct {
	Suite           FixtureSuite
	ExpectedResult  string
	Description     string
	DefaultBranch   string
	PRBranchBase    string
	PRTitle         string
	PRCommitMessage string
	PRChangePath    string
	PRChangeFind    string
	PRChangeReplace string
}

var fixtureMetadataByName = map[string]FixtureMetadata{
	"next-bun-monorepo": {
		Suite:           FixtureSuitePass,
		ExpectedResult:  "success",
		Description:     "bun workspace Next.js fixture without external services",
		DefaultBranch:   "main",
		PRBranchBase:    "test/forge-metal-warm-path",
		PRTitle:         "test: trigger forge-metal warm path",
		PRCommitMessage: "test: trigger forge-metal warm path",
		PRChangePath:    "apps/web/app/page.js",
		PRChangeFind:    "Hello from Bun main",
		PRChangeReplace: "Hello from Bun warm PR",
	},
	"next-npm-single-app": {
		Suite:           FixtureSuitePass,
		ExpectedResult:  "success",
		Description:     "single-package npm Next.js fixture with a multi-step CI script",
		DefaultBranch:   "main",
		PRBranchBase:    "test/forge-metal-warm-path",
		PRTitle:         "test: trigger forge-metal warm path",
		PRCommitMessage: "test: trigger forge-metal warm path",
		PRChangePath:    "app/page.js",
		PRChangeFind:    "Hello from npm single main",
		PRChangeReplace: "Hello from npm single warm PR",
	},
	"next-npm-workspaces": {
		Suite:           FixtureSuitePass,
		ExpectedResult:  "success",
		Description:     "npm workspaces monorepo fixture with a root-level CI entrypoint",
		DefaultBranch:   "main",
		PRBranchBase:    "test/forge-metal-warm-path",
		PRTitle:         "test: trigger forge-metal warm path",
		PRCommitMessage: "test: trigger forge-metal warm path",
		PRChangePath:    "apps/dashboard/app/page.js",
		PRChangeFind:    "Hello from npm workspace main",
		PRChangeReplace: "Hello from npm workspace warm PR",
	},
	"next-pnpm-postgres": {
		Suite:           FixtureSuitePass,
		ExpectedResult:  "success",
		Description:     "pnpm + Turborepo Next.js fixture with a Postgres service requirement",
		DefaultBranch:   "main",
		PRBranchBase:    "test/forge-metal-warm-path",
		PRTitle:         "test: trigger forge-metal warm path",
		PRCommitMessage: "test: trigger forge-metal warm path",
		PRChangePath:    "apps/web/app/page.js",
		PRChangeFind:    "Hello from main",
		PRChangeReplace: "Hello from warm PR",
	},
}

type FixtureRunOptions struct {
	FixturesRoot string
	Suites       []string
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
		metadata, err := lookupFixtureMetadata(entry.Name())
		if err != nil {
			return nil, err
		}
		fixtures = append(fixtures, Fixture{
			Name:     entry.Name(),
			Path:     fixturePath,
			Manifest: manifest,
			Metadata: metadata,
		})
	}
	if len(fixtures) == 0 {
		return nil, fmt.Errorf("no fixtures found in %s", root)
	}
	return fixtures, nil
}

func lookupFixtureMetadata(name string) (FixtureMetadata, error) {
	metadata, ok := fixtureMetadataByName[name]
	if !ok {
		return FixtureMetadata{}, fmt.Errorf("fixture %s is missing metadata", name)
	}
	return metadata, nil
}

func RunFixtureSuites(ctx context.Context, logger *slog.Logger, mgr *Manager, client *ForgejoClient, opts FixtureRunOptions) error {
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
	fixtures, suiteNames, err := selectFixturesBySuite(fixtures, opts.Suites)
	if err != nil {
		return err
	}
	runStamp := time.Now().UTC().Format("20060102-150405")
	runID := "fixtures-" + strings.Join(suiteNames, "-") + "-" + runStamp

	prepared := make([]preparedFixture, 0, len(fixtures))
	for i := range fixtures {
		metadataCopy := fixtures[i].Metadata
		metadataCopy.PRBranchBase = uniquePRBranch(metadataCopy.PRBranchBase, fixtures[i].Name, runStamp)
		fixtures[i].Metadata = metadataCopy
		fixture := fixtures[i]

		repo := Repository{
			Name:        fixture.Name,
			Description: fixture.Metadata.Description,
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
			DefaultBranch: fixture.Metadata.DefaultBranch,
			RunID:         runID,
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
		if err := waitForCommitRun(ctx, client, opts.Owner, item.Prepared.Fixture.Name, item.CommitSHA, item.Prepared.Fixture.Metadata.ExpectedResult); err != nil {
			return err
		}
	}

	summary := witness.Stop()
	logLeaseWitnessSummary(logger, runID, summary)
	if err := validateLeaseWitnessSummary(summary, len(triggered)); err != nil {
		return fmt.Errorf("fixture runtime overlap check failed: %w", err)
	}

	return nil
}

func selectFixturesBySuite(fixtures []Fixture, suites []string) ([]Fixture, []string, error) {
	if len(fixtures) == 0 {
		return nil, nil, fmt.Errorf("fixtures list must not be empty")
	}

	if len(suites) == 0 {
		suites = []string{string(FixtureSuitePass)}
	}

	normalizedSuites := make([]string, 0, len(suites))
	allowedSuites := make(map[FixtureSuite]struct{}, len(suites))
	for _, suiteName := range suites {
		suiteName = strings.ToLower(strings.TrimSpace(suiteName))
		if suiteName == "" {
			continue
		}
		suite := FixtureSuite(suiteName)
		switch suite {
		case FixtureSuitePass, FixtureSuiteFail:
		default:
			return nil, nil, fmt.Errorf("unknown fixture suite %q", suiteName)
		}
		if _, exists := allowedSuites[suite]; exists {
			continue
		}
		allowedSuites[suite] = struct{}{}
		normalizedSuites = append(normalizedSuites, suiteName)
	}

	if len(normalizedSuites) == 0 {
		normalizedSuites = []string{string(FixtureSuitePass)}
		allowedSuites[FixtureSuitePass] = struct{}{}
	}

	selected := make([]Fixture, 0, len(fixtures))
	for _, fixture := range fixtures {
		if _, ok := allowedSuites[fixture.Metadata.Suite]; ok {
			selected = append(selected, fixture)
		}
	}
	if len(selected) == 0 {
		return nil, nil, fmt.Errorf("no fixtures matched suites %s", strings.Join(normalizedSuites, ", "))
	}
	return selected, normalizedSuites, nil
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

	return initializeAndPushRepo(tmp, pushURL, username, email, fmt.Sprintf("feat: seed %s fixture", fixture.Name), fixture.Metadata.DefaultBranch)
}

func addWorkflowToMain(opts FixtureRunOptions, fixture Fixture, pushURL, runID string) error {
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
	return runGit(tmp, []string{"GIT_TERMINAL_PROMPT=0"}, "push", "origin", "HEAD:"+fixture.Metadata.DefaultBranch)
}

func createTriggerPR(ctx context.Context, client *ForgejoClient, opts FixtureRunOptions, fixture Fixture, pushURL, repoURL string) (string, error) {
	tmp, err := os.MkdirTemp("", "forge-metal-fixture-pr-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)

	if err := runGit("", nil, "clone", "--depth", "1", pushURL, tmp); err != nil {
		return "", err
	}
	prBranch := fixture.Metadata.PRBranchBase
	if err := runGit(tmp, nil, "checkout", "-b", prBranch); err != nil {
		return "", err
	}

	changePath := filepath.Join(tmp, fixture.Metadata.PRChangePath)
	data, err := os.ReadFile(changePath)
	if err != nil {
		return "", err
	}
	updated := strings.Replace(string(data), fixture.Metadata.PRChangeFind, fixture.Metadata.PRChangeReplace, 1)
	if updated == string(data) {
		return "", fmt.Errorf("fixture %s: pr_change_find not found in %s", fixture.Name, fixture.Metadata.PRChangePath)
	}
	if err := os.WriteFile(changePath, []byte(updated), 0o644); err != nil {
		return "", err
	}

	if err := commitAll(tmp, opts.Username, opts.Email, fixture.Metadata.PRCommitMessage); err != nil {
		return "", err
	}
	commitSHA, err := gitHeadSHA(tmp)
	if err != nil {
		return "", err
	}
	if err := runGit(tmp, []string{"GIT_TERMINAL_PROMPT=0"}, "push", "--force", "origin", "HEAD:"+prBranch); err != nil {
		return "", err
	}
	_, err = client.CreatePullRequest(ctx, opts.Owner, fixture.Name, fixture.Metadata.PRTitle, prBranch, fixture.Metadata.DefaultBranch)
	if err != nil {
		return "", err
	}
	return commitSHA, nil
}

func waitForCommitRun(ctx context.Context, client *ForgejoClient, owner, repo, commitSHA, expectedResult string) error {
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
			if !workflowRunFinished(run) {
				continue
			}
			if workflowRunMatchesExpectation(run, expectedResult) {
				return nil
			}
			actualResult := firstNonEmpty(run.Conclusion, run.Status, "unknown")
			return fmt.Errorf("workflow run %d for %s/%s completed with %s, expected %s", run.ID, owner, repo, actualResult, expectedResult)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for workflow run on %s/%s commit %s", owner, repo, commitSHA)
		}
		time.Sleep(15 * time.Second)
	}
}

func workflowRunFinished(run WorkflowRun) bool {
	status := strings.ToLower(strings.TrimSpace(run.Status))
	conclusion := strings.ToLower(strings.TrimSpace(run.Conclusion))

	switch status {
	case "success", "failure":
		return true
	case "completed":
		return true
	}

	return conclusion != ""
}

func workflowRunMatchesExpectation(run WorkflowRun, expectedResult string) bool {
	expectedResult = strings.ToLower(strings.TrimSpace(expectedResult))
	actualResult := strings.ToLower(strings.TrimSpace(firstNonEmpty(run.Conclusion, run.Status)))
	return expectedResult != "" && actualResult == expectedResult
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
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

func validateLeaseWitnessSummary(summary leaseWitnessSummary, expectedJobs int) error {
	if expectedJobs <= 0 {
		return fmt.Errorf("expected_jobs must be positive")
	}
	if len(summary.DistinctJobIDs) < expectedJobs {
		return fmt.Errorf("observed %d distinct jobs, expected %d", len(summary.DistinctJobIDs), expectedJobs)
	}
	requiredOverlap := 2
	if expectedJobs == 1 {
		requiredOverlap = 1
	}
	if summary.MaxActiveLeases < requiredOverlap {
		return fmt.Errorf("max active leases %d, required at least %d", summary.MaxActiveLeases, requiredOverlap)
	}
	return nil
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
