#!/usr/bin/env bash
set -euo pipefail

if [[ "$#" -ne 0 ]]; then
  echo "usage: bazelisk run //tools/bazel:doctor" >&2
  exit 2
fi

workspace="${BUILD_WORKSPACE_DIRECTORY:-}"
if [[ -z "${workspace}" ]]; then
  workspace="$(git rev-parse --show-toplevel)"
fi

cd "${workspace}"

require_file() {
  local path="$1"
  if [[ ! -f "${path}" ]]; then
    echo "missing ${path}" >&2
    exit 1
  fi
}

require_absent() {
  local path="$1"
  if [[ -e "${path}" ]]; then
    echo "unexpected root ${path}" >&2
    exit 1
  fi
}

require_line() {
  local path="$1"
  local line="$2"
  if ! grep -qx -- "${line}" "${path}"; then
    echo "${path} missing required line: ${line}" >&2
    exit 1
  fi
}

require_file ".bazelversion"
require_file ".bazeliskrc"
require_file ".bazelrc"
require_file "MODULE.bazel"
require_file "bazel.go.work"
require_absent "bazel/go/go.mod"
require_absent "go.work"
require_absent "package.json"
require_absent "pnpm-lock.yaml"

require_line ".bazelversion" "9.1.0"
require_line ".bazeliskrc" "USE_BAZEL_VERSION=9.1.0"
require_line ".bazeliskrc" "BAZELISK_VERIFY_SHA256=a667454f3f4f8878df8199136b82c199f6ada8477b337fae3b1ef854f01e4e2f"
require_line ".bazelrc" "common --enable_bzlmod"
require_line ".bazelrc" "common --noenable_workspace"
require_line ".bazelrc" "common --lockfile_mode=error"
require_line ".gitignore" "artifacts/"
require_line ".gitignore" "smoke-artifacts/"

echo "bazel doctor ok"
