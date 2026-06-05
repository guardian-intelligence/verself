package main

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
)

func isProjectedOutputFormat(format string) bool {
	switch normalizedOutputFormat(format) {
	case "text", "table", "dot", "mermaid":
		return true
	default:
		return false
	}
}

func writeProjectedOutput(w io.Writer, format string, value any) error {
	switch normalizedOutputFormat(format) {
	case "text":
		return writeTextOutput(w, value)
	case "table":
		return writeTableOutput(w, value)
	case "dot":
		graph, err := graphFromValue(value)
		if err != nil {
			return err
		}
		return writeDOTGraph(w, graph)
	case "mermaid":
		graph, err := graphFromValue(value)
		if err != nil {
			return err
		}
		return writeMermaidGraph(w, graph)
	default:
		return fmt.Errorf("unsupported projected output format %q", format)
	}
}

func normalizedOutputFormat(format string) string {
	key := strings.ToLower(strings.TrimSpace(format))
	key = strings.TrimPrefix(key, ".")
	if key == "yml" {
		return "yaml"
	}
	return key
}

func writeTextOutput(w io.Writer, value any) error {
	var out strings.Builder
	switch v := value.(type) {
	case guardianDocument:
		writeTextLine(&out, "document")
		writeTextField(&out, "kind", v.Kind)
		writeTextField(&out, "name", valueOrDash(v.Name))
		writeTextField(&out, "base_url", v.StaticConfig.BaseURL)
		writeTextField(&out, "credentials_ref", v.StaticConfig.CredentialsRef)
		writeTextField(&out, "substrate_state_dir", v.Board.Substrate.StateDir)
		writeTextField(&out, "access", fmt.Sprintf("%s@%s:%d", v.Board.Access.SSH.User, v.Board.Access.SSH.Host, v.Board.Access.SSH.Port))
		writeTextField(&out, "seed_target_root", v.Board.Seed.TargetRoot)
		writeTextField(&out, "seed_paths", fmt.Sprintf("%d", len(v.Board.Seed.Paths)))
		writeTextField(&out, "nomad", fmt.Sprintf("%s namespace=%s", v.Nomad.Address, v.Nomad.Namespace))
		writeTextField(&out, "nomad_jobs", fmt.Sprintf("%d", len(v.Nomad.Jobs)))
	case boardResult:
		writeTextLine(&out, resultTitle("board", v.Name))
		writeTextField(&out, "ready_to_fly", v.ReadyToFly)
		writeTextField(&out, "execution_mode", v.ExecutionMode)
		writeTextField(&out, "base_url", v.StaticConfig.BaseURL)
		writeTextField(&out, "credentials_ref", v.StaticConfig.CredentialsRef)
		writeTextField(&out, "static_config_digest", valueOrDash(v.StaticConfigDigest))
		writeTextField(&out, "substrate_state_dir", v.Substrate.StateDir)
		writeTextField(&out, "access", v.Access.Target)
		writeTextField(&out, "known_hosts_file", v.Access.KnownHostsFile)
		if v.Access.FallbackConfigured {
			writeTextField(&out, "fallback", v.Access.FallbackTarget)
		}
		writeTextField(&out, "seed_digest", valueOrDash(v.Seed.Digest))
		writeTextField(&out, "seed_root", valueOrDash(v.Seed.Root))
		writeTextField(&out, "seed_target_root", v.Seed.TargetRoot)
		writeTextField(&out, "seed_sources", fmt.Sprintf("%d", v.Seed.SourceCount))
		writeTextField(&out, "nomad", fmt.Sprintf("%s namespace=%s", v.Nomad.Address, v.Nomad.Namespace))
		writeTextSources(&out, v.Seed.Sources)
		writeTextJobs(&out, v.Jobs)
		writeTextConditions(&out, v.Conditions)
	case flyResult:
		writeTextLine(&out, resultTitle("fly", v.Name))
		writeTextField(&out, "ready_to_fly", v.ReadyToFly)
		writeTextField(&out, "execution_mode", v.ExecutionMode)
		writeTextField(&out, "static_config_digest", valueOrDash(v.StaticConfigDigest))
		writeTextField(&out, "seed_digest", valueOrDash(v.SeedDigest))
		writeTextField(&out, "seed_root", valueOrDash(v.SeedRoot))
		writeTextField(&out, "nomad", fmt.Sprintf("%s namespace=%s", v.Nomad.Address, v.Nomad.Namespace))
		writeTextJobs(&out, v.Jobs)
		writeTextConditions(&out, v.Conditions)
	default:
		return fmt.Errorf("text output is not supported for %T", value)
	}
	return writeRendered(w, out.String())
}

