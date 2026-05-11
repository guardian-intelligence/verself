package bazeltelemetry

import "testing"

func TestParseWorkspaceLabel(t *testing.T) {
	parts := ParseLabel("//src/websites/apps/verself-web:build")
	if parts.Package != "src/websites/apps/verself-web" {
		t.Fatalf("package = %q", parts.Package)
	}
	if parts.RuleName != "build" {
		t.Fatalf("rule = %q", parts.RuleName)
	}
	if parts.BuildFile != "src/websites/apps/verself-web/BUILD.bazel" {
		t.Fatalf("build file = %q", parts.BuildFile)
	}
}

func TestPackageParts(t *testing.T) {
	parts := PackageParts("src/websites/apps/company")
	if parts.BuildFile != "src/websites/apps/company/BUILD.bazel" {
		t.Fatalf("build file = %q", parts.BuildFile)
	}
}

func TestParseCanonicalRepoPackage(t *testing.T) {
	parts := PackageParts("@@bazel_tools//tools")
	if parts.Repository != "bazel_tools" {
		t.Fatalf("repo = %q", parts.Repository)
	}
	if parts.BuildFile != "@@bazel_tools//tools/BUILD.bazel" {
		t.Fatalf("build file = %q", parts.BuildFile)
	}
}
