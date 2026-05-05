package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

type queryDoc struct {
	ID       string   `json:"id"`
	Family   string   `json:"family"`
	Title    string   `json:"title"`
	Purpose  string   `json:"purpose"`
	Required []string `json:"required,omitempty"`
	Optional []string `json:"optional,omitempty"`
	Examples []string `json:"examples"`
	Next     []string `json:"next,omitempty"`
}

type staticIndex struct {
	Title       string   `json:"title"`
	Purpose     string   `json:"purpose"`
	Families    []family `json:"families"`
	StartHere   []string `json:"start_here"`
	Operational []string `json:"operational"`
}

type family struct {
	Name    string `json:"name"`
	Purpose string `json:"purpose"`
}

var families = []family{
	{Name: "overview", Purpose: "Show services emitting telemetry, recent deploy_run_keys, and error counts in one view. Windows are fixed (24h for services and errors, 7d for deploys); --minutes is ignored."},
	{Name: "catalog", Purpose: "Discover telemetry vocabulary without turning the landing page into a recency dashboard."},
	{Name: "describe", Purpose: "Explain one query, metric, service, span, or log field and show valid next commands."},
	{Name: "metric", Purpose: "Query metric latest values or explicit rate windows."},
	{Name: "trace", Purpose: "Inspect a single trace by TraceId."},
	{Name: "logs", Purpose: "Discover or query structured log attributes."},
	{Name: "http", Purpose: "Query normalized HTTP access events."},
	{Name: "deploy", Purpose: "Inspect verself-deploy, Bazel, and Nomad spans correlated by deploy_run_key."},
	{Name: "supply-chain", Purpose: "Inspect artifact policy results recorded during deploy runs."},
	{Name: "mail", Purpose: "Inspect inbound and outbound mail events and current mail metrics."},
	{Name: "workload-identity", Purpose: "Inspect SPIFFE mTLS, JWT-SVID, OpenBao relying-party auth, and SPIRE system logs."},
	{Name: "temporal", Purpose: "Inspect Temporal Web requests, Temporal auth decisions, smoke-test workflow activity, service logs, and live Temporal metric inventory."},
	{Name: "errors", Purpose: "Query normalized recent error signals when actively debugging."},
}

