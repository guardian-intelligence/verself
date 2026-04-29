#!/usr/bin/env bash
set -euo pipefail

# verify-firecracker-cutover-static.sh — source-tree-only assertions
# for the substrate + toolchain + bridge cleanup. Runs anywhere a Go
# toolchain is available; no SSH, no Ansible, no SOPS, no prod
# tooling. Intended for the Blacksmith CI smoke; the live deploy +
# ClickHouse-trace verification is done separately by the operator
# via `make sandbox-smoke-test`.
#
# Asserts:
#   1. vm-orchestrator daemon, CLI, and vm-bridge build under both
#      default and verself_fault_injection tags with -trimpath.
#   2. The built vm-bridge binary contains no GitHub-Actions-specific
#      env strings (RUNNER_TOOL_CACHE, AGENT_TOOLSDIRECTORY).
#   3. The built vm-orchestrator binary ships zero clickhouse-go
#      symbols (the daemon stays telemetry-agnostic).
#   4. gh-actions-runner and forgejo-runner BUILD.bazel files both
#      reference //src/vm-orchestrator/guest-images/runner-overlay-common
#      (the shared runner@1000 fixture filegroup).

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
vmo_dir="${repo_root}/src/vm-orchestrator"
artifact_root="${VERIFICATION_ARTIFACT_ROOT:-${repo_root}/smoke-artifacts/firecracker-cutover-static}"
mkdir -p "${artifact_root}"

log() { printf '[firecracker-cutover-static] %s\n' "$*"; }
fail() {
  printf '[firecracker-cutover-static] FAIL %s\n' "$*" >&2
  exit 1
}

log "build vm-orchestrator daemon + CLI + vm-bridge (default tags, -trimpath)"
(cd "${vmo_dir}" && go build -trimpath -o "${artifact_root}/vm-orchestrator" ./cmd/vm-orchestrator)
(cd "${vmo_dir}" && go build -trimpath -o "${artifact_root}/vm-orchestrator-cli" ./cmd/vm-orchestrator-cli)
(cd "${vmo_dir}" && go build -trimpath -o "${artifact_root}/vm-bridge" ./cmd/vm-bridge)

log "build vm-bridge under -tags verself_fault_injection (verification builds)"
(cd "${vmo_dir}" && go build -trimpath -tags verself_fault_injection -o "${artifact_root}/vm-bridge.fault-injection" ./cmd/vm-bridge)

log "vm-bridge binary carries no GitHub-Actions-specific env strings"
forbidden_bridge_strings=(
  RUNNER_TOOL_CACHE
  AGENT_TOOLSDIRECTORY
)
for needle in "${forbidden_bridge_strings[@]}"; do
  if strings "${artifact_root}/vm-bridge" | grep -qF -- "${needle}"; then
    fail "vm-bridge binary still contains forbidden env var ${needle}"
  fi
done

log "vm-orchestrator binary ships no clickhouse-go symbols"
if strings "${artifact_root}/vm-orchestrator" | grep -qE "clickhouse-go|github.com/ClickHouse"; then
  fail "vm-orchestrator binary references clickhouse-go; the daemon must stay telemetry-agnostic"
fi

log "gh-actions-runner + forgejo-runner BUILD.bazel reference //runner-overlay-common"
for build_file in \
  "${repo_root}/src/vm-orchestrator/guest-images/gh-actions-runner/BUILD.bazel" \
  "${repo_root}/src/vm-orchestrator/guest-images/forgejo-runner/BUILD.bazel"; do
  if ! grep -q "//src/vm-orchestrator/guest-images/runner-overlay-common" "${build_file}"; then
    fail "${build_file} does not reference //src/vm-orchestrator/guest-images/runner-overlay-common"
  fi
done

log "OK static cutover invariants hold; artifacts at ${artifact_root}"
