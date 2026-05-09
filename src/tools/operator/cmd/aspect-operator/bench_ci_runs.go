package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	ch "github.com/ClickHouse/clickhouse-go/v2"
	chdriver "github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"
	opch "github.com/verself/operator-runtime/clickhouse"
	opruntime "github.com/verself/operator-runtime/runtime"
)

// bench-ci-runs ingests wall-clock timings from the verself-sh repo's own
// GitHub Actions runs into verself.benchmark_runs. The headline benchmark
// is "is the Verself runner faster than Blacksmith on our own monorepo
// Bazel build?" — that comparison lives implicitly in the actual CI
// workflows that run on every PR/push, so this command turns those runs
// into structured benchmark rows without needing a separate dispatch.
//
// Workflow → (provider, cache_variant) mapping:
//
//   - verself-ci.yml          → provider=verself,    cache_variant=verself-checkpoint
//   - github-ci.yml           → provider=verself,    cache_variant=actions-cache
//   - blacksmith-ci.yml       → provider=blacksmith, cache_variant=blacksmith-stickydisk
//
// All rows land with workload="monorepo-bazel" and cache_state="warm";
// per-run cold/dirty differentiation is not yet captured here because the
// real CI flow does not produce identity-rerun pairs the way the canary
// does.

type benchCIRunsOptions struct {
	operatorRuntimeOptions
	repository string
	githubAPI  string
	githubToken string
	limit      int
	branches   string
}

type ciWorkflowSpec struct {
	File         string
	Provider     string
	CacheVariant string
}

var ciWorkflowCatalog = []ciWorkflowSpec{
	{File: "verself-ci.yml", Provider: "verself", CacheVariant: "verself-checkpoint"},
	{File: "github-ci.yml", Provider: "verself", CacheVariant: "actions-cache"},
	{File: "blacksmith-ci.yml", Provider: "blacksmith", CacheVariant: "blacksmith-stickydisk"},
}

