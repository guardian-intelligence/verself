"""Package-owned Go checks used by the Aspect verification lane."""

load("//src/tools/build:check_tests.bzl", "stamp_test")

GO_CHECK_TAG = "go_module_check"
GOSEC_G115_TAG = "go_gosec_g115_check"
GOLANGCI_LINT_TAG = "go_golangci_lint_check"
GO_VET_TAG = "go_vet_check"

_GO_MODULE_SOURCE_PATTERNS = [
    "**/*.go",
    "**/*.c",
    "**/*.h",
    "**/*.s",
    "**/*.sql",
    "**/*.zed",
    "**/*.proto",
    "go.mod",
    "go.sum",
    ".golangci.yml",
    ".golangci.yaml",
    ".golangci.toml",
    ".golangci.json",
]

_GO_MODULE_SOURCE_EXCLUDES = [
    "bazel-bin/**",
    "bazel-out/**",
    "bazel-testlogs/**",
    "bazel-verself/**",
    "node_modules/**",
    "vendor/**",
]

_CHECK_TAGS = [
    "repo_check",
    GO_CHECK_TAG,
    "local",
    "manual",
    "no-remote",
    "no-sandbox",
]

_CHECK_TEST_TAGS = [
    "repo_check",
    GO_CHECK_TAG,
    "local",
    "no-remote",
    "no-sandbox",
]

def _check_cmd(package_name, setup, package_setup, invocation):
    return """set -euo pipefail
out="$$PWD/$@"
execroot="$$PWD"
go_archive="$$PWD/$(location @dev_tool_go//file)"
go_tmp="$$(mktemp -d)"
trap 'rm -rf "$$go_tmp"' EXIT
tar -xzf "$$go_archive" -C "$$go_tmp"
go_tool="$$go_tmp/go/bin/go"
test -x "$$go_tool"
go_tool_dir="$$(dirname "$$go_tool")"
export PATH="$$go_tool_dir:$${{PATH:-}}"
if [ -z "$${{HOME:-}}" ]; then
  HOME="$$(eval printf %s ~)"
fi
if [ -z "$${{GOPATH:-}}" ]; then
  GOPATH="$$HOME/go"
fi
if [ -z "$${{GOMODCACHE:-}}" ]; then
  GOMODCACHE="$$GOPATH/pkg/mod"
fi
if [ -z "$${{GOCACHE:-}}" ]; then
  GOCACHE="$$HOME/.cache/go-build"
fi
export HOME GOPATH GOMODCACHE GOCACHE
{setup}
cd "{package_name}"
{package_setup}
export GOFLAGS="-mod=readonly"
export GOTOOLCHAIN=local
packages="$$("$$go_tool" list ./...)"
if [ -z "$$packages" ]; then
  touch "$$out"
  exit 0
fi
{invocation}
touch "$$out"
""".format(
        invocation = invocation,
        package_name = package_name,
        package_setup = package_setup,
        setup = setup,
    )

def _check(name, test_name, srcs, tag, setup, package_setup, invocation, tools = []):
    native.genrule(
        name = name,
        srcs = srcs,
        outs = [name + ".stamp"],
        cmd = _check_cmd(native.package_name(), setup, package_setup, invocation),
        tags = _CHECK_TAGS + [tag],
        tools = ["@dev_tool_go//file"] + tools,
    )
    stamp_test(
        name = test_name,
        target = ":" + name,
        tags = _CHECK_TEST_TAGS + [tag],
    )

def go_module_checks(name = "go_checks", extra_srcs = [], package_setup = ""):
    """Installs vet, lint, and G115 conversion checks for a Go module root.

    Args:
      name: Filegroup name that aggregates the generated check targets.
      extra_srcs: Additional files produced by Bazel rules that plain Go
        tooling needs in its package tree, such as go:embed inputs.
      package_setup: Shell setup run from the Go module root before checks.
    """

    native.filegroup(
        name = name + "_sources",
        srcs = native.glob(
            _GO_MODULE_SOURCE_PATTERNS,
            allow_empty = True,
            exclude = _GO_MODULE_SOURCE_EXCLUDES,
        ),
    )

    sources = [":" + name + "_sources"] + extra_srcs

    _check(
        name = "go_vet_check",
        test_name = "go_vet_test",
        srcs = sources,
        tag = GO_VET_TAG,
        setup = "",
        package_setup = package_setup,
        invocation = '"$$go_tool" vet ./...',
    )

    _check(
        name = "go_golangci_lint_check",
        test_name = "go_golangci_lint_test",
        srcs = sources,
        tag = GOLANGCI_LINT_TAG,
        setup = 'tool="$$PWD/$(execpath @com_github_golangci_golangci_lint_v2//cmd/golangci-lint:golangci-lint)"',
        package_setup = package_setup,
        invocation = '"$$tool" run --allow-parallel-runners ./...',
        tools = ["@com_github_golangci_golangci_lint_v2//cmd/golangci-lint:golangci-lint"],
    )

    _check(
        name = "go_gosec_g115_check",
        test_name = "go_gosec_g115_test",
        srcs = sources,
        tag = GOSEC_G115_TAG,
        setup = 'tool="$$PWD/$(execpath @com_github_securego_gosec_v2//cmd/gosec:gosec)"',
        package_setup = package_setup,
        invocation = '"$$tool" -quiet -include=G115 ./...',
        tools = ["@com_github_securego_gosec_v2//cmd/gosec:gosec"],
    )

    native.filegroup(
        name = name,
        srcs = [
            ":go_golangci_lint_check",
            ":go_gosec_g115_check",
            ":go_vet_check",
        ],
        tags = _CHECK_TAGS,
    )
