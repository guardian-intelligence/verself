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
	{Name: "overview", Purpose: "Show services emitting telemetry, recent deploy_run_keys, and 24h error counts in one view."},
	{Name: "catalog", Purpose: "Discover telemetry vocabulary without turning the landing page into a recency dashboard."},
	{Name: "describe", Purpose: "Explain one query, metric, service, span, or log field and show valid next commands."},
	{Name: "metric", Purpose: "Query metric latest values or explicit rate windows."},
	{Name: "trace", Purpose: "Inspect a single trace by TraceId."},
	{Name: "logs", Purpose: "Discover or query structured log attributes."},
	{Name: "http", Purpose: "Query normalized HTTP access events."},
	{Name: "deploy", Purpose: "Inspect Ansible deploy traces and deploy_run_key-correlated tasks."},
	{Name: "mail", Purpose: "Inspect inbound and outbound mail events and current mail metrics."},
	{Name: "workload-identity", Purpose: "Inspect SPIFFE mTLS, JWT-SVID, OpenBao relying-party auth, and SPIRE system logs."},
	{Name: "errors", Purpose: "Query normalized recent error signals when actively debugging."},
}

var queryDocs = []queryDoc{
	{
		ID:      "overview.services",
		Family:  "overview",
		Title:   "Overview: Services",
		Purpose: "Services that have emitted telemetry in the last 24h, ranked by total samples across metrics, traces, logs, and HTTP access.",
		Optional: []string{
			"LIMIT=<rows>",
			"FORMAT=table|json|markdown",
		},
		Examples: []string{
			"make observe WHAT=overview",
		},
		Next: []string{
			"make observe WHAT=describe SERVICE=<service>",
			"make observe WHAT=service SERVICE=<service>",
		},
	},
	{
		ID:      "overview.deploys",
		Family:  "overview",
		Title:   "Overview: Recent Deploys",
		Purpose: "Deploy_run_keys observed in the last 7 days with their role count, task count, error count, and elapsed time.",
		Optional: []string{
			"LIMIT=<rows>",
			"FORMAT=table|json|markdown",
		},
		Examples: []string{
			"make observe WHAT=overview",
		},
		Next: []string{
			"make observe WHAT=deploy RUN_KEY=<deploy-run-key>",
			"make observe WHAT=catalog SIGNAL=deploys",
		},
	},
	{
		ID:      "overview.errors",
		Family:  "overview",
		Title:   "Overview: 24h Error Counts",
		Purpose: "Top services by error count across trace, log, and HTTP access signals in the last 24h.",
		Optional: []string{
			"LIMIT=<rows>",
			"FORMAT=table|json|markdown",
		},
		Examples: []string{
			"make observe WHAT=overview",
		},
		Next: []string{
			"make observe WHAT=errors SERVICE=<service>",
			"make observe WHAT=service SERVICE=<service> ERRORS=1",
		},
	},
	{
		ID:      "catalog.index",
		Family:  "catalog",
		Title:   "Catalog Index",
		Purpose: "List query families and signal catalogs. Does not query recent activity.",
		Optional: []string{
			"FORMAT=table|json|markdown",
		},
		Examples: []string{
			"make observe",
		},
		Next: []string{
			"make observe WHAT=overview",
			"make observe WHAT=queries",
			"make observe WHAT=catalog",
		},
	},
	{
		ID:      "catalog.inventory",
		Family:  "catalog",
		Title:   "Catalog Inventory",
		Purpose: "One row per signal with service count, distinct-names count (labeled by what kind of names), and 7-day row count. Entry point before drilling into a specific signal.",
		Optional: []string{
			"FORMAT=table|json|markdown",
		},
		Examples: []string{
			"make observe WHAT=catalog",
		},
		Next: []string{
			"make observe WHAT=catalog SIGNAL=metrics",
			"make observe WHAT=catalog SIGNAL=traces",
			"make observe WHAT=catalog SIGNAL=logs",
			"make observe WHAT=catalog SIGNAL=http",
			"make observe WHAT=catalog SIGNAL=deploys",
		},
	},
	{
		ID:      "queries.list",
		Family:  "catalog",
		Title:   "Query Registry",
		Purpose: "List executable observe queries with parameters, examples, and next-step hints.",
		Optional: []string{
			"FORMAT=table|json|markdown",
		},
		Examples: []string{
			"make observe WHAT=queries",
			"make observe WHAT=describe QUERY=metric.latest",
		},
		Next: []string{
			"make observe WHAT=catalog SIGNAL=metrics",
			"make observe WHAT=describe QUERY=metric.latest",
			"make observe WHAT=errors",
		},
	},
	{
		ID:      "catalog.metrics",
		Family:  "catalog",
		Title:   "Metric Catalog",
		Purpose: "Discover metric namespaces and metric names from the semantic metric views.",
		Optional: []string{
			"SERVICE=<service-name>",
			"PREFIX=<metric-prefix>",
			"SEARCH=<substring>",
			"LIMIT=<rows>",
			"FORMAT=table|json|markdown",
		},
		Examples: []string{
			"make observe WHAT=catalog SIGNAL=metrics",
			"make observe WHAT=catalog SIGNAL=metrics PREFIX=system.",
			"make observe WHAT=catalog SIGNAL=metrics SEARCH=wireguard",
		},
		Next: []string{
			"make observe WHAT=describe METRIC=<metric>",
			"make observe WHAT=metric METRIC=<metric>",
		},
	},
	{
		ID:      "catalog.traces",
		Family:  "catalog",
		Title:   "Trace Span Catalog",
		Purpose: "Discover span names, emitting services, span kinds, and status vocabulary.",
		Optional: []string{
			"SERVICE=<service-name>",
			"SEARCH=<substring>",
			"LIMIT=<rows>",
			"FORMAT=table|json|markdown",
		},
		Examples: []string{
			"make observe WHAT=catalog SIGNAL=traces",
			"make observe WHAT=catalog SIGNAL=traces SERVICE=ansible",
			"make observe WHAT=describe SPAN=ansible.task",
		},
		Next: []string{
			"make observe WHAT=describe SPAN=<span>",
			"make observe WHAT=trace TRACE_ID=<trace-id>",
		},
	},
	{
		ID:      "catalog.logs",
		Family:  "catalog",
		Title:   "Log Attribute Catalog",
		Purpose: "Discover structured log attribute keys and sample values.",
		Optional: []string{
			"SERVICE=<service-name>",
			"SEARCH=<substring>",
			"LIMIT=<rows>",
			"FORMAT=table|json|markdown",
		},
		Examples: []string{
			"make observe WHAT=catalog SIGNAL=logs",
			"make observe WHAT=describe FIELD=http_status",
		},
		Next: []string{
			"make observe WHAT=describe FIELD=<log-attribute-key>",
			"make observe WHAT=service SERVICE=<service>",
		},
	},
	{
		ID:      "catalog.http",
		Family:  "catalog",
		Title:   "HTTP Access Catalog",
		Purpose: "Discover normalized HTTP hosts, methods, path counts, and status ranges.",
		Optional: []string{
			"HOST=<host>",
			"SEARCH=<substring>",
			"LIMIT=<rows>",
			"FORMAT=table|json|markdown",
		},
		Examples: []string{
			"make observe WHAT=catalog SIGNAL=http",
			"make observe WHAT=http HOST=auth.example.com STATUS_MIN=400",
		},
		Next: []string{
			"make observe WHAT=http HOST=<host>",
			"make observe WHAT=errors",
		},
	},
	{
		ID:      "catalog.deploys",
		Family:  "catalog",
		Title:   "Deploy Trace Catalog",
		Purpose: "Discover deploy roles and deploy_run_key values represented in Ansible spans.",
		Optional: []string{
			"SEARCH=<role-or-run-key-substring>",
			"LIMIT=<rows>",
			"FORMAT=table|json|markdown",
		},
		Examples: []string{
			"make observe WHAT=catalog SIGNAL=deploys",
			"make observe WHAT=deploy RUN_KEY=<deploy-run-key>",
		},
		Next: []string{
			"make observe WHAT=deploy RUN_KEY=<deploy-run-key>",
		},
	},
	{
		ID:      "describe.metric",
		Family:  "describe",
		Title:   "Describe Metric",
		Purpose: "Explain metric kind, unit, emitters, attributes, cardinality, sample values, and rate suitability.",
		Required: []string{
			"METRIC=<metric-name>",
		},
		Optional: []string{
			"FORMAT=table|json|markdown",
		},
		Examples: []string{
			"make observe WHAT=describe METRIC=system.cpu.time",
		},
		Next: []string{
			"make observe WHAT=metric METRIC=<metric>",
			"make observe WHAT=metric METRIC=<metric> MODE=rate GROUP_BY=<attribute>",
		},
	},
	{
		ID:      "describe.service",
		Family:  "describe",
		Title:   "Describe Service",
		Purpose: "List telemetry signals, metrics, spans, and log attributes known for one service.",
		Required: []string{
			"SERVICE=<service-name>",
		},
		Optional: []string{
			"LIMIT=<rows>",
			"FORMAT=table|json|markdown",
		},
		Examples: []string{
			"make observe WHAT=describe SERVICE=billing-service",
		},
		Next: []string{
			"make observe WHAT=catalog SIGNAL=metrics SERVICE=<service>",
			"make observe WHAT=service SERVICE=<service>",
		},
	},
	{
		ID:      "describe.span",
		Family:  "describe",
		Title:   "Describe Span",
		Purpose: "Explain one span name, emitting services, duration shape, statuses, and span attributes.",
		Required: []string{
			"SPAN=<span-name>",
		},
		Optional: []string{
			"SERVICE=<service-name>",
			"FORMAT=table|json|markdown",
		},
		Examples: []string{
			"make observe WHAT=describe SPAN=ansible.task",
		},
		Next: []string{
			"make observe WHAT=catalog SIGNAL=traces SERVICE=<service>",
			"make observe WHAT=trace TRACE_ID=<trace-id>",
		},
	},
	{
		ID:      "describe.field",
		Family:  "describe",
		Title:   "Describe Field",
		Purpose: "Find one attribute across log, span, and resource attribute maps; show services, row counts, and sample values per surface.",
		Required: []string{
			"FIELD=<attribute-key>",
		},
		Optional: []string{
			"SERVICE=<service-name>",
			"FORMAT=table|json|markdown",
		},
		Examples: []string{
			"make observe WHAT=describe FIELD=http_status",
			"make observe WHAT=describe FIELD=deploy_run_key",
		},
		Next: []string{
			"make observe WHAT=catalog SIGNAL=logs SERVICE=<service>",
			"make observe WHAT=describe SPAN=<span>",
		},
	},
	{
		ID:      "describe.query",
		Family:  "describe",
		Title:   "Describe Observe Query",
		Purpose: "Explain one observe query's purpose, parameters, examples, and next commands.",
		Required: []string{
			"QUERY=<query-id>",
		},
		Examples: []string{
			"make observe WHAT=describe QUERY=metric.latest",
			"make observe WHAT=describe QUERY=catalog.metrics",
		},
		Next: []string{
			"make observe WHAT=queries",
			"Run one of the described query examples",
		},
	},
	{
		ID:      "metric.latest",
		Family:  "metric",
		Title:   "Latest Metric Values",
		Purpose: "Show latest samples for one metric, optionally grouped by one metric attribute.",
		Required: []string{
			"METRIC=<metric-name>",
		},
		Optional: []string{
			"GROUP_BY=<metric-attribute-key>",
			"LIMIT=<rows>",
			"FORMAT=table|json|markdown",
		},
		Examples: []string{
			"make observe WHAT=metric METRIC=system.cpu.time",
			"make observe WHAT=metric METRIC=system.cpu.time GROUP_BY=state",
		},
		Next: []string{
			"make observe WHAT=describe METRIC=<metric>",
		},
	},
	{
		ID:      "metric.rate",
		Family:  "metric",
		Title:   "Metric Rate",
		Purpose: "Calculate an explicit per-second delta window for monotonic sum metrics.",
		Required: []string{
			"METRIC=<metric-name>",
			"MODE=rate",
		},
		Optional: []string{
			"GROUP_BY=<metric-attribute-key>",
			"MINUTES=<lookback>",
			"LIMIT=<rows>",
			"FORMAT=table|json|markdown",
		},
		Examples: []string{
			"make observe WHAT=metric METRIC=system.network.io MODE=rate GROUP_BY=device",
		},
		Next: []string{
			"make observe WHAT=describe METRIC=<metric>",
		},
	},
	{
		ID:      "service.activity",
		Family:  "service",
		Title:   "Service Operational View",
		Purpose: "Explicit recent HTTP spans and logs for one service.",
		Required: []string{
			"SERVICE=<service-name>",
		},
		Optional: []string{
			"ERRORS=1",
			"MINUTES=<lookback>",
			"LIMIT=<rows>",
			"FORMAT=table|json|markdown",
		},
		Examples: []string{
			"make observe WHAT=service SERVICE=billing-service",
			"make observe WHAT=service SERVICE=sandbox-rental-service ERRORS=1",
		},
		Next: []string{
			"make observe WHAT=describe SERVICE=<service>",
			"make observe WHAT=catalog SIGNAL=logs SERVICE=<service>",
			"make observe WHAT=errors SERVICE=<service>",
		},
	},
	{
		ID:      "errors.recent",
		Family:  "errors",
		Title:   "Recent Error Signals",
		Purpose: "Explicit recent errors across trace, log, and HTTP access projections.",
		Optional: []string{
			"SERVICE=<service-name>",
			"MINUTES=<lookback>",
			"LIMIT=<rows>",
			"FORMAT=table|json|markdown",
		},
		Examples: []string{
			"make observe WHAT=errors",
			"make observe WHAT=errors SERVICE=caddy",
		},
		Next: []string{
			"make observe WHAT=service SERVICE=<service> ERRORS=1",
			"make observe WHAT=trace TRACE_ID=<trace-id>",
			"make observe WHAT=http STATUS_MIN=400",
		},
	},
	{
		ID:      "http.access",
		Family:  "http",
		Title:   "HTTP Access Events",
		Purpose: "Explicit recent normalized HTTP access rows.",
		Optional: []string{
			"HOST=<host>",
			"STATUS_MIN=<status>",
			"SEARCH=<path-substring>",
			"MINUTES=<lookback>",
			"LIMIT=<rows>",
			"FORMAT=table|json|markdown",
		},
		Examples: []string{
			"make observe WHAT=http STATUS_MIN=400",
			"make observe WHAT=http HOST=auth.example.com STATUS_MIN=400",
		},
		Next: []string{
			"make observe WHAT=catalog SIGNAL=http",
			"make observe WHAT=errors",
			"make observe WHAT=trace TRACE_ID=<trace-id>",
		},
	},
	{
		ID:      "logs.recent",
		Family:  "logs",
		Title:   "Recent Structured Logs",
		Purpose: "Explicit recent log rows with optional service, field, and text filters.",
		Optional: []string{
			"SERVICE=<service-name>",
			"FIELD=<log-attribute-key>",
			"SEARCH=<body-or-attribute-substring>",
			"MINUTES=<lookback>",
			"LIMIT=<rows>",
			"FORMAT=table|json|markdown",
		},
		Examples: []string{
			"make observe WHAT=logs SERVICE=observe",
			"make observe WHAT=logs FIELD=query_id SEARCH=metric.latest",
		},
		Next: []string{
			"make observe WHAT=catalog SIGNAL=logs",
			"make observe WHAT=describe FIELD=<log-attribute-key>",
		},
	},
	{
		ID:      "trace.detail",
		Family:  "trace",
		Title:   "Trace Detail",
		Purpose: "Inspect one trace tree by TraceId.",
		Required: []string{
			"TRACE_ID=<trace-id>",
		},
		Optional: []string{
			"FORMAT=table|json|markdown",
		},
		Examples: []string{
			"make observe WHAT=trace TRACE_ID=<trace-id>",
		},
		Next: []string{
			"make observe WHAT=describe SPAN=<span>",
			"make observe WHAT=service SERVICE=<service>",
		},
	},
	{
		ID:      "deploy.tasks",
		Family:  "deploy",
		Title:   "Deploy Tasks",
		Purpose: "Explicit recent Ansible task spans or all tasks for one deploy_run_key.",
		Optional: []string{
			"RUN_KEY=<deploy-run-key>",
			"MINUTES=<lookback>",
			"LIMIT=<rows>",
			"FORMAT=table|json|markdown",
		},
		Examples: []string{
			"make observe WHAT=deploy",
			"make observe WHAT=deploy RUN_KEY=2026-04-17.000017@rust-forge-01",
		},
		Next: []string{
			"make observe WHAT=catalog SIGNAL=deploys",
			"make observe WHAT=trace TRACE_ID=<trace-id>",
		},
	},
	{
		ID:      "mail.events",
		Family:  "mail",
		Title:   "Mail Events",
		Purpose: "Explicit recent mail events plus latest mail metrics.",
		Optional: []string{
			"MINUTES=<lookback>",
			"LIMIT=<rows>",
			"FORMAT=table|json|markdown",
		},
		Examples: []string{
			"make observe WHAT=mail",
		},
		Next: []string{
			"make observe WHAT=describe SERVICE=stalwart",
			"make observe WHAT=catalog SIGNAL=logs SERVICE=stalwart",
			"make observe WHAT=service SERVICE=stalwart",
		},
	},
	{
		ID:      "workload_identity.spans",
		Family:  "workload-identity",
		Title:   "Workload Identity Spans",
		Purpose: "Show recent SPIFFE mTLS, JWT-SVID, and OpenBao relying-party auth spans.",
		Optional: []string{
			"MINUTES=<lookback>",
			"LIMIT=<rows>",
			"FORMAT=table|json|markdown",
		},
		Examples: []string{
			"make observe WHAT=workload-identity",
		},
		Next: []string{
			"make observe WHAT=describe SPAN=auth.spiffe.mtls.client",
			"make observe WHAT=describe SPAN=workload.openbao.kv.get",
			"make observe WHAT=describe SPAN=secrets.bao.jwt_svid.login",
		},
	},
	{
		ID:      "workload_identity.spire_logs",
		Family:  "workload-identity",
		Title:   "SPIRE Logs",
		Purpose: "Show recent SPIRE server and agent logs.",
		Optional: []string{
			"MINUTES=<lookback>",
			"LIMIT=<rows>",
			"FORMAT=table|json|markdown",
		},
		Examples: []string{
			"make observe WHAT=workload-identity",
		},
		Next: []string{
			"make observe WHAT=logs SERVICE=spire-agent",
			"make observe WHAT=service SERVICE=spire-server",
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
				return true, errors.New("WHAT=describe QUERY does not accept METRIC, SERVICE, SPAN, or FIELD")
			}
			return true, printQueryDescription(cfg)
		}
	}
	return false, nil
}

