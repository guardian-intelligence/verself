package deployengine

import (
	"strings"
	"testing"

	"github.com/hashicorp/nomad/api"

	"github.com/verself/deployment-service/internal/deploymodel"
)

func TestApplyArtifactGetterSourcesAcceptsPublisherURLs(t *testing.T) {
	inputs := &deployInputs{
		Bindings: map[string]artifactBinding{
			"openbao-runtime": {
				Artifact: deploymodel.Artifact{Output: "openbao-runtime"},
			},
		},
	}
	err := applyArtifactGetterSources(inputs, map[string]string{
		"openbao-runtime": "http://127.0.0.1:7380/gamma/sha/openbao-runtime.tar",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := inputs.Bindings["openbao-runtime"].Artifact.GetterSource; !strings.HasPrefix(got, "http://127.0.0.1:7380/") {
		t.Fatalf("getter source = %q", got)
	}
}

func TestBindArtifactsInSpecPreservesAuthoredChownAndDisablesGeneratedRootChown(t *testing.T) {
	source := "verself-artifact://runtime"
	destination := "local"
	envSource := "verself-artifact://tools"
	job := &api.Job{
		TaskGroups: []*api.TaskGroup{
			{
				Tasks: []*api.Task{
					{
						User: "root",
						Artifacts: []*api.TaskArtifact{
							{
								GetterSource: &source,
								RelativeDest: &destination,
								Chown:        true,
							},
						},
						Env: map[string]string{
							"VERSELF_TOOLS": envSource,
						},
					},
				},
			},
		},
	}
	bindings := map[string]artifactBinding{
		"runtime": {
			Artifact: deploymodel.Artifact{GetterSource: "http://127.0.0.1:7380/runtime.tar"},
			Checksum: "sha256:runtime",
		},
		"tools": {
			Artifact: deploymodel.Artifact{GetterSource: "http://127.0.0.1:7380/tools.tar"},
			Checksum: "sha256:tools",
		},
	}

	seen, err := bindArtifactsInSpec(job, bindings)
	if err != nil {
		t.Fatal(err)
	}
	if !seen["runtime"] || !seen["tools"] {
		t.Fatalf("seen = %#v, want runtime and tools", seen)
	}
	task := job.TaskGroups[0].Tasks[0]
	if len(task.Artifacts) != 2 {
		t.Fatalf("artifact count = %d, want 2", len(task.Artifacts))
	}
	if !task.Artifacts[0].Chown {
		t.Fatalf("authored artifact chown = false, want preserved true")
	}
	if task.Artifacts[1].Chown {
		t.Fatalf("generated root artifact chown = true, want false")
	}
}

func TestTaskArtifactChownForNonRootUser(t *testing.T) {
	if !taskArtifactChown("app_user") {
		t.Fatal("non-root task artifact chown = false, want true")
	}
	if taskArtifactChown("root") {
		t.Fatal("root task artifact chown = true, want false")
	}
	if taskArtifactChown(" ") {
		t.Fatal("empty task artifact chown = true, want false")
	}
}