func writeTextSources(w io.Writer, sources []seedSourceResult) {
	if len(sources) == 0 {
		return
	}
	writeTextLine(w, "")
	writeTextLine(w, "seed sources")
	for _, source := range sources {
		_, _ = fmt.Fprintf(w, "- %s -> %s mode=%s status=%s reason=%s sha256=%s\n", source.Source, source.Target, source.Mode, source.Status, source.Reason, valueOrDash(source.SHA256))
	}
}

func writeTextJobs(w io.Writer, jobs []nomadJobResult) {
	if len(jobs) == 0 {
		return
	}
	writeTextLine(w, "")
	writeTextLine(w, "jobs")
	for _, job := range jobs {
		_, _ = fmt.Fprintf(w, "- %s status=%s reason=%s required_for=%s\n", job.Path, job.Status, job.Reason, joinedOrDash(job.RequiredFor))
	}
}

func writeTextConditions(w io.Writer, conditions []condition) {
	if len(conditions) == 0 {
		return
	}
	writeTextLine(w, "")
	writeTextLine(w, "conditions")
	for _, cond := range conditions {
		_, _ = fmt.Fprintf(w, "- %s %s reason=%s resource=%s message=%s\n", cond.Status, cond.Type, cond.Reason, valueOrDash(cond.Resource), valueOrDash(cond.Message))
	}
}

func writeTextField(w io.Writer, key string, value string) {
	_, _ = fmt.Fprintf(w, "%s: %s\n", key, value)
}

func writeTextLine(w io.Writer, line string) {
	_, _ = fmt.Fprintln(w, line)
}

