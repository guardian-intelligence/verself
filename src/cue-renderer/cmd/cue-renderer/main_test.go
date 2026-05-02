package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/verself/cue-renderer/internal/load"
	"github.com/verself/cue-renderer/internal/render"
)

// topologyDir returns the absolute path to src/cue-renderer/ for the
// committed instance fixtures. Bazel populates TEST_SRCDIR/TEST_WORKSPACE
// for `bazelisk test`; `go test` falls back to walking up from the test
// file's location until cue.mod/module.cue is reached.
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
	t.Fatalf("could not locate src/cue-renderer/cue.mod/module.cue from %s or runfiles", wd)
	return ""
}

func TestRendererRegistry(t *testing.T) {
	rs := renderers()
	if len(rs) == 0 {
		t.Fatal("renderers() returned empty set")
	}
	seen := map[string]bool{}
	for _, r := range rs {
		name := r.Name()
		if name == "" {
			t.Errorf("%T returned empty Name()", r)
			continue
		}
		if seen[name] {
			t.Errorf("renderer name %q registered more than once", name)
		}
		seen[name] = true
	}
}

func TestRendererByName(t *testing.T) {
	for _, r := range renderers() {
		got, ok := rendererByName(r.Name())
		if !ok || got.Name() != r.Name() {
			t.Errorf("rendererByName(%q): got=%v ok=%v", r.Name(), got, ok)
		}
	}
	if _, ok := rendererByName("does-not-exist"); ok {
		t.Error("rendererByName(\"does-not-exist\") returned ok=true")
	}
}

func TestRenderAll_OutputPathShape(t *testing.T) {
	loaded, err := load.Topology(topologyDir(t), "prod")
	if err != nil {
		t.Fatalf("load topology: %v", err)
	}
	mem, artifacts, err := renderAll(context.Background(), loaded)
	if err != nil {
		t.Fatalf("renderAll: %v", err)
	}

	paths := mem.Paths()
	if len(paths) == 0 {
		t.Fatal("renderAll produced no files")
	}
	// Per-deploy artefacts are cache-relative under inventory/, share/,
	// handlers/, or jobs/. Bazel-input artefacts are authored in their owning
	// packages and are no longer emitted by cue-renderer.
	validRoots := []string{
		"inventory/",
		"share/",
		"handlers/",
		"jobs/",
	}
	produced := map[string]bool{}
	for _, p := range paths {
		if filepath.IsAbs(p) {
			t.Errorf("path %q is absolute; renderers must emit cache-relative paths", p)
		}
		validRoot := false
		for _, root := range validRoots {
			if strings.HasPrefix(p, root) {
				validRoot = true
				break
			}
		}
		if !validRoot {
			t.Errorf("path %q is not anchored at one of %v", p, validRoots)
		}
		produced[artifacts[p]] = true
	}
	for _, r := range renderers() {
		if !produced[r.Name()] {
			t.Errorf("renderer %q produced no files", r.Name())
		}
	}
}

func TestRenderAll_Determinism(t *testing.T) {
	loaded, err := load.Topology(topologyDir(t), "prod")
	if err != nil {
		t.Fatalf("load topology: %v", err)
	}

	mem1, _, err := renderAll(context.Background(), loaded)
	if err != nil {
		t.Fatalf("renderAll first: %v", err)
	}
	mem2, _, err := renderAll(context.Background(), loaded)
	if err != nil {
		t.Fatalf("renderAll second: %v", err)
	}

	paths1, paths2 := mem1.Paths(), mem2.Paths()
	if len(paths1) != len(paths2) {
		t.Fatalf("path count diverged: %d vs %d", len(paths1), len(paths2))
	}
	for i, p := range paths1 {
		if p != paths2[i] {
			t.Fatalf("path order diverged at %d: %q vs %q", i, p, paths2[i])
		}
		if mem1.SHA256(p) != mem2.SHA256(p) {
			t.Errorf("non-deterministic output at %s: %s vs %s", p, mem1.SHA256(p), mem2.SHA256(p))
		}
	}
}

func TestRenderAll_NoOverlap(t *testing.T) {
	// renderAll already errors when two renderers claim the same path; this
	// is the second-loop assertion that the artifacts map covers every path
	// emitted by the MemFS, with one renderer per path.
	loaded, err := load.Topology(topologyDir(t), "prod")
	if err != nil {
		t.Fatalf("load topology: %v", err)
	}
	mem, artifacts, err := renderAll(context.Background(), loaded)
	if err != nil {
		t.Fatalf("renderAll: %v", err)
	}
	for _, p := range mem.Paths() {
		if artifacts[p] == "" {
			t.Errorf("path %q has no recorded owner in artifacts map", p)
		}
	}
	if len(artifacts) != len(mem.Paths()) {
		t.Errorf("artifacts (%d) and paths (%d) disagree", len(artifacts), len(mem.Paths()))
	}
}

func TestLoadTopology_GraphSHA256Stable(t *testing.T) {
	dir := topologyDir(t)
	a, err := load.Topology(dir, "prod")
	if err != nil {
		t.Fatalf("first load: %v", err)
	}
	b, err := load.Topology(dir, "prod")
	if err != nil {
		t.Fatalf("second load: %v", err)
	}
	if a.GraphSHA256 != b.GraphSHA256 {
		t.Fatalf("GraphSHA256 unstable: %s vs %s", a.GraphSHA256, b.GraphSHA256)
	}
	if a.TopologySHA256 != b.TopologySHA256 {
		t.Fatalf("TopologySHA256 unstable: %s vs %s", a.TopologySHA256, b.TopologySHA256)
	}
	if a.ConfigSHA256 != b.ConfigSHA256 {
		t.Fatalf("ConfigSHA256 unstable: %s vs %s", a.ConfigSHA256, b.ConfigSHA256)
	}
	if a.CatalogSHA256 != b.CatalogSHA256 {
		t.Fatalf("CatalogSHA256 unstable: %s vs %s", a.CatalogSHA256, b.CatalogSHA256)
	}
	if len(a.GraphSHA256) != 64 {
		t.Fatalf("GraphSHA256 not hex-64: %q", a.GraphSHA256)
	}
}

func TestRender_SingleArtifactToMemFS(t *testing.T) {
	loaded, err := load.Topology(topologyDir(t), "prod")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	for _, r := range renderers() {
		mem := render.NewMemFS()
		if err := r.Render(context.Background(), loaded, mem); err != nil {
			t.Errorf("render %s: %v", r.Name(), err)
			continue
		}
		if len(mem.Paths()) == 0 {
			t.Errorf("renderer %q wrote no files", r.Name())
		}
	}
}
