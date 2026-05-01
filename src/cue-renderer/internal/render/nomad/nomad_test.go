package nomad_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/verself/cue-renderer/internal/load"
	"github.com/verself/cue-renderer/internal/render"
	"github.com/verself/cue-renderer/internal/render/nomad"
)

func topologyDir(t testing.TB) string {
	t.Helper()
	if srcdir := os.Getenv("TEST_SRCDIR"); srcdir != "" {
		ws := os.Getenv("TEST_WORKSPACE")
		if ws == "" {
			ws = "_main"
		}
		dir := filepath.Join(srcdir, ws, "src/cue-renderer")
		if _, err := os.Stat(filepath.Join(dir, "cue.mod", "module.cue")); err == nil {
			return dir
		}
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for cur := wd; cur != "/" && cur != "."; cur = filepath.Dir(cur) {
		candidate := filepath.Join(cur, "src/cue-renderer")
		if _, err := os.Stat(filepath.Join(candidate, "cue.mod", "module.cue")); err == nil {
			return candidate
		}
	}
	t.Fatalf("could not locate src/cue-renderer/cue.mod/module.cue")
	return ""
}

// renderProd runs the Nomad renderer against the committed prod
// instance and returns the produced files as a path → JSON map.
func renderProd(t testing.TB) map[string]map[string]any {
	t.Helper()
	loaded, err := load.Topology(topologyDir(t), "prod")
	if err != nil {
		t.Fatalf("load topology: %v", err)
	}
	mem := render.NewMemFS()
	if err := (nomad.Renderer{}).Render(context.Background(), loaded, mem); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := map[string]map[string]any{}
	for _, p := range mem.Paths() {
		body, ok := mem.Get(p)
		if !ok {
			t.Fatalf("MemFS lost %q", p)
		}
		var parsed map[string]any
		if err := json.Unmarshal(body, &parsed); err != nil {
			t.Fatalf("parse %s: %v", p, err)
		}
		out[p] = parsed
	}
	return out
}

// TestRender_ProfileServiceShape locks the load-bearing invariants of
// the rendered profile-service spec. Anything that drifts here is a
// real shape change the operator should review.
func TestRender_ProfileServiceShape(t *testing.T) {
	files := renderProd(t)
	spec, ok := files["jobs/profile-service.nomad.json"]
	if !ok {
		t.Fatalf("profile-service spec missing; got files: %v", keys(files))
	}
	job, ok := spec["Job"].(map[string]any)
	if !ok {
		t.Fatalf("Job: missing or wrong type")
	}
	if id, _ := job["ID"].(string); id != "profile-service" {
		t.Errorf("Job.ID: got %q, want %q", id, "profile-service")
	}
	if jt, _ := job["Type"].(string); jt != "service" {
		t.Errorf("Job.Type: got %q, want %q", jt, "service")
	}
	if dcs, _ := job["Datacenters"].([]any); len(dcs) != 1 || dcs[0] != "dc1" {
		t.Errorf("Job.Datacenters: got %v, want [dc1]", dcs)
	}
	if _, ok := job["Meta"].(map[string]any); !ok {
		t.Error("Job.Meta: missing — the Bazel Nomad resolver needs the key declared so digest stamping merges cleanly")
	}

	groups, _ := job["TaskGroups"].([]any)
	if len(groups) != 1 {
		t.Fatalf("TaskGroups: got %d, want 1", len(groups))
	}
	group := groups[0].(map[string]any)

	// TaskGroup.Update is a per-service runtime contract. The renderer
	// reserves the key as a placeholder; src/cue-renderer/nomad/resolve_jobs.py
	// fills it in from each component's nomad-overrides.json before
	// stamping spec_sha256. Confirm the key is declared and unfilled.
	if _, present := group["Update"]; !present {
		t.Error("TaskGroups[0].Update: key not declared; the resolver expects to overwrite an existing field")
	}
	if v := group["Update"]; v != nil {
		t.Errorf("TaskGroups[0].Update: renderer should leave this nil for the resolver to fill, got %v", v)
	}

	networks, _ := group["Networks"].([]any)
	if len(networks) != 1 {
		t.Fatalf("Networks: got %d, want 1", len(networks))
	}
	net := networks[0].(map[string]any)
	if mode, _ := net["Mode"].(string); mode != "host" {
		t.Errorf("Networks[0].Mode: got %q, want %q", mode, "host")
	}
	reserved, _ := net["ReservedPorts"].([]any)
	wantPorts := map[string]int{"public_http": 4258, "internal_https": 4259}
	if len(reserved) != len(wantPorts) {
		t.Fatalf("ReservedPorts: got %d, want %d", len(reserved), len(wantPorts))
	}
	for _, raw := range reserved {
		p := raw.(map[string]any)
		label, _ := p["Label"].(string)
		value, _ := p["Value"].(float64)
		if hn, _ := p["HostNetwork"].(string); hn != "loopback" {
			t.Errorf("ReservedPort %q HostNetwork: got %q, want %q", label, hn, "loopback")
		}
		if want, ok := wantPorts[label]; !ok || want != int(value) {
			t.Errorf("ReservedPort %q value: got %v, want %d", label, value, want)
		}
	}

	tasks, _ := group["Tasks"].([]any)
	if len(tasks) == 0 {
		t.Fatalf("Tasks: empty")
	}
	task := serviceTask(t, group)
	if drv, _ := task["Driver"].(string); drv != "raw_exec" {
		t.Errorf("Task.Driver: got %q, want raw_exec", drv)
	}
	if u, _ := task["User"].(string); u != "profile_service" {
		t.Errorf("Task.User: got %q, want profile_service", u)
	}
	config, _ := task["Config"].(map[string]any)
	if got, _ := config["command"].(string); got != "local/bin/profile-service" {
		t.Errorf("Task.Config.command: got %q, want local/bin/profile-service", got)
	}
	artifacts, _ := task["Artifacts"].([]any)
	if len(artifacts) != 1 {
		t.Fatalf("Task.Artifacts: got %d, want 1", len(artifacts))
	}
	artifact := artifacts[0].(map[string]any)
	if chown, _ := artifact["Chown"].(bool); !chown {
		t.Errorf("Task.Artifacts[0].Chown: got %v, want true", artifact["Chown"])
	}
	if got, _ := artifact["GetterSource"].(string); got != "verself-artifact://profile-service" {
		t.Errorf("Task.Artifacts[0].GetterSource: got %q", got)
	}

	env, _ := task["Env"].(map[string]any)
	if len(env) == 0 {
		t.Fatal("Task.Env: empty")
	}
	for k, v := range env {
		s, ok := v.(string)
		if !ok {
			t.Errorf("Task.Env[%q]: not a string (%T)", k, v)
			continue
		}
		if strings.Contains(s, "{{") || strings.Contains(s, "}}") {
			t.Errorf("Task.Env[%q]=%q: unresolved placeholder leaked into rendered spec", k, s)
		}
	}
}

// TestRender_SandboxRentalMultiUnitShape locks the first multi-process
// Nomad migration: the primary task binds both service ports, while the
// recurring worker binds none.
func TestRender_SandboxRentalMultiUnitShape(t *testing.T) {
	files := renderProd(t)
	spec, ok := files["jobs/sandbox-rental.nomad.json"]
	if !ok {
		t.Fatalf("sandbox-rental spec missing; got files: %v", keys(files))
	}
	job, ok := spec["Job"].(map[string]any)
	if !ok {
		t.Fatalf("Job: missing or wrong type")
	}
	if id, _ := job["ID"].(string); id != "sandbox-rental" {
		t.Errorf("Job.ID: got %q, want sandbox-rental", id)
	}

	groups, _ := job["TaskGroups"].([]any)
	if len(groups) != 2 {
		t.Fatalf("TaskGroups: got %d, want 2", len(groups))
	}
	byName := map[string]map[string]any{}
	for _, raw := range groups {
		group := raw.(map[string]any)
		name, _ := group["Name"].(string)
		byName[name] = group
	}

	primary := byName["sandbox-rental-service"]
	if primary == nil {
		t.Fatalf("primary TaskGroup missing; got %v", keysAny(byName))
	}
	worker := byName["sandbox-rental-recurring-worker"]
	if worker == nil {
		t.Fatalf("worker TaskGroup missing; got %v", keysAny(byName))
	}

	assertTaskMemory(t, primary, 512)
	assertTaskMemory(t, worker, 512)

	primaryPorts := reservedPortMap(t, primary)
	wantPorts := map[string]int{"internal_https": 4263, "public_http": 4243}
	if len(primaryPorts) != len(wantPorts) {
		t.Fatalf("primary ReservedPorts: got %v, want %v", primaryPorts, wantPorts)
	}
	for label, want := range wantPorts {
		if got := primaryPorts[label]; got != want {
			t.Errorf("primary ReservedPorts[%s]: got %d, want %d", label, got, want)
		}
	}
	if ports := reservedPortMap(t, worker); len(ports) != 0 {
		t.Fatalf("worker ReservedPorts: got %v, want empty", ports)
	}

	primaryEnv := taskEnv(t, primary)
	for key, want := range map[string]string{
		"VERSELF_CRED_CLICKHOUSE_CA_CERT":       "/etc/credstore/sandbox-rental/clickhouse-ca-cert",
		"VERSELF_CRED_FORGEJO_TOKEN":            "/etc/credstore/sandbox-rental/forgejo-token",
		"VERSELF_CRED_FORGEJO_WEBHOOK_SECRET":   "/etc/credstore/sandbox-rental/forgejo-webhook-secret",
		"VERSELF_CRED_FORGEJO_BOOTSTRAP_SECRET": "/etc/credstore/sandbox-rental/forgejo-bootstrap-secret",
	} {
		if got, _ := primaryEnv[key].(string); got != want {
			t.Errorf("primary Env[%s]: got %q, want %q", key, got, want)
		}
	}
	if _, ok := primaryEnv["CREDENTIALS_DIRECTORY"]; ok {
		t.Error("primary Env unexpectedly sets CREDENTIALS_DIRECTORY")
	}
	if got, _ := primaryEnv["VERSELF_LISTEN_ADDR"].(string); got != "127.0.0.1:4243" {
		t.Errorf("primary VERSELF_LISTEN_ADDR: got %q", got)
	}
	if got, _ := primaryEnv["VERSELF_INTERNAL_LISTEN_ADDR"].(string); got != "127.0.0.1:4263" {
		t.Errorf("primary VERSELF_INTERNAL_LISTEN_ADDR: got %q", got)
	}

	workerEnv := taskEnv(t, worker)
	if _, ok := workerEnv["VERSELF_LISTEN_ADDR"]; ok {
		t.Error("worker Env unexpectedly has VERSELF_LISTEN_ADDR")
	}
	if _, ok := workerEnv["VERSELF_INTERNAL_LISTEN_ADDR"]; ok {
		t.Error("worker Env unexpectedly has VERSELF_INTERNAL_LISTEN_ADDR")
	}
	if _, ok := workerEnv["VERSELF_CLICKHOUSE_ADDRESS"]; ok {
		t.Error("worker Env unexpectedly has VERSELF_CLICKHOUSE_ADDRESS")
	}
}

func TestRender_AllApplicationWorkloadsOptedIntoNomad(t *testing.T) {
	files := renderProd(t)
	want := []string{
		"billing.nomad.json",
		"company.nomad.json",
		"governance-service.nomad.json",
		"identity-service.nomad.json",
		"mailbox-service.nomad.json",
		"notifications-service.nomad.json",
		"object-storage-service.nomad.json",
		"profile-service.nomad.json",
		"projects-service.nomad.json",
		"sandbox-rental.nomad.json",
		"secrets-service.nomad.json",
		"source-code-hosting-service.nomad.json",
		"verself-web.nomad.json",
	}
	for _, name := range want {
		path := "jobs/" + name
		if _, ok := files[path]; !ok {
			t.Errorf("%s missing", path)
		}
	}
}

// TestRender_IndexEnumeratesAllOptedIn enforces the renderer/tool
// contract: every component that produces a job spec also gets an
// index.json row, and vice versa. The Bazel Nomad resolver is the reader
// of that file and the contract has to be tight.
func TestRender_IndexEnumeratesAllOptedIn(t *testing.T) {
	files := renderProd(t)

	specs := map[string]bool{}
	for path := range files {
		if !strings.HasPrefix(path, "jobs/") || !strings.HasSuffix(path, ".nomad.json") {
			continue
		}
		jobID := strings.TrimSuffix(strings.TrimPrefix(path, "jobs/"), ".nomad.json")
		specs[jobID] = true
	}
	if len(specs) == 0 {
		t.Skip("no nomad-supervised components in prod topology yet")
	}

	idx, ok := files["jobs/index.json"]
	if !ok {
		t.Fatalf("jobs/index.json missing despite %d spec files", len(specs))
	}
	if _, ok := idx["artifact_base_url"]; ok {
		t.Fatal("jobs/index.json still exposes legacy artifact_base_url")
	}
	if _, ok := idx["artifact_store_path"]; ok {
		t.Fatal("jobs/index.json still exposes legacy artifact_store_path")
	}
	delivery, ok := idx["artifact_delivery"].(map[string]any)
	if !ok {
		t.Fatalf("jobs/index.json artifact_delivery missing or wrong type")
	}
	if got, _ := delivery["kind"].(string); got != "garage_s3_private_origin" {
		t.Fatalf("artifact_delivery.kind: got %q", got)
	}
	if got, _ := delivery["getter_source_prefix"].(string); got != "s3::https://artifacts.internal.verself.sh:9443/nomad-artifacts" {
		t.Fatalf("artifact_delivery.getter_source_prefix: got %q", got)
	}
	if got, _ := delivery["key_prefix"].(string); got != "sha256" {
		t.Fatalf("artifact_delivery.key_prefix: got %q", got)
	}
	if got, _ := delivery["public"].(bool); got {
		t.Fatal("artifact_delivery.public: got true")
	}
	origin, ok := delivery["origin"].(map[string]any)
	if !ok {
		t.Fatalf("artifact_delivery.origin missing or wrong type")
	}
	if got, _ := origin["hostname"].(string); got != "artifacts.internal.verself.sh" {
		t.Fatalf("artifact_delivery.origin.hostname: got %q", got)
	}
	if got, _ := origin["placement"].(string); got != "node_local" {
		t.Fatalf("artifact_delivery.origin.placement: got %q", got)
	}
	if got, _ := origin["listen_host"].(string); got != "127.0.0.1" {
		t.Fatalf("artifact_delivery.origin.listen_host: got %q", got)
	}
	if got, _ := origin["public_dns"].(bool); got {
		t.Fatal("artifact_delivery.origin.public_dns: got true")
	}
	if got, _ := origin["public_ingress"].(bool); got {
		t.Fatal("artifact_delivery.origin.public_ingress: got true")
	}
	rows, _ := idx["components"].([]any)
	indexed := map[string]bool{}
	for _, raw := range rows {
		row := raw.(map[string]any)
		jobID, _ := row["job_id"].(string)
		indexed[jobID] = true
		if _, _, _, _ = row["component"], row["bazel_label"], row["output"], row["job_id"]; row["component"] == "" || row["bazel_label"] == "" || row["output"] == "" {
			t.Errorf("index entry %v missing required fields", row)
		}
		artifacts, _ := row["artifacts"].([]any)
		if len(artifacts) == 0 {
			t.Errorf("index entry %v missing artifacts", row)
		}
	}
	for s := range specs {
		if !indexed[s] {
			t.Errorf("spec %s.nomad.json has no index entry", s)
		}
	}
	for i := range indexed {
		if !specs[i] {
			t.Errorf("index entry %q has no corresponding spec file", i)
		}
	}
}

func TestRender_IndexIncludesSandboxWorkerArtifact(t *testing.T) {
	files := renderProd(t)
	idx := files["jobs/index.json"]
	rows, _ := idx["components"].([]any)
	for _, raw := range rows {
		row := raw.(map[string]any)
		if component, _ := row["component"].(string); component != "sandbox_rental" {
			continue
		}
		artifacts, _ := row["artifacts"].([]any)
		outputs := map[string]bool{}
		for _, item := range artifacts {
			artifact := item.(map[string]any)
			output, _ := artifact["output"].(string)
			outputs[output] = true
		}
		for _, output := range []string{"sandbox-rental-service", "sandbox-rental-recurring-worker"} {
			if !outputs[output] {
				t.Fatalf("sandbox_rental artifacts missing %s: got %v", output, outputs)
			}
		}
		return
	}
	t.Fatal("sandbox_rental index entry missing")
}

func TestRender_FrontendNodeAppUsesRuntimeArtifact(t *testing.T) {
	files := renderProd(t)
	spec, ok := files["jobs/verself-web.nomad.json"]
	if !ok {
		t.Fatalf("verself-web spec missing; got files: %v", keys(files))
	}
	job := spec["Job"].(map[string]any)
	groups, _ := job["TaskGroups"].([]any)
	if len(groups) != 1 {
		t.Fatalf("verself-web TaskGroups: got %d, want 1", len(groups))
	}
	group := groups[0].(map[string]any)
	tasks, _ := group["Tasks"].([]any)
	if len(tasks) != 1 {
		t.Fatalf("verself-web Tasks: got %d, want service", len(tasks))
	}
	task := serviceTask(t, group)
	config, _ := task["Config"].(map[string]any)
	if got, _ := config["command"].(string); got != "local/bin/verself-web" {
		t.Fatalf("frontend command: got %q", got)
	}
	artifacts, _ := task["Artifacts"].([]any)
	outputs := map[string]bool{}
	for _, raw := range artifacts {
		artifact := raw.(map[string]any)
		source, _ := artifact["GetterSource"].(string)
		outputs[strings.TrimPrefix(source, "verself-artifact://")] = true
	}
	for _, output := range []string{"verself-web", "nodejs-runtime"} {
		if !outputs[output] {
			t.Fatalf("verself-web task artifacts missing %s: got %v", output, outputs)
		}
	}

	idx := files["jobs/index.json"]
	rows, _ := idx["components"].([]any)
	for _, raw := range rows {
		row := raw.(map[string]any)
		if component, _ := row["component"].(string); component != "verself_web" {
			continue
		}
		artifacts, _ := row["artifacts"].([]any)
		indexOutputs := map[string]bool{}
		for _, item := range artifacts {
			artifact := item.(map[string]any)
			output, _ := artifact["output"].(string)
			indexOutputs[output] = true
		}
		for _, output := range []string{"verself-web", "nodejs-runtime"} {
			if !indexOutputs[output] {
				t.Fatalf("verself_web index artifacts missing %s: got %v", output, indexOutputs)
			}
		}
		return
	}
	t.Fatal("verself_web index entry missing")
}

func TestRender_BillingUsesNomadPrestartMigration(t *testing.T) {
	files := renderProd(t)
	spec, ok := files["jobs/billing.nomad.json"]
	if !ok {
		t.Fatalf("billing spec missing; got files: %v", keys(files))
	}
	job := spec["Job"].(map[string]any)
	groups, _ := job["TaskGroups"].([]any)
	if len(groups) != 1 {
		t.Fatalf("billing TaskGroups: got %d, want 1", len(groups))
	}
	group := groups[0].(map[string]any)
	tasks, _ := group["Tasks"].([]any)
	if len(tasks) != 2 {
		t.Fatalf("billing Tasks: got %d, want migrate + service", len(tasks))
	}
	migrate := tasks[0].(map[string]any)
	if got, _ := migrate["Name"].(string); got != "billing-service-migrate" {
		t.Fatalf("migration task name: got %q", got)
	}
	lifecycle, _ := migrate["Lifecycle"].(map[string]any)
	if got, _ := lifecycle["Hook"].(string); got != "prestart" {
		t.Fatalf("migration Lifecycle.Hook: got %q", got)
	}
	if got, _ := lifecycle["Sidecar"].(bool); got {
		t.Fatal("migration Lifecycle.Sidecar: got true, want false")
	}
	config, _ := migrate["Config"].(map[string]any)
	if got, _ := config["command"].(string); got != "local/bin/billing-service" {
		t.Fatalf("migration command: got %q", got)
	}
	args, _ := config["args"].([]any)
	if len(args) != 2 || args[0] != "migrate" || args[1] != "up" {
		t.Fatalf("migration args: got %v, want [migrate up]", args)
	}
}

func keys(m map[string]map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func keysAny(m map[string]map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func taskEnv(t testing.TB, group map[string]any) map[string]any {
	t.Helper()
	task := serviceTask(t, group)
	env, _ := task["Env"].(map[string]any)
	if env == nil {
		t.Fatalf("%s Env missing", group["Name"])
	}
	return env
}

func assertTaskMemory(t testing.TB, group map[string]any, want int) {
	t.Helper()
	task := serviceTask(t, group)
	resources, _ := task["Resources"].(map[string]any)
	got, _ := resources["MemoryMB"].(float64)
	if int(got) != want {
		t.Fatalf("%s MemoryMB: got %v, want %d", group["Name"], resources["MemoryMB"], want)
	}
}

func reservedPortMap(t testing.TB, group map[string]any) map[string]int {
	t.Helper()
	networks, _ := group["Networks"].([]any)
	if len(networks) == 0 {
		return map[string]int{}
	}
	if len(networks) != 1 {
		t.Fatalf("%s Networks: got %d, want <=1", group["Name"], len(networks))
	}
	network := networks[0].(map[string]any)
	rawPorts, _ := network["ReservedPorts"].([]any)
	out := map[string]int{}
	for _, raw := range rawPorts {
		port := raw.(map[string]any)
		label, _ := port["Label"].(string)
		value, _ := port["Value"].(float64)
		if hostNetwork, _ := port["HostNetwork"].(string); hostNetwork != "loopback" {
			t.Errorf("%s ReservedPort[%s] HostNetwork: got %q, want loopback", group["Name"], label, hostNetwork)
		}
		out[label] = int(value)
	}
	return out
}

func serviceTask(t testing.TB, group map[string]any) map[string]any {
	t.Helper()
	name, _ := group["Name"].(string)
	tasks, _ := group["Tasks"].([]any)
	for _, raw := range tasks {
		task := raw.(map[string]any)
		if got, _ := task["Name"].(string); got == name {
			return task
		}
	}
	t.Fatalf("%s service task missing; got tasks %v", name, tasks)
	return nil
}