func printIndex(cfg config) error {
	index := staticIndex{
		Title:    "Forge Metal Observe",
		Purpose:  "Discover the telemetry vocabulary before running operational queries.",
		Families: sortedFamilies(),
		StartHere: []string{
			"make observe WHAT=overview",
			"make observe WHAT=queries",
			"make observe WHAT=catalog SIGNAL=metrics",
			"make observe WHAT=describe SERVICE=<service>",
		},
		Operational: []string{
			"make observe WHAT=errors",
			"make observe WHAT=logs SERVICE=<service>",
			"make observe WHAT=service SERVICE=<service>",
			"make observe WHAT=http STATUS_MIN=400",
			"make observe WHAT=workload-identity",
			"make observe WHAT=deploy RUN_KEY=<deploy-run-key>",
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
		fmt.Println("# Observe Query Registry")
		fmt.Println()
		for _, doc := range docs {
			printQueryDocMarkdown(os.Stdout, doc)
			fmt.Println()
		}
	default:
		fmt.Println("Observe Query Registry")
		fmt.Println()
		for _, doc := range docs {
			fmt.Printf("%-22s %-10s %s\n", doc.ID, doc.Family, doc.Purpose)
		}
		fmt.Println("\nDescribe one query:")
		fmt.Println("  make observe WHAT=describe QUERY=metric.latest")
	}
	return nil
}

func printQueryDescription(cfg config) error {
	doc, ok := findQueryDoc(cfg.queryName)
	if !ok {
		return fmt.Errorf("unknown observe query %q; run `make observe WHAT=queries`", cfg.queryName)
	}
	switch cfg.format {
	case formatJSON:
		return writeStaticJSON(doc)
	case formatMarkdown:
		printQueryDocMarkdown(os.Stdout, doc)
	default:
		fmt.Printf("%s\n\n", doc.Title)
		fmt.Printf("ID:      %s\n", doc.ID)
		fmt.Printf("Family:  %s\n", doc.Family)
		fmt.Printf("Purpose: %s\n", doc.Purpose)
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
	fmt.Fprintf(out, "## %s\n\n", doc.ID)
	fmt.Fprintf(out, "%s\n\n", doc.Purpose)
	fmt.Fprintf(out, "- Family: `%s`\n", doc.Family)
	printMarkdownList(out, "Required", doc.Required)
	printMarkdownList(out, "Optional", doc.Optional)
	printMarkdownList(out, "Examples", doc.Examples)
	printMarkdownList(out, "Next", doc.Next)
}

func printList(title string, values []string) {
	if len(values) == 0 {
		return
	}
	fmt.Printf("\n%s:\n", title)
	for _, value := range values {
		fmt.Printf("  %s\n", value)
	}
}

func printMarkdownList(out io.Writer, title string, values []string) {
	if len(values) == 0 {
		return
	}
	fmt.Fprintf(out, "- %s:\n", title)
	for _, value := range values {
		fmt.Fprintf(out, "  - `%s`\n", value)
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