func writeTableOutput(w io.Writer, value any) error {
	var out strings.Builder
	tw := tabwriter.NewWriter(&out, 0, 0, 2, ' ', 0)
	switch v := value.(type) {
	case guardianDocument:
		writeTableSection(tw, "SUMMARY")
		writeTableRows(tw, "FIELD", "VALUE")
		writeTableRows(tw, "kind", v.Kind)
		writeTableRows(tw, "name", valueOrDash(v.Name))
		writeTableRows(tw, "base_url", v.StaticConfig.BaseURL)
		writeTableRows(tw, "credentials_ref", v.StaticConfig.CredentialsRef)
		writeTableRows(tw, "substrate_state_dir", v.Board.Substrate.StateDir)
		writeTableRows(tw, "access", fmt.Sprintf("%s@%s:%d", v.Board.Access.SSH.User, v.Board.Access.SSH.Host, v.Board.Access.SSH.Port))
		writeTableRows(tw, "seed_target_root", v.Board.Seed.TargetRoot)
		writeTableRows(tw, "nomad", fmt.Sprintf("%s namespace=%s", v.Nomad.Address, v.Nomad.Namespace))
		writeDocumentSeedPathTable(tw, v.Board.Seed.Paths)
		writeDocumentJobTable(tw, v.Nomad.Jobs)
	case boardResult:
		writeTableSection(tw, "SUMMARY")
		writeTableRows(tw, "FIELD", "VALUE")
		writeTableRows(tw, "name", valueOrDash(v.Name))
		writeTableRows(tw, "ready_to_fly", v.ReadyToFly)
		writeTableRows(tw, "execution_mode", v.ExecutionMode)
		writeTableRows(tw, "base_url", v.StaticConfig.BaseURL)
		writeTableRows(tw, "credentials_ref", v.StaticConfig.CredentialsRef)
		writeTableRows(tw, "static_config_digest", valueOrDash(v.StaticConfigDigest))
		writeTableRows(tw, "substrate_state_dir", v.Substrate.StateDir)
		writeTableRows(tw, "access", v.Access.Target)
		writeTableRows(tw, "known_hosts_file", v.Access.KnownHostsFile)
		writeTableRows(tw, "fallback", fallbackSummary(v.Access))
		writeTableRows(tw, "seed_digest", valueOrDash(v.Seed.Digest))
		writeTableRows(tw, "seed_root", valueOrDash(v.Seed.Root))
		writeTableRows(tw, "seed_target_root", v.Seed.TargetRoot)
		writeTableRows(tw, "nomad_address", v.Nomad.Address)
		writeTableRows(tw, "nomad_namespace", v.Nomad.Namespace)
		writeSeedSourceTable(tw, v.Seed.Sources)
		writeNomadJobTable(tw, v.Jobs)
		writeConditionTable(tw, v.Conditions)
	case flyResult:
		writeTableSection(tw, "SUMMARY")
		writeTableRows(tw, "FIELD", "VALUE")
		writeTableRows(tw, "name", valueOrDash(v.Name))
		writeTableRows(tw, "ready_to_fly", v.ReadyToFly)
		writeTableRows(tw, "execution_mode", v.ExecutionMode)
		writeTableRows(tw, "static_config_digest", valueOrDash(v.StaticConfigDigest))
		writeTableRows(tw, "seed_digest", valueOrDash(v.SeedDigest))
		writeTableRows(tw, "seed_root", valueOrDash(v.SeedRoot))
		writeTableRows(tw, "nomad_address", v.Nomad.Address)
		writeTableRows(tw, "nomad_namespace", v.Nomad.Namespace)
		writeNomadJobTable(tw, v.Jobs)
		writeConditionTable(tw, v.Conditions)
	default:
		return fmt.Errorf("table output is not supported for %T", value)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	return writeRendered(w, out.String())
}

func writeDocumentSeedPathTable(w io.Writer, paths []seedPath) {
	if len(paths) == 0 {
		return
	}
	writeTableSection(w, "SEED PATHS")
	writeTableRows(w, "SOURCE", "TARGET", "MODE")
	for _, seedPath := range paths {
		writeTableRows(w, seedPath.Source, seedPath.Target, seedPath.Mode)
	}
}

func writeDocumentJobTable(w io.Writer, jobs []nomadJobSpec) {
	if len(jobs) == 0 {
		return
	}
	writeTableSection(w, "NOMAD JOBS")
	writeTableRows(w, "PATH", "REQUIRED_FOR")
	for _, job := range jobs {
		writeTableRows(w, job.Path, joinedOrDash(job.RequiredFor))
	}
}

func writeSeedSourceTable(w io.Writer, sources []seedSourceResult) {
	if len(sources) == 0 {
		return
	}
	writeTableSection(w, "SEED SOURCES")
	writeTableRows(w, "SOURCE", "TARGET", "MODE", "STATUS", "REASON", "SHA256")
	for _, source := range sources {
		writeTableRows(w, source.Source, source.Target, source.Mode, source.Status, source.Reason, valueOrDash(source.SHA256))
	}
}

func writeNomadJobTable(w io.Writer, jobs []nomadJobResult) {
	if len(jobs) == 0 {
		return
	}
	writeTableSection(w, "JOBS")
	writeTableRows(w, "PATH", "STATUS", "REASON", "REQUIRED_FOR")
	for _, job := range jobs {
		writeTableRows(w, job.Path, job.Status, job.Reason, joinedOrDash(job.RequiredFor))
	}
}

func writeConditionTable(w io.Writer, conditions []condition) {
	if len(conditions) == 0 {
		return
	}
	writeTableSection(w, "CONDITIONS")
	writeTableRows(w, "STATUS", "TYPE", "REASON", "RESOURCE", "MESSAGE")
	for _, cond := range conditions {
		writeTableRows(w, cond.Status, cond.Type, cond.Reason, valueOrDash(cond.Resource), valueOrDash(cond.Message))
	}
}

func writeTableSection(w io.Writer, name string) {
	_, _ = fmt.Fprintf(w, "\n%s\n", name)
}

func writeTableRows(w io.Writer, values ...string) {
	for i, value := range values {
		if i > 0 {
			_, _ = fmt.Fprint(w, "\t")
		}
		_, _ = fmt.Fprint(w, tableCell(value))
	}
	_, _ = fmt.Fprintln(w)
}

type outputGraph struct {
	Name  string
	Nodes []graphNode
	Edges []graphEdge
}

type graphNode struct {
	ID    string
	Label string
}

type graphEdge struct {
	From  string
	To    string
	Label string
}

type graphBuilder struct {
	graph outputGraph
	seen  map[string]struct{}
}

func graphFromValue(value any) (outputGraph, error) {
	switch v := value.(type) {
	case guardianDocument:
		return graphFromDocument(v), nil
	case boardResult:
		return graphFromBoardResult(v), nil
	case flyResult:
		return graphFromFlyResult(v), nil
	default:
		return outputGraph{}, fmt.Errorf("graph output is not supported for %T", value)
	}
}

func graphFromDocument(doc guardianDocument) outputGraph {
	b := newGraphBuilder("guardian")
	document := b.node("document", graphTitle("FlyProcedure", doc.Name))
	staticConfig := b.node("static_config", staticConfigSpecLabel(doc.StaticConfig))
	board := b.node("board", "board")
	substrate := b.node("substrate", substrateSpecLabel(doc.Board.Substrate))
	access := b.node("access_ssh", sshAccessSpecLabel(doc.Board.Access.SSH))
	seed := b.node("seed", seedSpecLabel(doc.Board.Seed))
	nomad := b.node("nomad", nomadSpecLabel(doc.Nomad))
	b.edge(document, staticConfig, "")
	b.edge(document, board, "")
	b.edge(board, substrate, "")
	b.edge(board, access, "")
	b.edge(board, seed, "")
	b.edge(document, nomad, "")
	for _, seedPath := range doc.Board.Seed.Paths {
		id := b.node(graphID("seed_file", seedPath.Target), seedPathSpecLabel(seedPath))
		b.edge(seed, id, "")
	}
	for _, job := range doc.Nomad.Jobs {
		id := b.node(graphID("nomad_job", job.Path), nomadJobSpecLabel(job))
		b.edge(nomad, id, joinedOrEmpty(job.RequiredFor))
	}
	return b.graph
}

func graphFromBoardResult(result boardResult) outputGraph {
	b := newGraphBuilder("guardian_board")
	board := b.node("board_result", graphTitle("board", result.Name)+"\nready_to_fly="+result.ReadyToFly+"\nexecutionMode="+result.ExecutionMode)
	staticConfig := b.node("static_config", staticConfigResultLabel(result.StaticConfig, result.StaticConfigDigest))
	substrate := b.node("substrate", substrateResultLabel(result.Substrate))
	access := b.node("access", accessResultLabel(result.Access))
	seed := b.node("seed", seedResultLabel(result.Seed))
	nomad := b.node("nomad", nomadResultLabel(result.Nomad))
	b.edge(board, staticConfig, "")
	b.edge(board, substrate, "")
	b.edge(board, access, result.Access.Method)
	b.edge(board, seed, "")
	b.edge(board, nomad, "")
	for _, source := range result.Seed.Sources {
		id := b.node(graphID("seed_file", source.Target), seedSourceResultLabel(source))
		b.edge(seed, id, "")
	}
	for _, job := range result.Jobs {
		id := b.node(graphID("nomad_job", job.Path), nomadJobResultLabel(job))
		b.edge(nomad, id, joinedOrEmpty(job.RequiredFor))
	}
	addConditionGraphNodes(b, board, result.Conditions)
	return b.graph
}

func graphFromFlyResult(result flyResult) outputGraph {
	b := newGraphBuilder("guardian_fly")
	fly := b.node("fly_result", graphTitle("fly", result.Name)+"\nready_to_fly="+result.ReadyToFly+"\nexecutionMode="+result.ExecutionMode)
	staticConfig := b.node("static_config", "staticConfig\ndigest="+valueOrDash(result.StaticConfigDigest))
	seed := b.node("seed", labelLines("seed", "digest="+valueOrDash(result.SeedDigest), "root="+valueOrDash(result.SeedRoot)))
	nomad := b.node("nomad", nomadResultLabel(result.Nomad))
	b.edge(fly, staticConfig, "")
	b.edge(fly, seed, "")
	b.edge(fly, nomad, "")
	for _, job := range result.Jobs {
		id := b.node(graphID("nomad_job", job.Path), nomadJobResultLabel(job))
		b.edge(nomad, id, joinedOrEmpty(job.RequiredFor))
	}
	addConditionGraphNodes(b, fly, result.Conditions)
	return b.graph
}

func addConditionGraphNodes(b *graphBuilder, parent string, conditions []condition) {
	for _, cond := range conditions {
		id := graphID("condition", cond.Type+"_"+cond.Reason+"_"+cond.Resource)
		label := cond.Status + " " + cond.Type + "\n" + cond.Reason
		if cond.Resource != "" {
			label += "\n" + cond.Resource
		}
		if cond.Message != "" {
			label += "\n" + cond.Message
		}
		id = b.node(id, label)
		b.edge(parent, id, "condition")
	}
}

func newGraphBuilder(name string) *graphBuilder {
	return &graphBuilder{graph: outputGraph{Name: name}, seen: map[string]struct{}{}}
}

func (b *graphBuilder) node(id string, label string) string {
	id = b.uniqueID(id)
	if _, ok := b.seen[id]; ok {
		return id
	}
	b.seen[id] = struct{}{}
	b.graph.Nodes = append(b.graph.Nodes, graphNode{ID: id, Label: label})
	return id
}

func (b *graphBuilder) edge(from string, to string, label string) {
	b.graph.Edges = append(b.graph.Edges, graphEdge{From: from, To: to, Label: label})
}

func (b *graphBuilder) uniqueID(id string) string {
	if _, ok := b.seen[id]; !ok {
		return id
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s_%d", id, i)
		if _, ok := b.seen[candidate]; !ok {
			return candidate
		}
	}
}

func staticConfigSpecLabel(config staticConfig) string {
	return labelLines(
		"staticConfig",
		"baseURL="+config.BaseURL,
		"credentialsRef="+config.CredentialsRef,
		"digest="+digestValue(config),
	)
}

func staticConfigResultLabel(config configResult, digest string) string {
	return labelLines(
		"staticConfig",
		"baseURL="+config.BaseURL,
		"credentialsRef="+config.CredentialsRef,
		"digest="+valueOrDash(digest),
	)
}

func substrateSpecLabel(substrate substrateSpec) string {
	return labelLines("substrate", "stateDir="+substrate.StateDir)
}

func substrateResultLabel(substrate substrateResult) string {
	return labelLines("substrate", "stateDir="+substrate.StateDir)
}

func sshAccessSpecLabel(ssh sshAccessSpec) string {
	lines := []string{
		"access",
		"method=ssh",
		"target=" + fmt.Sprintf("%s@%s:%d", ssh.User, ssh.Host, ssh.Port),
		"knownHostsFile=" + ssh.KnownHostsFile,
	}
	if ssh.IdentityFile != "" {
		lines = append(lines, "identityFile="+ssh.IdentityFile)
	}
	if ssh.ConnectTimeout != "" {
		lines = append(lines, "connectTimeout="+ssh.ConnectTimeout)
	}
	if ssh.WireGuardFallback != nil {
		fallback := fmt.Sprintf("%s@%s:%d", ssh.User, ssh.WireGuardFallback.Host, ssh.WireGuardFallback.Port)
		if ssh.WireGuardFallback.Interface != "" {
			fallback += " via " + ssh.WireGuardFallback.Interface
		}
		lines = append(lines, "fallback="+fallback)
	}
	return labelLines(lines...)
}

func accessResultLabel(access accessResult) string {
	lines := []string{
		"access",
		"method=" + access.Method,
		"target=" + access.Target,
		"knownHostsFile=" + access.KnownHostsFile,
	}
	if access.FallbackConfigured {
		lines = append(lines, "fallback="+access.FallbackTarget)
	}
	return labelLines(lines...)
}

func seedSpecLabel(seed seedSpec) string {
	return labelLines(
		"seed",
		"targetRoot="+seed.TargetRoot,
		"sourceCount="+fmt.Sprintf("%d", len(seed.Paths)),
	)
}

func seedResultLabel(seed seedResult) string {
	return labelLines(
		"seed",
		"targetRoot="+seed.TargetRoot,
		"root="+valueOrDash(seed.Root),
		"digest="+valueOrDash(seed.Digest),
		"sourceCount="+fmt.Sprintf("%d", seed.SourceCount),
	)
}

func seedPathSpecLabel(seedPath seedPath) string {
	return labelLines(
		"seed file",
		"target="+seedPath.Target,
		"source="+seedPath.Source,
		"mode="+seedPath.Mode,
	)
}

func seedSourceResultLabel(source seedSourceResult) string {
	return labelLines(
		"seed file",
		"target="+source.Target,
		"source="+source.Source,
		"mode="+source.Mode,
		"sha256="+valueOrDash(source.SHA256),
		"status="+source.Status,
		"reason="+source.Reason,
	)
}

func nomadSpecLabel(nomad nomadSpec) string {
	return labelLines(
		"nomad",
		"address="+nomad.Address,
		"namespace="+nomad.Namespace,
		"jobs="+fmt.Sprintf("%d", len(nomad.Jobs)),
	)
}

func nomadResultLabel(nomad nomadPlanResult) string {
	return labelLines(
		"nomad",
		"address="+nomad.Address,
		"namespace="+nomad.Namespace,
	)
}

func nomadJobSpecLabel(job nomadJobSpec) string {
	return labelLines(
		"nomad job",
		"path="+job.Path,
		"requiredFor="+joinedOrDash(job.RequiredFor),
	)
}

func nomadJobResultLabel(job nomadJobResult) string {
	return labelLines(
		"nomad job",
		"path="+job.Path,
		"requiredFor="+joinedOrDash(job.RequiredFor),
		"status="+job.Status,
		"reason="+job.Reason,
	)
}

func writeDOTGraph(w io.Writer, graph outputGraph) error {
	var out strings.Builder
	_, _ = fmt.Fprintf(&out, "digraph %s {\n", dotID(graph.Name))
	_, _ = fmt.Fprintln(&out, "  rankdir=LR;")
	_, _ = fmt.Fprintln(&out, "  node [shape=box];")
	for _, node := range graph.Nodes {
		_, _ = fmt.Fprintf(&out, "  %s [label=%s];\n", dotID(node.ID), dotString(node.Label))
	}
	for _, edge := range graph.Edges {
		if edge.Label == "" {
			_, _ = fmt.Fprintf(&out, "  %s -> %s;\n", dotID(edge.From), dotID(edge.To))
			continue
		}
		_, _ = fmt.Fprintf(&out, "  %s -> %s [label=%s];\n", dotID(edge.From), dotID(edge.To), dotString(edge.Label))
	}
	_, _ = fmt.Fprintln(&out, "}")
	return writeRendered(w, out.String())
}

func writeMermaidGraph(w io.Writer, graph outputGraph) error {
	var out strings.Builder
	_, _ = fmt.Fprintln(&out, "flowchart LR")
	for _, node := range graph.Nodes {
		_, _ = fmt.Fprintf(&out, "  %s[\"%s\"]\n", mermaidID(node.ID), mermaidLabel(node.Label))
	}
	for _, edge := range graph.Edges {
		if edge.Label == "" {
			_, _ = fmt.Fprintf(&out, "  %s --> %s\n", mermaidID(edge.From), mermaidID(edge.To))
			continue
		}
		_, _ = fmt.Fprintf(&out, "  %s -->|%s| %s\n", mermaidID(edge.From), mermaidEdgeLabel(edge.Label), mermaidID(edge.To))
	}
	return writeRendered(w, out.String())
}

func writeRendered(w io.Writer, value string) error {
	_, err := io.WriteString(w, value)
	return err
}

func resultTitle(kind string, name string) string {
	if name == "" {
		return kind
	}
	return kind + " " + name
}

func graphTitle(kind string, name string) string {
	if name == "" {
		return kind
	}
	return kind + ": " + name
}

func fallbackSummary(access accessResult) string {
	if !access.FallbackConfigured {
		return "-"
	}
	return valueOrDash(access.FallbackTarget)
}

func joinedOrDash(values []string) string {
	joined := strings.Join(values, ",")
	return valueOrDash(joined)
}

func joinedOrEmpty(values []string) string {
	return strings.Join(values, ",")
}

func valueOrDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}

