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
		"next-npm-single-app-fail",
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
	if fixtures[0].Metadata.Suite != FixtureSuitePass {
		t.Fatalf("fixture suite: got %q want %q", fixtures[0].Metadata.Suite, FixtureSuitePass)
	}
	if fixtures[0].Metadata.ExpectedResult != "success" {
		t.Fatalf("fixture expected result: got %q want success", fixtures[0].Metadata.ExpectedResult)
	}

	var failFixture Fixture
	for _, fixture := range fixtures {
		if fixture.Name == "next-npm-single-app-fail" {
			failFixture = fixture
			break
		}
	}
	if failFixture.Name == "" {
		t.Fatal("next-npm-single-app-fail fixture missing from matrix")
	}
	if failFixture.Metadata.ExpectedFailurePhase != "run" {
		t.Fatalf("fail fixture phase: got %q want run", failFixture.Metadata.ExpectedFailurePhase)
	}
	if failFixture.Metadata.ExpectedFailureExitCode != 1 {
		t.Fatalf("fail fixture exit code: got %d want 1", failFixture.Metadata.ExpectedFailureExitCode)
	}
	if failFixture.Metadata.ExpectedFailureMessageContains == "" {
		t.Fatal("fail fixture expected failure message is empty")
	}
}

func TestSelectFixturesBySuite(t *testing.T) {
	fixtures := []Fixture{
		{
			Name: "pass-fixture",
			Metadata: FixtureMetadata{
				Suite:          FixtureSuitePass,
				ExpectedResult: "success",
			},
		},
		{
			Name: "fail-fixture",
			Metadata: FixtureMetadata{
				Suite:          FixtureSuiteFail,
				ExpectedResult: "failure",
			},
		},
	}

	selected, suites, err := selectFixturesBySuite(fixtures, []string{"pass"})
	if err != nil {
		t.Fatalf("selectFixturesBySuite: %v", err)
	}
	if !reflect.DeepEqual(suites, []string{"pass"}) {
		t.Fatalf("suite names: got %v want [pass]", suites)
	}
	if len(selected) != 1 || selected[0].Name != "pass-fixture" {
		t.Fatalf("selected fixtures: got %#v", selected)
	}
}

func TestAssertFixtureExecOutcome(t *testing.T) {
	metadata := FixtureMetadata{
		ExpectedFailurePhase:           "run",
		ExpectedFailureExitCode:        1,
		ExpectedFailureMessageContains: "FORGE_METAL_FIXTURE_EXPECTED_TEST_FAILURE",
	}
	outcome := fixtureExecOutcome{
		FailurePhase:    "run",
		FailureExitCode: 1,
		GuestLogTail:    "Error: FORGE_METAL_FIXTURE_EXPECTED_TEST_FAILURE",
	}
	if err := assertFixtureExecOutcome(metadata, outcome); err != nil {
		t.Fatalf("assertFixtureExecOutcome: %v", err)
	}
}
