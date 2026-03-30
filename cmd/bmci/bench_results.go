package main

import (
	"fmt"
	"log/slog"
	"math"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/forge-metal/forge-metal/internal/clickhouse"
	"github.com/forge-metal/forge-metal/internal/config"
)

type benchRow struct {
	RunID     string
	Jobs      uint64
	DepsMs    float64
	LintMs    float64
	TscMs     float64
	BuildMs   float64
	TestMs    float64
	CIMs      float64
	E2EMs     float64
	MemMB     float64
	WrittenMB float64
	CloneMs   float64
}

func benchResultsCmd() *cobra.Command {
	var (
		configPath string
		project    string
	)

	cmd := &cobra.Command{
		Use:   "bench-results",
		Short: "Compare baseline vs optimized benchmark results from ClickHouse",
		Long: `Queries the two most recent benchmark runs for a project and prints
a side-by-side comparison with speedup ratios.

Requires ClickHouse to be running with ci_events data.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			if cfg.ClickHouse.Addr == "" {
				return fmt.Errorf("clickhouse address not configured")
			}

			ch, err := clickhouse.New(cfg.ClickHouse)
			if err != nil {
				return fmt.Errorf("connect to clickhouse: %w", err)
			}
			defer ch.Close()

			ctx := cmd.Context()

			query := `
				SELECT
					run_id,
					count()                                  AS jobs,
					round(median(deps_install_ns) / 1e6)     AS deps_ms,
					round(median(lint_ns) / 1e6)             AS lint_ms,
					round(median(typecheck_ns) / 1e6)        AS tsc_ms,
					round(median(build_ns) / 1e6)            AS build_ms,
					round(median(test_ns) / 1e6)             AS test_ms,
					round(median(total_ci_ns) / 1e6)         AS ci_ms,
					round(median(total_e2e_ns) / 1e6)        AS e2e_ms,
					round(median(memory_peak_bytes) / 1e6)   AS mem_mb,
					round(median(zfs_written_bytes) / 1e6)   AS written_mb,
					round(median(zfs_clone_ns) / 1e6, 1)     AS clone_ms
				FROM ` + cfg.ClickHouse.Database + `.ci_events
				WHERE repo = $1
				  AND created_at > now() - INTERVAL 24 HOUR
				GROUP BY run_id
				ORDER BY min(created_at) ASC
			`

			rows, err := ch.QueryRows(ctx, query, project)
			if err != nil {
				return fmt.Errorf("query: %w", err)
			}
			defer rows.Close()

			var results []benchRow
			for rows.Next() {
				var r benchRow
				if err := rows.Scan(
					&r.RunID, &r.Jobs,
					&r.DepsMs, &r.LintMs, &r.TscMs, &r.BuildMs, &r.TestMs,
					&r.CIMs, &r.E2EMs, &r.MemMB, &r.WrittenMB, &r.CloneMs,
				); err != nil {
					return fmt.Errorf("scan row: %w", err)
				}
				results = append(results, r)
			}
			if err := rows.Err(); err != nil {
				return fmt.Errorf("iterate rows: %w", err)
			}

			if len(results) == 0 {
				return fmt.Errorf("no benchmark results found for project %q in the last 24 hours", project)
			}

			printResults(project, results)
			return nil
		},
	}

	cmd.Flags().StringVar(&configPath, "config", "", "Config file override")
	cmd.Flags().StringVar(&project, "project", "next-learn", "Project name to compare")

	return cmd
}

func printResults(project string, results []benchRow) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	if len(results) == 1 {
		fmt.Printf("\nBenchmark: %s (1 run, %d jobs)\n\n", project, results[0].Jobs)
		printSingleRun(results[0])
		return
	}

	// Use first as baseline, last as optimized.
	baseline := results[0]
	optimized := results[len(results)-1]

	fmt.Printf("\nBenchmark: %s (%d runs)\n", project, len(results))
	fmt.Printf("  Baseline:  run %s (%d jobs)\n", short(baseline.RunID), baseline.Jobs)
	fmt.Printf("  Optimized: run %s (%d jobs)\n\n", short(optimized.RunID), optimized.Jobs)

	w := tabwriter.NewWriter(os.Stdout, 2, 4, 3, ' ', tabwriter.AlignRight)
	fmt.Fprintf(w, "  %s\t%s\t%s\t%s\t\n", "Phase", "Baseline", "Optimized", "Speedup")
	fmt.Fprintf(w, "  %s\t%s\t%s\t%s\t\n", "─────", "────────", "─────────", "───────")

	printPhaseRow(w, "ZFS clone", baseline.CloneMs, optimized.CloneMs)
	printPhaseRow(w, "deps", baseline.DepsMs, optimized.DepsMs)
	printPhaseRow(w, "lint", baseline.LintMs, optimized.LintMs)
	if baseline.TscMs > 0 || optimized.TscMs > 0 {
		printPhaseRow(w, "typecheck", baseline.TscMs, optimized.TscMs)
	}
	printPhaseRow(w, "build", baseline.BuildMs, optimized.BuildMs)
	if baseline.TestMs > 0 || optimized.TestMs > 0 {
		printPhaseRow(w, "test", baseline.TestMs, optimized.TestMs)
	}
	fmt.Fprintf(w, "  %s\t%s\t%s\t%s\t\n", "─────", "────────", "─────────", "───────")
	printPhaseRow(w, "Total CI", baseline.CIMs, optimized.CIMs)
	printPhaseRow(w, "Total E2E", baseline.E2EMs, optimized.E2EMs)
	w.Flush()

	fmt.Println()
	fmt.Printf("  Memory peak:  %s → %s\n", fmtMB(baseline.MemMB), fmtMB(optimized.MemMB))
	fmt.Printf("  ZFS written:  %s → %s\n", fmtMB(baseline.WrittenMB), fmtMB(optimized.WrittenMB))
	fmt.Println()

	logger.Info("query ClickHouse for full data",
		"sql", fmt.Sprintf("SELECT * FROM ci_events WHERE run_id IN ('%s','%s') ORDER BY created_at",
			baseline.RunID, optimized.RunID))
}

func printSingleRun(r benchRow) {
	w := tabwriter.NewWriter(os.Stdout, 2, 4, 3, ' ', 0)
	fmt.Fprintf(w, "  %s\t%s\t\n", "Phase", "Median (ms)")
	fmt.Fprintf(w, "  %s\t%s\t\n", "─────", "───────────")
	fmt.Fprintf(w, "  %s\t%s\t\n", "ZFS clone", fmtMs(r.CloneMs))
	fmt.Fprintf(w, "  %s\t%s\t\n", "deps", fmtMs(r.DepsMs))
	fmt.Fprintf(w, "  %s\t%s\t\n", "lint", fmtMs(r.LintMs))
	fmt.Fprintf(w, "  %s\t%s\t\n", "build", fmtMs(r.BuildMs))
	fmt.Fprintf(w, "  %s\t%s\t\n", "Total CI", fmtMs(r.CIMs))
	fmt.Fprintf(w, "  %s\t%s\t\n", "Total E2E", fmtMs(r.E2EMs))
	w.Flush()
}

func printPhaseRow(w *tabwriter.Writer, name string, baseline, optimized float64) {
	if baseline == 0 && optimized == 0 {
		return
	}
	speedup := "—"
	if optimized == 0 && baseline > 0 {
		speedup = "skipped"
	} else if optimized > 0 && baseline > 0 {
		ratio := baseline / optimized
		speedup = fmt.Sprintf("%.1fx", ratio)
	}
	fmt.Fprintf(w, "  %s\t%s\t%s\t%s\t\n", name, fmtMs(baseline), fmtMs(optimized), speedup)
}

func fmtMs(ms float64) string {
	if ms == 0 {
		return "0"
	}
	if ms < 1000 {
		return fmt.Sprintf("%.0fms", ms)
	}
	return fmt.Sprintf("%.1fs", ms/1000)
}

func fmtMB(mb float64) string {
	if mb == 0 {
		return "0"
	}
	return fmt.Sprintf("%.0fMB", math.Round(mb))
}

func short(runID string) string {
	if len(runID) > 8 {
		return runID[:8]
	}
	return runID
}
