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
		t.Error("Job.Meta: missing — nomad-deploy needs the key declared so submit-time digest injection merges cleanly")
	}

	groups, _ := job["TaskGroups"].([]any)
	if len(groups) != 1 {
		t.Fatalf("TaskGroups: got %d, want 1", len(groups))
	}
	group := groups[0].(map[string]any)

	update, ok := group["Update"].(map[string]any)
	if !ok {
		t.Fatal("TaskGroups[0].Update: missing")
	}
	if v, _ := update["MaxParallel"].(float64); int(v) != 1 {
		t.Errorf("Update.MaxParallel: got %v, want 1", update["MaxParallel"])
	}
	if v, _ := update["AutoRevert"].(bool); !v {
		t.Errorf("Update.AutoRevert: got %v, want true", update["AutoRevert"])
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
	if len(tasks) != 1 {
		t.Fatalf("Tasks: got %d, want 1", len(tasks))
	}
	task := tasks[0].(map[string]any)
	if drv, _ := task["Driver"].(string); drv != "raw_exec" {
		t.Errorf("Task.Driver: got %q, want raw_exec", drv)
	}
	if u, _ := task["User"].(string); u != "profile_service" {
		t.Errorf("Task.User: got %q, want profile_service", u)
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
		// Every placeholder except component_auth_audience must be
		// resolved at render time. nomad-deploy substitutes the audience
		// from /etc/verself/<id>/auth_audience at submit time.
		stripped := strings.ReplaceAll(s, nomad.AuthAudiencePlaceholder, "")
		if strings.Contains(stripped, "{{") || strings.Contains(stripped, "}}") {
			t.Errorf("Task.Env[%q]=%q: unresolved placeholder leaked into rendered spec", k, s)
		}
	}
}

// TestRender_IndexEnumeratesAllOptedIn enforces the renderer/tool
// contract: every component that produces a job spec also gets an
// index.json row, and vice versa. nomad-deploy enumerate is the only
// reader of that file and the contract has to be tight.
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
	rows, _ := idx["components"].([]any)
	indexed := map[string]bool{}
	for _, raw := range rows {
		row := raw.(map[string]any)
		jobID, _ := row["job_id"].(string)
		indexed[jobID] = true
		if _, _, _, _ = row["component"], row["bazel_label"], row["output"], row["job_id"]; row["component"] == "" || row["bazel_label"] == "" || row["output"] == "" {
			t.Errorf("index entry %v missing required fields", row)
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

func keys(m map[string]map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