func tableCell(value string) string {
	value = strings.ReplaceAll(value, "\t", " ")
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	return valueOrDash(value)
}

func graphID(prefix string, value string) string {
	suffix := sanitizedID(value)
	if suffix == "" {
		suffix = "item"
	}
	return prefix + "_" + suffix
}

func sanitizedID(value string) string {
	var b strings.Builder
	lastUnderscore := false
	for _, r := range strings.ToLower(value) {
		ok := r >= 'a' && r <= 'z' || r >= '0' && r <= '9'
		if ok {
			b.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore && b.Len() > 0 {
			b.WriteByte('_')
			lastUnderscore = true
		}
	}
	return strings.Trim(b.String(), "_")
}

func labelLines(lines ...string) string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func dotID(value string) string {
	return dotString(value)
}

func dotString(value string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range value {
		switch r {
		case '\\':
			b.WriteString("\\\\")
		case '"':
			b.WriteString("\\\"")
		case '\n':
			b.WriteString("\\n")
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

func mermaidID(value string) string {
	var b strings.Builder
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "node"
	}
	return b.String()
}

func mermaidLabel(value string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;",
		`"`, "&quot;",
		"<", "&lt;",
		">", "&gt;",
		"\r", " ",
		"\n", "<br/>",
	)
	return replacer.Replace(valueOrDash(value))
}

func mermaidEdgeLabel(value string) string {
	label := mermaidLabel(value)
	label = strings.ReplaceAll(label, "|", "/")
	return label
}
