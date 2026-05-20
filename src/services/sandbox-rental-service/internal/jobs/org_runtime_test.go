package jobs

import (
	"reflect"
	"testing"

	vmorchestrator "github.com/verself/vm-orchestrator"
)

func TestImageRefsFromFilesystemMountsIncludesSubstrateAndImageMounts(t *testing.T) {
	got := imageRefsFromFilesystemMounts([]vmorchestrator.FilesystemMount{
		{SourceRef: "toolchain-base"},
		{SourceRef: "pool/orgs/org_a/goldens/scope/generations/gen-a@sealed"},
		{SourceRef: ""},
		{SourceRef: "cache-base"},
		{SourceRef: "toolchain-base"},
	})
	want := []string{"cache-base", "substrate", "toolchain-base"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("image refs = %#v, want %#v", got, want)
	}
}
