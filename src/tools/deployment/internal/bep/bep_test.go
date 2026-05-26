package bep

import (
	"reflect"
	"strings"
	"testing"
)

func TestResolveOutputGroup(t *testing.T) {
	stream := &Stream{
		NamedSets: map[string]NamedSetOfFiles{
			"descriptor": {
				Files: []File{{
					Name:       "nomad_component.nomad_component.json",
					PathPrefix: []string{"bazel-out", "k8-fastbuild", "bin", "src", "service"},
				}},
			},
			"artifact": {
				Files: []File{{
					Name:       "service.tar",
					PathPrefix: []string{"bazel-out", "k8-fastbuild", "bin", "src", "service"},
				}},
			},
			"default": {
				FileSets: []string{"descriptor", "artifact"},
			},
		},
		Targets: map[string]TargetComplete{
			"//src/service:nomad_component": {
				Label:   "//src/service:nomad_component",
				Success: true,
				OutputGroups: []OutputGroup{
					{Name: "default", FileSets: []string{"default"}},
					{Name: "nomad_descriptor", FileSets: []string{"descriptor"}},
				},
			},
		},
	}

	got, err := stream.ResolveOutputGroup("//src/service:nomad_component", "nomad_descriptor", "/workspace")
	if err != nil {
		t.Fatalf("ResolveOutputGroup() error = %v", err)
	}
	want := []string{"/workspace/bazel-out/k8-fastbuild/bin/src/service/nomad_component.nomad_component.json"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ResolveOutputGroup() = %v, want %v", got, want)
	}

	defaults, err := stream.ResolveOutputs("//src/service:nomad_component", "/workspace")
	if err != nil {
		t.Fatalf("ResolveOutputs() error = %v", err)
	}
	if len(defaults) != 2 {
		t.Fatalf("ResolveOutputs() len = %d, want 2: %v", len(defaults), defaults)
	}
}

func TestResolveOutputGroupErrorsOnMissingGroup(t *testing.T) {
	stream := &Stream{
		NamedSets: map[string]NamedSetOfFiles{},
		Targets: map[string]TargetComplete{
			"//src/service:nomad_component": {
				Label:        "//src/service:nomad_component",
				Success:      true,
				OutputGroups: []OutputGroup{{Name: "default", FileSets: []string{"default"}}},
			},
		},
	}

	_, err := stream.ResolveOutputGroup("//src/service:nomad_component", "nomad_descriptor", "/workspace")
	if err == nil {
		t.Fatal("ResolveOutputGroup() error = nil")
	}
	if !strings.Contains(err.Error(), "has no nomad_descriptor output group") {
		t.Fatalf("ResolveOutputGroup() error = %q", err)
	}
}
