package toolcatalog

import (
	"strings"
	"testing"
)

func TestResolveReturnsExecutable(t *testing.T) {
	resolved, err := ResolveFrom(Catalog{
		Tools: map[string]Tool{
			"bazel": {
				Platforms: map[string]PlatformTool{
					"linux/amd64": {Executable: "bazel"},
				},
			},
		},
	}, "bazel", "linux/amd64")
	if err != nil {
		t.Fatalf("ResolveFrom: %v", err)
	}
	if resolved.Name != "bazel" || resolved.Platform != "linux/amd64" || resolved.Executable != "bazel" {
		t.Fatalf("resolved = %#v, want bazel/linux/amd64/bazel", resolved)
	}
}

func TestResolveRejectsUnknownPlatform(t *testing.T) {
	_, err := ResolveFrom(Catalog{
		Tools: map[string]Tool{
			"bazel": {Platforms: map[string]PlatformTool{"linux/amd64": {Executable: "bazel"}}},
		},
	}, "bazel", "darwin/arm64")
	if err == nil || !strings.Contains(err.Error(), "no platform") {
		t.Fatalf("error = %v, want unknown-platform rejection", err)
	}
}

func TestResolveRejectsMissingExecutable(t *testing.T) {
	_, err := ResolveFrom(Catalog{
		Tools: map[string]Tool{
			"bazel": {Platforms: map[string]PlatformTool{"linux/amd64": {Executable: ""}}},
		},
	}, "bazel", "linux/amd64")
	if err == nil || !strings.Contains(err.Error(), "executable is required") {
		t.Fatalf("error = %v, want missing-executable rejection", err)
	}
}
