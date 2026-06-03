package deployengine

import (
	"strings"
	"testing"

	"github.com/verself/deployment-service/internal/deploymodel"
)

func TestApplyArtifactGetterSourcesAcceptsBootstrapFileURLs(t *testing.T) {
	inputs := &deployInputs{
		Bindings: map[string]artifactBinding{
			"openbao-runtime": {
				Artifact: deploymodel.Artifact{Output: "openbao-runtime"},
			},
		},
	}
	err := applyArtifactGetterSources(inputs, map[string]string{
		"openbao-runtime": "file:///var/lib/verself/bootstrap/artifacts/gamma/sha/openbao-runtime.tar",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := inputs.Bindings["openbao-runtime"].Artifact.GetterSource; !strings.HasPrefix(got, "file:///") {
		t.Fatalf("getter source = %q", got)
	}
}
