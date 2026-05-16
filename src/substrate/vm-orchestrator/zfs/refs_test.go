package zfs

import "testing"

func TestImageSnapshotRef(t *testing.T) {
	img, err := NewImage(Roots{Pool: "pool", ImageDataset: "images"}, "gh-actions-runner")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := img.Snapshot().String(), "pool/images/gh-actions-runner@ready"; got != want {
		t.Fatalf("snapshot = %q, want %q", got, want)
	}
}

func TestRejectsHostPathRefs(t *testing.T) {
	for _, ref := range []string{"", "../x", "a/b", "a@b", "-bad"} {
		if IsValidRef(ref) {
			t.Fatalf("IsValidRef(%q) = true", ref)
		}
	}
}

func TestGenerationRoundTripsOrgScopedRef(t *testing.T) {
	roots := Roots{Pool: "pool", OrgDataset: "orgs", GoldenDataset: "goldens"}
	volume, err := NewVolume(roots, "org_a", "scope-a")
	if err != nil {
		t.Fatal(err)
	}
	dataset, err := volume.GenerationDataset("generation-a")
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseGeneration(roots, dataset+"@sealed")
	if err != nil {
		t.Fatal(err)
	}
	if got.Volume().OrgID() != "org_a" || got.Volume().ID() != "scope-a" {
		t.Fatalf("parsed generation volume = %s/%s, want org_a/scope-a", got.Volume().OrgID(), got.Volume().ID())
	}
}