type ghRun struct {
	ID           int64     `json:"id"`
	Name         string    `json:"name"`
	HeadBranch   string    `json:"head_branch"`
	HeadSHA      string    `json:"head_sha"`
	Status       string    `json:"status"`
	Conclusion   string    `json:"conclusion"`
	RunStartedAt time.Time `json:"run_started_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	HTMLURL      string    `json:"html_url"`
}

type ghRunsPage struct {
	WorkflowRuns []ghRun `json:"workflow_runs"`
}

func cmdBenchCIRuns(args []string) error {
	opts := benchCIRunsOptions{
		repository:  envOr("BENCH_CI_REPOSITORY", "guardian-intelligence/verself"),
		githubAPI:   envOr("BENCH_CI_GITHUB_API_URL", "https://api.github.com"),
		githubToken: firstNonEmpty(os.Getenv("BENCH_CI_GITHUB_TOKEN"), os.Getenv("GITHUB_TOKEN")),
		limit:       50,
		branches:    "main,beta,gamma",
	}
	fs := flagSet("bench-ci-runs")
	fs.StringVar(&opts.repository, "repository", opts.repository, "owner/name of the repository to ingest")
	fs.StringVar(&opts.githubAPI, "github-api-url", opts.githubAPI, "GitHub API base URL")
	fs.StringVar(&opts.githubToken, "github-token", opts.githubToken, "GitHub token (read access to actions runs)")
	fs.IntVar(&opts.limit, "limit", opts.limit, "Max runs to fetch per workflow")
	fs.StringVar(&opts.branches, "branches", opts.branches, "Comma-separated branch allowlist")
	addOperatorRuntimeFlags(&opts.operatorRuntimeOptions)
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if strings.TrimSpace(opts.githubToken) == "" {
		return fmt.Errorf("BENCH_CI_GITHUB_TOKEN, GITHUB_TOKEN, or --github-token is required")
	}
	if strings.TrimSpace(opts.repository) == "" {
		return fmt.Errorf("--repository is required")
	}
	branches := splitCSV(opts.branches)
	branchSet := make(map[string]struct{}, len(branches))
	for _, b := range branches {
		branchSet[b] = struct{}{}
	}
	return runOperatorRuntime("benchmark.ci_runs", opts.operatorRuntimeOptions, false, opch.Config{Database: "verself"},
		func(rt *opruntime.Runtime, chClient *opch.Client) error {
			rows, err := collectCIRunsRows(rt.Ctx, &http.Client{Timeout: 30 * time.Second}, opts, branchSet)
			if err != nil {
				return err
			}
			if len(rows) == 0 {
				return fmt.Errorf("no eligible CI runs returned for %s", opts.repository)
			}
			existing, err := existingProviderRunIDs(rt.Ctx, chClient.Conn, rows)
			if err != nil {
				return err
			}
			fresh := rows[:0]
			for _, row := range rows {
				if _, seen := existing[row.ProviderRunID]; seen {
					continue
				}
				fresh = append(fresh, row)
			}
			if len(fresh) == 0 {
				fmt.Fprintf(os.Stdout, "no new CI runs to ingest (existing=%d)\n", len(existing))
				return nil
			}
			if err := insertBenchmarkRuns(rt.Ctx, chClient.Conn, fresh); err != nil {
				return err
			}
			fmt.Fprintf(os.Stdout, "ingested %d CI runs (skipped %d already-present)\n", len(fresh), len(rows)-len(fresh))
			return nil
		})
}

func collectCIRunsRows(ctx context.Context, client *http.Client, opts benchCIRunsOptions, branchSet map[string]struct{}) ([]benchmarkRunRow, error) {
	var rows []benchmarkRunRow
	for _, spec := range ciWorkflowCatalog {
		page, err := fetchWorkflowRuns(ctx, client, opts, spec.File)
		if err != nil {
			return nil, fmt.Errorf("fetch %s runs: %w", spec.File, err)
		}
		for _, run := range page.WorkflowRuns {
			if run.Status != "completed" {
				continue
			}
			if len(branchSet) > 0 {
				if _, ok := branchSet[run.HeadBranch]; !ok {
					continue
				}
			}
			rows = append(rows, ciRunToBenchmarkRow(run, opts.repository, spec))
		}
	}
	return rows, nil
}

func fetchWorkflowRuns(ctx context.Context, client *http.Client, opts benchCIRunsOptions, workflow string) (ghRunsPage, error) {
	q := url.Values{}
	q.Set("per_page", fmt.Sprintf("%d", opts.limit))
	q.Set("status", "completed")
	q.Set("exclude_pull_requests", "false")
	path := fmt.Sprintf("/repos/%s/actions/workflows/%s/runs?%s", opts.repository, url.PathEscape(workflow), q.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(opts.githubAPI, "/")+path, nil)
	if err != nil {
		return ghRunsPage{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+opts.githubToken)
	req.Header.Set("X-GitHub-Api-Version", githubAPIVersion)
	req.Header.Set("User-Agent", "verself-bench-ci-runs")
	resp, err := client.Do(req)
	if err != nil {
		return ghRunsPage{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return ghRunsPage{}, nil
	}
	if resp.StatusCode != http.StatusOK {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return ghRunsPage{}, fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(detail)))
	}
	var page ghRunsPage
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(&page); err != nil {
		return ghRunsPage{}, err
	}
	return page, nil
}

// ciRunToBenchmarkRow maps a finished GitHub Actions workflow run to a
// benchmark_runs row. The cache_state is "warm" because real CI does not
// produce identity reruns; the canary covers cold/warm/dirty separately.
// The unique provider_run_id (the GitHub run id) is used to skip rows we've
// already ingested.
func ciRunToBenchmarkRow(run ghRun, repository string, spec ciWorkflowSpec) benchmarkRunRow {
	wallClockMs := int64(0)
	if !run.RunStartedAt.IsZero() && !run.UpdatedAt.IsZero() && run.UpdatedAt.After(run.RunStartedAt) {
		wallClockMs = run.UpdatedAt.Sub(run.RunStartedAt).Milliseconds()
	}
	result := run.Conclusion
	if result == "" {
		result = "unknown"
	}
	return benchmarkRunRow{
		BenchmarkRunID:     uuid.New(),
		Workload:           "monorepo-bazel",
		Provider:           spec.Provider + ":" + spec.CacheVariant,
		CacheState:         "warm",
		RunnerClass:        "",
		RepositoryFullName: repository,
		HeadBranch:         run.HeadBranch,
		CommitSHA:          run.HeadSHA,
		ProviderRunID:      uint64(run.ID),
		ProviderJobID:      0,
		AttemptID:          uuid.Nil,
		ExecutionID:        uuid.Nil,
		WallClockMs:        uint64MaybeNeg(wallClockMs),
		Result:             result,
		ObservedAt:         run.UpdatedAt.UTC(),
	}
}

func existingProviderRunIDs(ctx context.Context, conn chdriver.Conn, rows []benchmarkRunRow) (map[uint64]struct{}, error) {
	if len(rows) == 0 {
		return map[uint64]struct{}{}, nil
	}
	ids := make([]uint64, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ProviderRunID)
	}
	const q = `
SELECT DISTINCT provider_run_id
FROM verself.benchmark_runs
WHERE workload = 'monorepo-bazel'
  AND provider_run_id IN ({ids:Array(UInt64)})
`
	out := make(map[uint64]struct{}, len(rows))
	rowsCH, err := conn.Query(ctx, q, ch.Named("ids", ids))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rowsCH.Close() }()
	for rowsCH.Next() {
		var id uint64
		if err := rowsCH.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = struct{}{}
	}
	return out, rowsCH.Err()
}

func splitCSV(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
