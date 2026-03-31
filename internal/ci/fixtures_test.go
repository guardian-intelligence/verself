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
}
