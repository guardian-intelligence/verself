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
