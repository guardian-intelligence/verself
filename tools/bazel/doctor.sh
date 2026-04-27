#!/usr/bin/env bash
set -euo pipefail

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

require_file ".bazelversion"
require_file ".bazeliskrc"
require_file ".bazelrc"
require_file "MODULE.bazel"
require_file "bazel.go.work"
require_absent "bazel/go/go.mod"
require_absent "go.work"
require_absent "package.json"
require_absent "pnpm-lock.yaml"

grep -qx "9.1.0" .bazelversion
grep -qx "USE_BAZEL_VERSION=9.1.0" .bazeliskrc
grep -qx "BAZELISK_VERIFY_SHA256=a667454f3f4f8878df8199136b82c199f6ada8477b337fae3b1ef854f01e4e2f" .bazeliskrc
grep -qx "artifacts/" .gitignore
grep -qx "proof-artifacts/" .gitignore

if [[ "${1:-}" == "--emit-proof-span" ]]; then
  run_id="${VERIFICATION_RUN_ID:-bazel-doctor-$(date -u +%Y%m%dT%H%M%SZ)}"
  attrs="$(
    RUN_ID="${run_id}" python3 - <<'PY'
import json
import os

print(json.dumps({
    "verself.verification_run": os.environ["RUN_ID"],
    "verself.deploy_id": os.environ.get("VERSELF_DEPLOY_ID", ""),
    "verself.deploy_run_key": os.environ.get("VERSELF_DEPLOY_RUN_KEY", ""),
    "verself.deploy_kind": os.environ.get("VERSELF_DEPLOY_KIND", "bazel-proof"),
    "build.system": "bazel",
    "bazel.version": "9.1.0",
    "bazelisk.version": "1.28.1",
    "artifact.root": "artifacts",
    "proof_artifact.root": "proof-artifacts",
}))
PY
  )"
  PROOF_SPAN_SERVICE="bazel-doctor" \
    PROOF_SPAN_NAME="bazel.doctor" \
    PROOF_SPAN_ATTRS_JSON="${attrs}" \
    bash -c 'cd src/otel && go run ./cmd/proof-span'
  echo "${run_id}"
else
  echo "bazel doctor ok"
fi
