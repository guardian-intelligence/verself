package deployengine

import "testing"

func TestBootstrapModeCanUseR2ControlPlanePublishing(t *testing.T) {
	exec := execution{Options: Options{Bootstrap: true}}
	if !exec.bootstrapMode() {
		t.Fatal("explicit bootstrap run should use bootstrap Nomad sequencing")
	}
}
