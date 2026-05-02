package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	opch "github.com/verself/operator-runtime/clickhouse"
	opruntime "github.com/verself/operator-runtime/runtime"
	"gopkg.in/yaml.v3"
)

type detectIntrusionsOptions struct {
	operatorRuntimeOptions
	hours  uint
	format string
}

type opsVars struct {
	KnownCertIDSuffixes []string `yaml:"known_cert_id_suffixes"`
}

func cmdDetectIntrusions(args []string) error {
	fs := flagSet("detect-intrusions")
	opts := &detectIntrusionsOptions{}
	addOperatorRuntimeFlags(&opts.operatorRuntimeOptions)
	fs.StringVar(&opts.site, "site", opts.site, "Deploy site")
	fs.StringVar(&opts.repoRoot, "repo-root", "", "verself-sh checkout root (defaults to cwd)")
	fs.StringVar(&opts.device, "device", "", "Operator device name (defaults to the single onboarded device)")
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
		suffixes, err := loadKnownCertIDSuffixes(rt.RepoRoot)
		if err != nil {
			return err
		}
		table, err := chClient.QueryTableParams(rt.Ctx, detectIntrusionsSQL(suffixes), map[string]string{
			"hours": strconv.FormatUint(uint64(opts.hours), 10),
		})
		if err != nil {
			return err
		}
		return printTableFormat(os.Stdout, table, opts.format)
	})
}

func loadKnownCertIDSuffixes(repoRoot string) ([]string, error) {
	path := filepath.Join(repoRoot, "src", "host-configuration", "ansible", "group_vars", "all", "generated", "ops.yml")
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var vars opsVars
	if err := yaml.Unmarshal(raw, &vars); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if len(vars.KnownCertIDSuffixes) == 0 {
		return nil, errors.New("known_cert_id_suffixes is empty in authored ops.yml")
	}
	return vars.KnownCertIDSuffixes, nil
}

func detectIntrusionsSQL(suffixes []string) string {
	literals := make([]string, 0, len(suffixes))
	for _, suffix := range suffixes {
		literals = append(literals, clickHouseStringLiteral(suffix))
	}
	return fmt.Sprintf(`
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
  AND (
       auth_method != 'publickey-cert'
    OR NOT match(cert_id, '^verself-(operator|workload|breakglass)-[a-z0-9-]+$')
    OR replaceRegexpOne(cert_id, '^verself-(operator|workload|breakglass)-', '') NOT IN (%s)
  )
ORDER BY recorded_at DESC
`, strings.Join(literals, ","))
}

func clickHouseStringLiteral(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `'`, `\'`)
	return "'" + value + "'"
}
