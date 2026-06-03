package deployengine

import (
	"strings"
	"testing"

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
