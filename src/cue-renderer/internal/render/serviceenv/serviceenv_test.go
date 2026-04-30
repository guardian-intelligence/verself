package serviceenv_test

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/verself/cue-renderer/internal/load"
	"github.com/verself/cue-renderer/internal/render/projection"
	"github.com/verself/cue-renderer/internal/render/serviceenv"
)

// topologyDir mirrors the helper in cmd/cue-renderer/main_test.go: prefer
// the Bazel runfiles layout, fall back to walking up from the test's
// working directory until cue.mod/module.cue is reachable.
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

func loadComponents(t testing.TB) []projection.NamedMap {
	t.Helper()
	loaded, err := load.Topology(topologyDir(t), "prod")
	if err != nil {
		t.Fatalf("load topology: %v", err)
	}
	components, err := projection.Components(loaded)
	if err != nil {
		t.Fatalf("projection.Components: %v", err)
	}
	return components
}

func componentByName(t testing.TB, components []projection.NamedMap, name string) projection.NamedMap {
	t.Helper()
	for _, c := range components {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("component %q not found in prod topology", name)
	return projection.NamedMap{}
}

func unitByName(t testing.TB, component projection.NamedMap, name string) map[string]any {
	t.Helper()
	workload, ok := component.Value["workload"].(map[string]any)
	if !ok {
		t.Fatalf("%s.workload missing", component.Name)
	}
	rawUnits, _ := workload["units"].([]any)
	for _, raw := range rawUnits {
		unit, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if got, _ := unit["name"].(string); got == name {
			return unit
		}
	}
	t.Fatalf("%s.workload.units.%s not found", component.Name, name)
	return nil
}

func TestUnit_NomadServicesCarrySupervisorTelemetry(t *testing.T) {
	profile := componentByName(t, loadComponents(t), "profile_service")
	env, err := serviceenv.Unit(profile, unitByName(t, profile, "profile-service"))
	if err != nil {
		t.Fatalf("Unit: %v", err)
	}
	if env["VERSELF_SUPERVISOR"] != "nomad" {
		t.Fatalf("VERSELF_SUPERVISOR: got %q want nomad", env["VERSELF_SUPERVISOR"])
	}
	if env["OTEL_RESOURCE_ATTRIBUTES"] != "verself.supervisor=nomad" {
		t.Fatalf("OTEL_RESOURCE_ATTRIBUTES: got %q", env["OTEL_RESOURCE_ATTRIBUTES"])
	}
}

// TestEndpointsForUnit_PrimaryGetsAllEndpoints asserts the primary unit
// owns every endpoint declared on the component. profile_service is the
// minimal real fixture (single endpoint pair, no processes block).
func TestEndpointsForUnit_PrimaryGetsAllEndpoints(t *testing.T) {
	profile := componentByName(t, loadComponents(t), "profile_service")
	got, err := serviceenv.EndpointsForUnit(profile, map[string]any{"name": "profile-service"})
	if err != nil {
		t.Fatalf("EndpointsForUnit: %v", err)
	}
	want := []string{"internal_https", "public_http"}
	if !equalSorted(got, want) {
		t.Fatalf("primary endpoints: got %v, want %v", got, want)
	}
}

// TestEndpointsForUnit_NamedProcessOwnsOnlyDeclared is the F1
// regression guard. object-storage-admin is a named process whose
// `processes.admin.endpoints` declares only "admin_http"; the unit must
// NOT inherit "public_http". The previous nomad-side helper returned
// every component endpoint regardless of unit, which would force two
// TaskGroups to fight over the same ReservedPort label.
func TestEndpointsForUnit_NamedProcessOwnsOnlyDeclared(t *testing.T) {
	storage := componentByName(t, loadComponents(t), "object_storage_service")
	got, err := serviceenv.EndpointsForUnit(storage, map[string]any{"name": "object-storage-admin"})
	if err != nil {
		t.Fatalf("EndpointsForUnit: %v", err)
	}
	want := []string{"admin_http"}
	if !equalSorted(got, want) {
		t.Fatalf("named-process endpoints: got %v, want %v", got, want)
	}
}

// TestEndpointsForUnit_RecurringWorkerOwnsNothing covers the
// background-worker shape: sandbox-rental-recurring-worker has no
// declared endpoints and must own none. Returning the parent
// component's endpoints here would be the same multi-TaskGroup port
// collision that the named-process case prevents.
func TestEndpointsForUnit_RecurringWorkerOwnsNothing(t *testing.T) {
	sandbox := componentByName(t, loadComponents(t), "sandbox_rental")
	got, err := serviceenv.EndpointsForUnit(sandbox, map[string]any{"name": "sandbox-rental-recurring-worker"})
	if err != nil {
		t.Fatalf("EndpointsForUnit: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("worker endpoints: got %v, want empty", got)
	}
}

// TestEndpointsForUnit_NoOverlapBetweenUnitsOfOneComponent enforces the
// invariant the Nomad renderer relies on across all multi-unit
// components in the topology, regardless of supervisor: no two units of
// the same component own the same endpoint label. Once any of these
// components opts into nomad supervision, overlap would render an
// invalid spec.
func TestEndpointsForUnit_NoOverlapBetweenUnitsOfOneComponent(t *testing.T) {
	for _, c := range loadComponents(t) {
		workload, ok := c.Value["workload"].(map[string]any)
		if !ok {
			continue
		}
		rawUnits, _ := workload["units"].([]any)
		if len(rawUnits) < 2 {
			continue
		}
		owned := map[string][]string{}
		for _, raw := range rawUnits {
			unit, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			name, _ := unit["name"].(string)
			endpoints, err := serviceenv.EndpointsForUnit(c, unit)
			if err != nil {
				t.Fatalf("%s.units.%s: EndpointsForUnit: %v", c.Name, name, err)
			}
			owned[name] = endpoints
		}
		seen := map[string]string{}
		for unit, endpoints := range owned {
			for _, ep := range endpoints {
				if other, dup := seen[ep]; dup {
					t.Errorf("component %q: endpoint %q is owned by both %q and %q",
						c.Name, ep, other, unit)
				}
				seen[ep] = unit
			}
		}
	}
}

func equalSorted(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	x := append([]string(nil), a...)
	y := append([]string(nil), b...)
	sort.Strings(x)
	sort.Strings(y)
	for i := range x {
		if x[i] != y[i] {
			return false
		}
	}
	return true
}
