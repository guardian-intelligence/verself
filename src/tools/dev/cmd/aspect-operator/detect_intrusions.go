package main

import (
	"fmt"
	"os"
	"strconv"

	opch "github.com/verself/operator-runtime/clickhouse"
	opruntime "github.com/verself/operator-runtime/runtime"
)

type detectIntrusionsOptions struct {
	operatorRuntimeOptions
	hours  uint
	format string
}

func cmdDetectIntrusions(args []string) error {
	fs := flagSet("detect-intrusions")
	opts := &detectIntrusionsOptions{}
	addOperatorRuntimeFlags(&opts.operatorRuntimeOptions)
	fs.StringVar(&opts.site, "site", opts.site, "Deploy site")
	fs.StringVar(&opts.repoRoot, "repo-root", "", "verself-sh checkout root (defaults to cwd)")
	fs.UintVar(&opts.hours, "hours", 24, "Lookback window in hours")
	fs.StringVar(&opts.format, "format", "table", "Output format: table|json|csv|tsv")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if opts.hours == 0 || opts.hours > 24*31 {
		return fmt.Errorf("detect-intrusions: --hours must be between 1 and 744")
	}
	switch normalizeOutputFormat(opts.format) {
	case "table", "json", "csv", "tsv":
	default:
		return fmt.Errorf("detect-intrusions: --format must be table, json, csv, or tsv")
	}
	return runOperatorRuntime("detect_intrusions", opts.operatorRuntimeOptions, false, opch.Config{Database: "verself"}, func(rt *opruntime.Runtime, chClient *opch.Client) error {
		table, err := chClient.QueryTableParams(rt.Ctx, detectIntrusionsSQL(), map[string]string{
			"hours": strconv.FormatUint(uint64(opts.hours), 10),
		})
		if err != nil {
			return err
		}
		return printTableFormat(os.Stdout, table, opts.format)
	})
}

func detectIntrusionsSQL() string {
	return `
SELECT
    recorded_at,
    outcome,
    auth_method,
    cert_id,
    user,
    source_ip,
    body
FROM verself.host_auth_events
WHERE event_date >= today() - 31
  AND recorded_at >= now() - toIntervalHour({hours:UInt32})
  AND outcome = 'accepted'
  AND source_ip NOT IN ('127.0.0.1', '::1')
ORDER BY recorded_at DESC
`
}
