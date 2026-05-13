"""Package-owned Go checks used by the Aspect verification lane."""

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

def _check_cmd(package_name, setup, invocation):
    return """set -euo pipefail
out="$$PWD/$@"
go_tool="/usr/local/go/bin/go"
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
export GOFLAGS="-mod=readonly"
export GOTOOLCHAIN=local
{invocation}
touch "$$out"
""".format(
        invocation = invocation,
        package_name = package_name,
        setup = setup,
    )

def _check(name, srcs, tag, setup, invocation, tools = []):
    native.genrule(
        name = name,
        srcs = srcs,
        outs = [name + ".stamp"],
        cmd = _check_cmd(native.package_name(), setup, invocation),
        tags = _CHECK_TAGS + [tag],
        tools = tools,
    )

def go_module_checks(name = "go_checks"):
    """Installs vet, lint, and G115 conversion checks for a Go module root."""

    native.filegroup(
        name = name + "_sources",
        srcs = native.glob(
            _GO_MODULE_SOURCE_PATTERNS,
            allow_empty = True,
            exclude = _GO_MODULE_SOURCE_EXCLUDES,
        ),
    )

    sources = [":" + name + "_sources"]

    _check(
        name = "go_vet_check",
        srcs = sources,
        tag = GO_VET_TAG,
        setup = "",
        invocation = '"$$go_tool" vet ./...',
    )

    _check(
        name = "go_golangci_lint_check",
        srcs = sources,
        tag = GOLANGCI_LINT_TAG,
        setup = 'tool="$$PWD/$(execpath @com_github_golangci_golangci_lint_v2//cmd/golangci-lint:golangci-lint)"',
        invocation = '"$$tool" run --allow-parallel-runners ./...',
        tools = ["@com_github_golangci_golangci_lint_v2//cmd/golangci-lint:golangci-lint"],
    )

    _check(
        name = "go_gosec_g115_check",
        srcs = sources,
        tag = GOSEC_G115_TAG,
        setup = 'tool="$$PWD/$(execpath @com_github_securego_gosec_v2//cmd/gosec:gosec)"',
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