var queryDocs = []queryDoc{
	{
		ID:      "overview.services",
		Family:  "overview",
		Title:   "Overview: Services",
		Purpose: "Services that have emitted telemetry in the last 24h, ranked by total samples across metrics, traces, logs, and HTTP access.",
		Optional: []string{
			"--limit=<rows>",
			"--format=table|json|markdown",
		},
		Examples: []string{
			"aspect observe --what=overview",
		},
		Next: []string{
			"aspect observe --what=describe --service=<service>",
			"aspect observe --what=service --service=<service>",
		},
	},
	{
		ID:      "overview.deploys",
		Family:  "overview",
		Title:   "Overview: Recent Deploys",
		Purpose: "Deploy_run_keys observed in the last 7 days with service/span counts, error count, and elapsed time.",
		Optional: []string{
			"--limit=<rows>",
			"--format=table|json|markdown",
		},
		Examples: []string{
			"aspect observe --what=overview",
		},
		Next: []string{
			"aspect observe --what=deploy --run-key=<deploy-run-key>",
			"aspect observe --what=catalog --signal=deploys",
		},
	},
	{
		ID:      "overview.errors",
		Family:  "overview",
		Title:   "Overview: 24h Error Counts",
		Purpose: "Top services by error count across trace, log, and HTTP access signals in the last 24h.",
		Optional: []string{
			"--limit=<rows>",
			"--format=table|json|markdown",
		},
		Examples: []string{
			"aspect observe --what=overview",
		},
		Next: []string{
			"aspect observe --what=errors --service=<service>",
			"aspect observe --what=service --service=<service> --errors",
		},
	},
	{
		ID:      "catalog.index",
		Family:  "catalog",
		Title:   "Catalog Index",
		Purpose: "List query families and signal catalogs. Does not query recent activity.",
		Optional: []string{
			"--format=table|json|markdown",
		},
		Examples: []string{
			"aspect observe",
		},
		Next: []string{
			"aspect observe --what=overview",
			"aspect observe --what=queries",
			"aspect observe --what=catalog",
		},
	},
	{
		ID:      "catalog.inventory",
		Family:  "catalog",
		Title:   "Catalog Inventory",
		Purpose: "One row per signal with service count, distinct-names count (labeled by what kind of names), and 7-day row count. Entry point before drilling into a specific signal.",
		Optional: []string{
			"--format=table|json|markdown",
		},
		Examples: []string{
			"aspect observe --what=catalog",
		},
		Next: []string{
			"aspect observe --what=catalog --signal=metrics",
			"aspect observe --what=catalog --signal=traces",
			"aspect observe --what=catalog --signal=logs",
			"aspect observe --what=catalog --signal=http",
			"aspect observe --what=catalog --signal=deploys",
		},
	},
	{
		ID:      "queries.list",
		Family:  "catalog",
		Title:   "Query Registry",
		Purpose: "List executable observe queries with parameters, examples, and next-step hints.",
		Optional: []string{
			"--format=table|json|markdown",
		},
		Examples: []string{
			"aspect observe --what=queries",
			"aspect observe --what=describe --query=metric.latest",
		},
		Next: []string{
			"aspect observe --what=catalog --signal=metrics",
			"aspect observe --what=describe --query=metric.latest",
			"aspect observe --what=errors",
		},
	},
	{
		ID:      "catalog.metrics",
		Family:  "catalog",
		Title:   "Metric Catalog",
		Purpose: "Discover metric namespaces and metric names from the semantic metric views.",
		Optional: []string{
			"--service=<service-name>",
			"--prefix=<metric-prefix>",
			"--search=<substring>",
			"--limit=<rows>",
			"--format=table|json|markdown",
		},
		Examples: []string{
			"aspect observe --what=catalog --signal=metrics",
			"aspect observe --what=catalog --signal=metrics --prefix=system.",
			"aspect observe --what=catalog --signal=metrics --search=wireguard",
		},
		Next: []string{
			"aspect observe --what=describe --metric=<metric>",
			"aspect observe --what=metric --metric=<metric>",
		},
	},
	{
		ID:      "catalog.traces",
		Family:  "catalog",
		Title:   "Trace Span Catalog",
		Purpose: "Discover span names, emitting services, span kinds, and status vocabulary.",
		Optional: []string{
			"--service=<service-name>",
			"--search=<substring>",
			"--limit=<rows>",
			"--format=table|json|markdown",
		},
		Examples: []string{
			"aspect observe --what=catalog --signal=traces",
			"aspect observe --what=catalog --signal=traces --service=verself-deploy",
			"aspect observe --what=describe --span=verself_deploy.run",
		},
		Next: []string{
			"aspect observe --what=describe --span=<span>",
			"aspect observe --what=trace --trace-id=<trace-id>",
		},
	},
	{
		ID:      "catalog.logs",
		Family:  "catalog",
		Title:   "Log Attribute Catalog",
		Purpose: "Discover structured log attribute keys and sample values.",
		Optional: []string{
			"--service=<service-name>",
			"--search=<substring>",
			"--limit=<rows>",
			"--format=table|json|markdown",
		},
		Examples: []string{
			"aspect observe --what=catalog --signal=logs",
			"aspect observe --what=describe --field=http_status",
		},
		Next: []string{
			"aspect observe --what=describe --field=<log-attribute-key>",
			"aspect observe --what=service --service=<service>",
		},
	},
	{
		ID:      "catalog.http",
		Family:  "catalog",
		Title:   "HTTP Access Catalog",
		Purpose: "Discover normalized HTTP hosts, methods, path counts, and status ranges.",
		Optional: []string{
			"--host=<host>",
			"--search=<substring>",
			"--limit=<rows>",
			"--format=table|json|markdown",
		},
		Examples: []string{
			"aspect observe --what=catalog --signal=http",
			"aspect observe --what=http --host=auth.example.com --status-min=400",
		},
		Next: []string{
			"aspect observe --what=http --host=<host>",
			"aspect observe --what=errors",
		},
	},
	{
		ID:      "catalog.deploys",
		Family:  "catalog",
		Title:   "Deploy Trace Catalog",
		Purpose: "Discover deploy_run_key values and services represented in deploy-correlated spans.",
		Optional: []string{
			"--search=<role-or-run-key-substring>",
			"--limit=<rows>",
			"--format=table|json|markdown",
		},
		Examples: []string{
			"aspect observe --what=catalog --signal=deploys",
			"aspect observe --what=deploy --run-key=<deploy-run-key>",
		},
		Next: []string{
			"aspect observe --what=deploy --run-key=<deploy-run-key>",
		},
	},
	{
		ID:      "describe.metric",
		Family:  "describe",
		Title:   "Describe Metric",
		Purpose: "Explain metric kind, unit, emitters, attributes, cardinality, sample values, and rate suitability.",
		Required: []string{
			"--metric=<metric-name>",
		},
		Optional: []string{
			"--format=table|json|markdown",
		},
		Examples: []string{
			"aspect observe --what=describe --metric=system.cpu.time",
		},
		Next: []string{
			"aspect observe --what=metric --metric=<metric>",
			"aspect observe --what=metric --metric=<metric> --mode=rate --group-by=<attribute>",
		},
	},
	{
		ID:      "describe.service",
		Family:  "describe",
		Title:   "Describe Service",
		Purpose: "List telemetry signals, metrics, spans, and log attributes known for one service.",
		Required: []string{
			"--service=<service-name>",
		},
		Optional: []string{
			"--limit=<rows>",
			"--format=table|json|markdown",
		},
		Examples: []string{
			"aspect observe --what=describe --service=billing-service",
		},
		Next: []string{
			"aspect observe --what=catalog --signal=metrics --service=<service>",
			"aspect observe --what=service --service=<service>",
		},
	},
	{
		ID:      "describe.span",
		Family:  "describe",
		Title:   "Describe Span",
		Purpose: "Explain one span name, emitting services, duration shape, statuses, and span attributes.",
		Required: []string{
			"--span=<span-name>",
		},
		Optional: []string{
			"--service=<service-name>",
			"--format=table|json|markdown",
		},
		Examples: []string{
			"aspect observe --what=describe --span=verself_deploy.run",
		},
		Next: []string{
			"aspect observe --what=catalog --signal=traces --service=<service>",
			"aspect observe --what=trace --trace-id=<trace-id>",
		},
	},
	{
		ID:      "describe.field",
		Family:  "describe",
		Title:   "Describe Field",
		Purpose: "Find one attribute across log, span, and resource attribute maps; show services, row counts, and sample values per surface.",
		Required: []string{
			"--field=<attribute-key>",
		},
		Optional: []string{
			"--service=<service-name>",
			"--format=table|json|markdown",
		},
		Examples: []string{
			"aspect observe --what=describe --field=http_status",
			"aspect observe --what=describe --field=deploy_run_key",
		},
		Next: []string{
			"aspect observe --what=catalog --signal=logs --service=<service>",
			"aspect observe --what=describe --span=<span>",
		},
	},
	{
		ID:      "describe.query",
		Family:  "describe",
		Title:   "Describe Observe Query",
		Purpose: "Explain one observe query's purpose, parameters, examples, and next commands.",
		Required: []string{
			"--query=<query-id>",
		},
		Examples: []string{
			"aspect observe --what=describe --query=metric.latest",
			"aspect observe --what=describe --query=catalog.metrics",
		},
		Next: []string{
			"aspect observe --what=queries",
			"Run one of the described query examples",
		},
	},
	{
		ID:      "metric.latest",
		Family:  "metric",
		Title:   "Latest Metric Values",
		Purpose: "Show latest samples for one metric, optionally grouped by one metric attribute.",
		Required: []string{
			"--metric=<metric-name>",
		},
		Optional: []string{
			"--group-by=<metric-attribute-key>",
			"--limit=<rows>",
			"--format=table|json|markdown",
		},
		Examples: []string{
			"aspect observe --what=metric --metric=system.cpu.time",
			"aspect observe --what=metric --metric=system.cpu.time --group-by=state",
		},
		Next: []string{
			"aspect observe --what=describe --metric=<metric>",
		},
	},
	{
		ID:      "metric.rate",
		Family:  "metric",
		Title:   "Metric Rate",
		Purpose: "Calculate an explicit per-second delta window for monotonic sum metrics.",
		Required: []string{
			"--metric=<metric-name>",
			"--mode=rate",
		},
		Optional: []string{
			"--group-by=<metric-attribute-key>",
			"--minutes=<lookback>",
			"--limit=<rows>",
			"--format=table|json|markdown",
		},
		Examples: []string{
			"aspect observe --what=metric --metric=system.network.io --mode=rate --group-by=device",
		},
		Next: []string{
			"aspect observe --what=describe --metric=<metric>",
		},
	},
	{
		ID:      "service.activity",
		Family:  "service",
		Title:   "Service Operational View",
		Purpose: "Explicit recent HTTP spans and logs for one service.",
		Required: []string{
			"--service=<service-name>",
		},
		Optional: []string{
			"--errors",
			"--minutes=<lookback>",
			"--limit=<rows>",
			"--format=table|json|markdown",
		},
		Examples: []string{
			"aspect observe --what=service --service=billing-service",
			"aspect observe --what=service --service=sandbox-rental-service --errors",
		},
		Next: []string{
			"aspect observe --what=describe --service=<service>",
			"aspect observe --what=catalog --signal=logs --service=<service>",
			"aspect observe --what=errors --service=<service>",
		},
	},
	{
		ID:      "errors.recent",
		Family:  "errors",
		Title:   "Recent Error Signals",
		Purpose: "Explicit recent errors across trace, log, and HTTP access projections.",
		Optional: []string{
			"--service=<service-name>",
			"--minutes=<lookback>",
			"--limit=<rows>",
			"--format=table|json|markdown",
		},
		Examples: []string{
			"aspect observe --what=errors",
			"aspect observe --what=errors --service=haproxy",
		},
		Next: []string{
			"aspect observe --what=service --service=<service> --errors",
			"aspect observe --what=trace --trace-id=<trace-id>",
			"aspect observe --what=http --status-min=400",
		},
	},
	{
		ID:      "http.access",
		Family:  "http",
		Title:   "HTTP Access Events",
		Purpose: "Explicit recent normalized HTTP access rows.",
		Optional: []string{
			"--host=<host>",
			"--status-min=<status>",
			"--search=<path-substring>",
			"--minutes=<lookback>",
			"--limit=<rows>",
			"--format=table|json|markdown",
		},
		Examples: []string{
			"aspect observe --what=http --status-min=400",
			"aspect observe --what=http --host=auth.example.com --status-min=400",
		},
		Next: []string{
			"aspect observe --what=catalog --signal=http",
			"aspect observe --what=errors",
			"aspect observe --what=trace --trace-id=<trace-id>",
		},
	},
	{
		ID:      "logs.recent",
		Family:  "logs",
		Title:   "Recent Structured Logs",
		Purpose: "Explicit recent log rows with optional service, field, and text filters.",
		Optional: []string{
			"--service=<service-name>",
			"--field=<log-attribute-key>",
			"--search=<body-or-attribute-substring>",
			"--minutes=<lookback>",
			"--limit=<rows>",
			"--format=table|json|markdown",
		},
		Examples: []string{
			"aspect observe --what=logs --service=observe",
			"aspect observe --what=logs --field=query_id --search=metric.latest",
		},
		Next: []string{
			"aspect observe --what=catalog --signal=logs",
			"aspect observe --what=describe --field=<log-attribute-key>",
		},
	},
	{
		ID:      "trace.detail",
		Family:  "trace",
		Title:   "Trace Detail",
		Purpose: "Inspect one trace tree by TraceId.",
		Required: []string{
			"--trace-id=<trace-id>",
		},
		Optional: []string{
			"--format=table|json|markdown",
		},
		Examples: []string{
			"aspect observe --what=trace --trace-id=<trace-id>",
		},
		Next: []string{
			"aspect observe --what=describe --span=<span>",
			"aspect observe --what=service --service=<service>",
		},
	},
	{
		ID:      "deploy.tasks",
		Family:  "deploy",
		Title:   "Deploy Runs",
		Purpose: "Recent verself-deploy run spans, or the deploy/Bazel/Nomad timeline for one deploy_run_key.",
		Optional: []string{
			"--run-key=<deploy-run-key>",
			"--minutes=<lookback>",
			"--limit=<rows>",
			"--format=table|json|markdown",
		},
		Examples: []string{
			"aspect observe --what=deploy",
			"aspect observe --what=deploy --run-key=2026-04-17.000017@vs-dev-w0",
		},
		Next: []string{
			"aspect observe --what=catalog --signal=deploys",
			"aspect observe --what=trace --trace-id=<trace-id>",
		},
	},
	{
		ID:      "deploy.bazel_cache",
		Family:  "deploy",
		Title:   "Deploy Bazel Cache Hit/Miss Totals",
		Purpose: "Total bazel-remote action-cache and CAS lookups in the lookback window broken down by hit/miss. Sums the per-(kind, method, status) counter delta of bazel_remote_incoming_requests_total.",
		Optional: []string{
			"--minutes=<lookback>",
			"--format=table|json|markdown",
		},
		Examples: []string{
			"aspect observe --what=deploy",
			"aspect observe --what=deploy --minutes=1440",
		},
		Next: []string{
			"aspect observe --what=deploy --run-key=<deploy-run-key>",
		},
	},
	{
		ID:      "deploy.codegen_actions",
		Family:  "deploy",
		Title:   "Deploy Codegen Actions",
		Purpose: "Every codegen Bazel spawn (OpenAPISpec, OAPICodegen, OpenapiTsGen, NpmPackage) that ran during one deploy. cache_hit=1 means the action graph was warm; cache_hit=0 means a Huma route or spec source change forced a real codegen run.",
		Required: []string{
			"--run-key=<deploy-run-key>",
		},
		Optional: []string{
			"--format=table|json|markdown",
		},
		Examples: []string{
			"aspect observe --what=deploy --run-key=2026-04-29.000003@rust-forge-01",
		},
		Next: []string{
			"aspect observe --what=deploy --run-key=<deploy-run-key>",
			"aspect observe --what=trace --trace-id=<trace-id>",
		},
	},
	{
		ID:      "deploy.rebuild_blast_radius",
		Family:  "deploy",
		Title:   "Deploy Rebuild Blast Radius",
		Purpose: "Per-service breakdown of every Bazel spawn that ran in one deploy, split by execution vs. cache hit. Verifies a Huma-route change rebuilt the right consuming services and only the right ones.",
		Required: []string{
			"--run-key=<deploy-run-key>",
		},
		Optional: []string{
			"--format=table|json|markdown",
		},
		Examples: []string{
			"aspect observe --what=deploy --run-key=2026-04-29.000003@rust-forge-01",
		},
		Next: []string{
			"aspect observe --what=deploy --run-key=<deploy-run-key>",
			"aspect observe --what=catalog --signal=deploys",
		},
	},
	{
		ID:      "supply_chain.policy_summary",
		Family:  "supply-chain",
		Title:   "Supply-Chain Policy Summary",
		Purpose: "Group deploy-time artifact policy evidence by surface, source kind, policy result, and admission state.",
		Optional: []string{
			"--run-key=<deploy-run-key>",
			"--minutes=<lookback>",
			"--limit=<rows>",
			"--format=table|json|markdown",
		},
		Examples: []string{
			"aspect observe --what=supply-chain --run-key=<deploy-run-key>",
			"aspect observe --what=supply-chain --minutes=1440",
		},
		Next: []string{
			"aspect supply-chain inventory --format=json",
			"aspect observe --what=trace --trace-id=<trace-id>",
		},
	},
	{
		ID:      "supply_chain.policy_findings",
		Family:  "supply-chain",
		Title:   "Supply-Chain Policy Findings",
		Purpose: "Show per-source supply-chain policy evidence rows for a deploy_run_key or lookback window.",
		Optional: []string{
			"--run-key=<deploy-run-key>",
			"--minutes=<lookback>",
			"--limit=<rows>",
			"--format=table|json|markdown",
		},
		Examples: []string{
			"aspect observe --what=supply-chain --run-key=<deploy-run-key>",
		},
		Next: []string{
			"aspect observe --what=supply-chain --run-key=<deploy-run-key> --format=json",
		},
	},
	{
		ID:      "mail.events",
		Family:  "mail",
		Title:   "Mail Events",
		Purpose: "Explicit recent mail events plus latest mail metrics.",
		Optional: []string{
			"--minutes=<lookback>",
			"--limit=<rows>",
			"--format=table|json|markdown",
		},
		Examples: []string{
			"aspect observe --what=mail",
		},
		Next: []string{
			"aspect observe --what=describe --service=stalwart",
			"aspect observe --what=catalog --signal=logs --service=stalwart",
			"aspect observe --what=service --service=stalwart",
		},
	},
	{
		ID:      "workload_identity.spans",
		Family:  "workload-identity",
		Title:   "Workload Identity Spans",
		Purpose: "Show recent SPIFFE mTLS, JWT-SVID, and OpenBao relying-party auth spans.",
		Optional: []string{
			"--minutes=<lookback>",
			"--limit=<rows>",
			"--format=table|json|markdown",
		},
		Examples: []string{
			"aspect observe --what=workload-identity",
		},
		Next: []string{
			"aspect observe --what=describe --span=auth.spiffe.mtls.client",
			"aspect observe --what=describe --span=workload.openbao.kv.get",
			"aspect observe --what=describe --span=secrets.bao.jwt_svid.login",
		},
	},
	{
		ID:      "workload_identity.spire_logs",
		Family:  "workload-identity",
		Title:   "SPIRE Logs",
		Purpose: "Show recent SPIRE server and agent logs.",
		Optional: []string{
			"--minutes=<lookback>",
			"--limit=<rows>",
			"--format=table|json|markdown",
		},
		Examples: []string{
			"aspect observe --what=workload-identity",
		},
		Next: []string{
			"aspect observe --what=logs --service=spire-agent",
			"aspect observe --what=service --service=spire-server",
		},
	},
	{
		ID:      "temporal.activity",
		Family:  "temporal",
		Title:   "Temporal Activity",
		Purpose: "Show recent Temporal auth spans, schema/bootstrap runs, service logs, and metric catalog rows.",
		Optional: []string{
			"--minutes=<lookback>",
			"--limit=<rows>",
			"--format=table|json|markdown",
		},
		Examples: []string{
			"aspect observe --what=temporal",
		},
		Next: []string{
			"aspect observe --what=service --service=temporal-server",
			"aspect observe --what=describe --service=temporal-server",
			"aspect observe --what=describe --span=temporal.auth.authorize",
			"aspect observe --what=logs --service=temporal-bootstrap",
			"aspect observe --what=logs --service=temporal-schema",
		},
	},
}

