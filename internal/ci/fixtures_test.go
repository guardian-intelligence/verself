package ci

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestLoadFixtures_FixtureMatrix(t *testing.T) {
	root := filepath.Join("..", "..", "test", "fixtures")
	fixtures, err := LoadFixtures(root)
	if err != nil {
		t.Fatalf("LoadFixtures: %v", err)
	}

	var names []string
	for _, fixture := range fixtures {
		names = append(names, fixture.Name)
	}

	want := []string{
		"next-bun-monorepo",
		"next-npm-single-app",
		"next-npm-workspaces",
		"next-pnpm-postgres",
	}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("fixture names: got %v want %v", names, want)
	}
	if fixtures[0].Metadata.DefaultBranch != "main" {
		t.Fatalf("fixture metadata default branch: got %q", fixtures[0].Metadata.DefaultBranch)
	}
	if fixtures[0].Metadata.Description == "" {
		t.Fatalf("fixture metadata description is empty")
	}
}
