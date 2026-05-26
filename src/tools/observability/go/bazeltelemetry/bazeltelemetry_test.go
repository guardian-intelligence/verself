package bazeltelemetry

import "testing"

func TestParseWorkspaceLabel(t *testing.T) {
	parts := ParseLabel("//src/viteplus-monorepo/apps/verself-web:build")
	if parts.Package != "src/viteplus-monorepo/apps/verself-web" {
		t.Fatalf("package = %q", parts.Package)
	}
	if parts.RuleName != "build" {
		t.Fatalf("rule = %q", parts.RuleName)
	}
	if parts.BuildFile != "src/viteplus-monorepo/apps/verself-web/BUILD.bazel" {
		t.Fatalf("build file = %q", parts.BuildFile)
	}
}

func TestPackageParts(t *testing.T) {
	parts := PackageParts("src/viteplus-monorepo/apps/company")
	if parts.BuildFile != "src/viteplus-monorepo/apps/company/BUILD.bazel" {
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