func handleStatic(cfg config) (bool, error) {
	switch cfg.what {
	case "", "help":
		return true, printIndex(cfg)
	case "queries":
		return true, printQueries(cfg)
	case "describe":
		if cfg.queryName != "" {
			if cfg.metric != "" || cfg.service != "" || cfg.span != "" || cfg.field != "" {
				return true, errors.New("--what=describe --query does not accept --metric, --service, --span, or --field")
			}
			return true, printQueryDescription(cfg)
		}
	}
	return false, nil
}

func printIndex(cfg config) error {
	index := staticIndex{
		Title:    "Verself Observe",
		Purpose:  "Discover the telemetry vocabulary before running operational queries.",
		Families: sortedFamilies(),
		StartHere: []string{
			"aspect observe --what=overview",
			"aspect observe --what=queries",
			"aspect observe --what=catalog --signal=metrics",
			"aspect observe --what=describe --service=<service>",
		},
		Operational: []string{
			"aspect observe --what=errors",
			"aspect observe --what=logs --service=<service>",
			"aspect observe --what=service --service=<service>",
			"aspect observe --what=http --status-min=400",
			"aspect observe --what=workload-identity",
			"aspect observe --what=temporal",
			"aspect observe --what=deploy --run-key=<deploy-run-key>",
			"aspect observe --what=supply-chain --run-key=<deploy-run-key>",
		},
	}
	switch cfg.format {
	case formatJSON:
		return writeStaticJSON(index)
	case formatMarkdown:
		fmt.Printf("# %s\n\n%s\n\n", index.Title, index.Purpose)
		fmt.Println("## Query Families")
		for _, family := range index.Families {
			fmt.Printf("- `%s`: %s\n", family.Name, family.Purpose)
		}
		fmt.Println("\n## Start Here")
		for _, cmd := range index.StartHere {
			fmt.Printf("- `%s`\n", cmd)
		}
		fmt.Println("\n## Explicit Operational Queries")
		for _, cmd := range index.Operational {
			fmt.Printf("- `%s`\n", cmd)
		}
	default:
		fmt.Printf("%s\n\n%s\n\n", index.Title, index.Purpose)
		fmt.Println("Query families:")
		for _, family := range index.Families {
			fmt.Printf("  %-10s %s\n", family.Name, family.Purpose)
		}
		fmt.Println("\nStart here:")
		for _, cmd := range index.StartHere {
			fmt.Printf("  %s\n", cmd)
		}
		fmt.Println("\nExplicit operational queries:")
		for _, cmd := range index.Operational {
			fmt.Printf("  %s\n", cmd)
		}
	}
	return nil
}

func printQueries(cfg config) error {
	docs := sortedQueryDocs()
	switch cfg.format {
	case formatJSON:
		return writeStaticJSON(docs)
	case formatMarkdown:
		_, _ = fmt.Fprintln(os.Stdout, "# Observe Query Registry")
		_, _ = fmt.Fprintln(os.Stdout)
		for _, doc := range docs {
			printQueryDocMarkdown(os.Stdout, doc)
			_, _ = fmt.Fprintln(os.Stdout)
		}
	default:
		_, _ = fmt.Fprintln(os.Stdout, "Observe Query Registry")
		_, _ = fmt.Fprintln(os.Stdout)
		for _, doc := range docs {
			_, _ = fmt.Fprintf(os.Stdout, "%-22s %-10s %s\n", doc.ID, doc.Family, doc.Purpose)
		}
		_, _ = fmt.Fprintln(os.Stdout, "\nDescribe one query:")
		_, _ = fmt.Fprintln(os.Stdout, "  aspect observe --what=describe --query=metric.latest")
	}
	return nil
}

func printQueryDescription(cfg config) error {
	doc, ok := findQueryDoc(cfg.queryName)
	if !ok {
		return fmt.Errorf("unknown observe query %q; run `aspect observe --what=queries`", cfg.queryName)
	}
	switch cfg.format {
	case formatJSON:
		return writeStaticJSON(doc)
	case formatMarkdown:
		printQueryDocMarkdown(os.Stdout, doc)
	default:
		_, _ = fmt.Fprintf(os.Stdout, "%s\n\n", doc.Title)
		_, _ = fmt.Fprintf(os.Stdout, "ID:      %s\n", doc.ID)
		_, _ = fmt.Fprintf(os.Stdout, "Family:  %s\n", doc.Family)
		_, _ = fmt.Fprintf(os.Stdout, "Purpose: %s\n", doc.Purpose)
		printList("Required", doc.Required)
		printList("Optional", doc.Optional)
		printList("Examples", doc.Examples)
		printList("Next", doc.Next)
	}
	return nil
}

func writeStaticJSON(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func printQueryDocMarkdown(out io.Writer, doc queryDoc) {
	_, _ = fmt.Fprintf(out, "## %s\n\n", doc.ID)
	_, _ = fmt.Fprintf(out, "%s\n\n", doc.Purpose)
	_, _ = fmt.Fprintf(out, "- Family: `%s`\n", doc.Family)
	printMarkdownList(out, "Required", doc.Required)
	printMarkdownList(out, "Optional", doc.Optional)
	printMarkdownList(out, "Examples", doc.Examples)
	printMarkdownList(out, "Next", doc.Next)
}

func printList(title string, values []string) {
	if len(values) == 0 {
		return
	}
	_, _ = fmt.Fprintf(os.Stdout, "\n%s:\n", title)
	for _, value := range values {
		_, _ = fmt.Fprintf(os.Stdout, "  %s\n", value)
	}
}

func printMarkdownList(out io.Writer, title string, values []string) {
	if len(values) == 0 {
		return
	}
	_, _ = fmt.Fprintf(out, "- %s:\n", title)
	for _, value := range values {
		_, _ = fmt.Fprintf(out, "  - `%s`\n", value)
	}
}

func findQueryDoc(id string) (queryDoc, bool) {
	for _, doc := range queryDocs {
		if doc.ID == id {
			return doc, true
		}
	}
	return queryDoc{}, false
}

func sortedQueryDocs() []queryDoc {
	docs := append([]queryDoc(nil), queryDocs...)
	sort.Slice(docs, func(i, j int) bool {
		if docs[i].Family != docs[j].Family {
			return docs[i].Family < docs[j].Family
		}
		return docs[i].ID < docs[j].ID
	})
	return docs
}

func sortedFamilies() []family {
	result := append([]family(nil), families...)
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}

func queryDocNext(id string) []string {
	doc, ok := findQueryDoc(canonicalDocID(id))
	if !ok {
		return nil
	}
	return doc.Next
}

func queryPurpose(id string) string {
	doc, ok := findQueryDoc(canonicalDocID(id))
	if !ok {
		return ""
	}
	return doc.Purpose
}

func queryTitle(id string) string {
	doc, ok := findQueryDoc(id)
	if ok {
		return doc.Title
	}
	doc, ok = findQueryDoc(canonicalDocID(id))
	if !ok {
		return titleizeQueryID(id)
	}
	return doc.Title + ": " + titleizeQueryID(strings.TrimPrefix(id, canonicalDocID(id)+"."))
}

func queryFamily(id string) string {
	doc, ok := findQueryDoc(canonicalDocID(id))
	if !ok {
		return strings.Split(id, ".")[0]
	}
	return doc.Family
}

func canonicalDocID(id string) string {
	switch {
	case strings.HasPrefix(id, "catalog.metrics."):
		return "catalog.metrics"
	case strings.HasPrefix(id, "describe.metric."):
		return "describe.metric"
	case strings.HasPrefix(id, "describe.service."):
		return "describe.service"
	case strings.HasPrefix(id, "describe.span."):
		return "describe.span"
	case strings.HasPrefix(id, "describe.field."):
		return "describe.field"
	case id == "service.http_spans" || id == "service.logs":
		return "service.activity"
	case id == "mail.metrics":
		return "mail.events"
	case id == "workload_identity.spire_logs":
		return "workload_identity.spans"
	case strings.HasPrefix(id, "temporal."):
		return "temporal.activity"
	case id == "deploy.run":
		return "deploy.tasks"
	default:
		return id
	}
}

func titleizeQueryID(id string) string {
	id = strings.Trim(id, ".")
	if id == "" {
		return "details"
	}
	parts := strings.FieldsFunc(id, func(r rune) bool {
		return r == '.' || r == '_'
	})
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, " ")
}
