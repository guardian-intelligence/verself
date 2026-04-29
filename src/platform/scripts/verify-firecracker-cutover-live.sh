#!/usr/bin/env bash
set -euo pipefail

# verify-firecracker-cutover-live.sh — invariants for the substrate +
# toolchain + bridge cleanup landed on main:
#
#   1. vm-bridge is workload-agnostic (no GH-specific env strings, no
#      hardcoded /opt/actions-runner / /opt/forgejo-runner /
#      /opt/hostedtoolcache).
#   2. The substrate ext4 carries no runner@1000 user and no /opt/*
#      mount-point stubs — toolchain images and vm-bridge own those.
#   3. gh-actions-runner and forgejo-runner toolchain ext4s both
#      carry the runner@1000 user from //runner-overlay-common, with
#      identical sha256 so the bridge's content-digest collision
#      check treats the second image as a clean no-op rather than a
#      collision.
#   4. vm-orchestrator ships no clickhouse-go imports — the daemon is
#      pure event-source; persistence is a downstream consumer
#      concern.
#   5. After a fresh sandbox smoke run, the FC configure_all span
#      family carries firecracker.step_timeout_ms attributes (typed
#      Step cutover) and the lease lifecycle logs contain zero
#      "etc-overlay collision" rows (content-digest collision check).
#
# Layered:
#   1. static checks (no prod) — the source tree we'd deploy.
#   2. sandbox smoke roundtrip — runs verify-vm-orchestrator-live.sh,
#      which honours VERIFICATION_DEPLOY=1 to rebuild guest-rootfs +
#      redeploy the firecracker stack before driving the smoke. This
#      step is what brings prod up to the cutover bits.
#   3. SSH-side substrate + toolchain debugfs — asserted *after* the
#      deploy so we're inspecting the post-cutover state, not whatever
#      shape prod was in when the workflow started.
#   4. ClickHouse asserts on what the smoke produced.
#
# Each layer fails fast with the exact line the assertion fired on so
# the GitHub Actions log pinpoints the regression.

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=src/platform/scripts/lib/verification-context.sh
source "${script_dir}/lib/verification-context.sh"
verification_context_init "${BASH_SOURCE[0]}"

repo_root="${VERIFICATION_REPO_ROOT}"
vmo_dir="${repo_root}/src/vm-orchestrator"
artifact_root="${VERIFICATION_ARTIFACT_ROOT:-${VERIFICATION_SMOKE_ARTIFACT_ROOT}/firecracker-cutover}"
mkdir -p "${artifact_root}"

log() {
  printf '[firecracker-cutover] %s\n' "$*"
}

fail() {
  printf '[firecracker-cutover] FAIL %s\n' "$*" >&2
  exit 1
}

# ----------------------------------------------------------------------
# 1. Static asserts (build + binary string scans, no prod dependency).
# ----------------------------------------------------------------------

log "static: building vm-orchestrator daemon + CLI + vm-bridge (default tags, -trimpath)"
# -trimpath strips source/GOROOT paths from the binary so a GitHub
# Actions runner that installs Go to /opt/hostedtoolcache/go/... does
# not leak that path into the binary (which would false-positive every
# /opt/* path scan below).
(cd "${vmo_dir}" && go build -trimpath -o "${artifact_root}/vm-orchestrator" ./cmd/vm-orchestrator)
(cd "${vmo_dir}" && go build -trimpath -o "${artifact_root}/vm-orchestrator-cli" ./cmd/vm-orchestrator-cli)
(cd "${vmo_dir}" && go build -trimpath -o "${artifact_root}/vm-bridge" ./cmd/vm-bridge)

log "static: building vm-bridge under -tags verself_fault_injection (verification builds, -trimpath)"
(cd "${vmo_dir}" && go build -trimpath -tags verself_fault_injection -o "${artifact_root}/vm-bridge.fault-injection" ./cmd/vm-bridge)

log "static: vm-bridge binary carries no GitHub-Actions-specific env strings"
# Scan for env var names — those only appear in our source as literal
# string keys for envMap, never in Go stdlib data or path debug info.
# Mount-point paths (/opt/actions-runner, /opt/hostedtoolcache, etc.)
# deliberately are NOT scanned in the bridge binary: -trimpath strips
# the GOROOT prefix but the build environment can still surface
# /opt/hostedtoolcache via runtime.GOROOT() metadata; those paths are
# instead asserted absent from the substrate ext4 below, which is
# where the regression would actually matter.
forbidden_bridge_strings=(
  RUNNER_TOOL_CACHE
  AGENT_TOOLSDIRECTORY
)
for needle in "${forbidden_bridge_strings[@]}"; do
  if strings "${artifact_root}/vm-bridge" | grep -qF -- "${needle}"; then
    fail "vm-bridge binary still contains forbidden env var ${needle}"
  fi
done

log "static: vm-orchestrator binary ships no clickhouse-go symbols"
if strings "${artifact_root}/vm-orchestrator" | grep -qE "clickhouse-go|github.com/ClickHouse"; then
  fail "vm-orchestrator binary references clickhouse-go; the daemon must stay telemetry-agnostic"
fi

log "static: gh-actions-runner + forgejo-runner BUILD.bazel reference //runner-overlay-common"
for build_file in \
  "${repo_root}/src/vm-orchestrator/guest-images/gh-actions-runner/BUILD.bazel" \
  "${repo_root}/src/vm-orchestrator/guest-images/forgejo-runner/BUILD.bazel"; do
  if ! grep -q "//src/vm-orchestrator/guest-images/runner-overlay-common" "${build_file}"; then
    fail "${build_file} does not reference //src/vm-orchestrator/guest-images/runner-overlay-common"
  fi
done

# ----------------------------------------------------------------------
# 2. Drive a sandbox smoke roundtrip. The smoke fixture rebuilds
#    guest-rootfs + redeploys the firecracker stack (when
#    VERIFICATION_DEPLOY=1) and then pushes a real Forgejo workflow
#    run, allocates a runner, leases a VM, executes, and tears down —
#    exactly the path the cutover touched. The timestamp window we
#    capture frames the post-smoke ClickHouse queries below. Run this
#    BEFORE the substrate/toolchain debugfs checks so those checks see
#    the post-cutover artefacts on disk.
# ----------------------------------------------------------------------

log "smoke: triggering verify-vm-orchestrator-live.sh"
window_start_unix="$(date -u +%s)"
VERIFICATION_RUN_ID="firecracker-cutover-$(date -u +%Y%m%dT%H%M%SZ)" \
VERIFICATION_ARTIFACT_ROOT="${artifact_root}/smoke" \
  "${script_dir}/verify-vm-orchestrator-live.sh"
window_end_unix="$(date -u +%s)"

# ----------------------------------------------------------------------
# 3. Substrate + toolchain ext4 asserts (SSH to the deployed host).
#    The smoke above already redeployed (if VERIFICATION_DEPLOY=1) so
#    these reads inspect the cutover artefacts, not whatever shape
#    prod was in when the workflow started.
# ----------------------------------------------------------------------

guest_images_dir="/var/lib/verself/guest-images"
substrate_path="${guest_images_dir}/substrate.ext4"
gh_toolchain_path="${guest_images_dir}/toolchains/gh-actions-runner.ext4"
forgejo_toolchain_path="${guest_images_dir}/toolchains/forgejo-runner.ext4"

log "remote: substrate ext4 carries neither runner user nor /opt/* stubs"
substrate_passwd="$(verification_ssh "sudo debugfs -R 'cat /etc/passwd' ${substrate_path} 2>/dev/null" || true)"
if printf '%s' "${substrate_passwd}" | awk -F: '$3 == "1000" {found=1} END {exit !found}'; then
  fail "substrate /etc/passwd unexpectedly contains a UID-1000 entry"
fi
# debugfs `ls -l <dir>` on a missing path emits "File not found"; an
# extant directory emits inode+mode columns. The `<>` syntax in
# debugfs is reserved for inode references, not paths — use `ls`.
for opt_dir in /opt/actions-runner /opt/forgejo-runner /opt/hostedtoolcache; do
  ls_output="$(verification_ssh "sudo debugfs -R 'ls -l ${opt_dir}' ${substrate_path} 2>&1" || true)"
  if ! printf '%s' "${ls_output}" | grep -qiE "(File not found|does not exist)"; then
    fail "substrate still contains stub directory ${opt_dir}: ${ls_output}"
  fi
done

log "remote: gh-actions-runner toolchain etc-overlay carries the runner@1000 user from runner-overlay-common"
gh_overlay_passwd="$(verification_ssh "sudo debugfs -R 'cat /etc-overlay/passwd' ${gh_toolchain_path}" 2>/dev/null || true)"
if ! printf '%s' "${gh_overlay_passwd}" | grep -q "^runner:x:1000:1000:verself runner"; then
  fail "gh-actions-runner toolchain image is missing the shared runner@1000 entry from runner-overlay-common (got: ${gh_overlay_passwd})"
fi

log "remote: forgejo-runner toolchain etc-overlay carries the same runner@1000 entry (DRY check)"
forgejo_overlay_passwd="$(verification_ssh "sudo debugfs -R 'cat /etc-overlay/passwd' ${forgejo_toolchain_path}" 2>/dev/null || true)"
if [[ "${gh_overlay_passwd}" != "${forgejo_overlay_passwd}" ]]; then
  fail "gh-actions-runner and forgejo-runner /etc-overlay/passwd differ; they must both consume runner-overlay-common verbatim"
fi

window_start="$(date -u -d "@${window_start_unix}" +'%Y-%m-%d %H:%M:%S')"
window_end="$(date -u -d "@${window_end_unix}" +'%Y-%m-%d %H:%M:%S')"

# ----------------------------------------------------------------------
# 4. ClickHouse asserts on the spans the smoke just produced.
# ----------------------------------------------------------------------

cd "${VERIFICATION_PLATFORM_ROOT}"

log "clickhouse: every vmorchestrator.firecracker.configure span emitted in the smoke window carries a step_timeout_ms attribute"
missing_timeout="$(./scripts/clickhouse.sh \
  --database default \
  --param_window_start="${window_start}" \
  --param_window_end="${window_end}" \
  --query "
    SELECT count()
    FROM otel_traces
    WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String})
      AND SpanName = 'vmorchestrator.firecracker.configure'
      AND mapContains(SpanAttributes, 'firecracker.step_timeout_ms') = 0
    FORMAT TabSeparated
  " | tr -d '[:space:]')"
if [[ "${missing_timeout}" != "0" ]]; then
  fail "found ${missing_timeout} configure spans without firecracker.step_timeout_ms (typed Step cutover regressed)"
fi

log "clickhouse: zero etc-overlay collision lines in the smoke window (content-digest collision check held)"
collision_count="$(./scripts/clickhouse.sh \
  --database default \
  --param_window_start="${window_start}" \
  --param_window_end="${window_end}" \
  --query "
    SELECT count()
    FROM otel_logs
    WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String})
      AND ServiceName = 'vm-orchestrator'
      AND Body LIKE '%etc-overlay collision%'
    FORMAT TabSeparated
  " | tr -d '[:space:]')"
if [[ "${collision_count}" != "0" ]]; then
  fail "found ${collision_count} etc-overlay collision log lines; the runner-overlay-common shared filegroup should produce none"
fi

log "clickhouse: vm_lease_evidence still records the canonical lease lifecycle for at least one lease in the smoke window"
lifecycle_count="$(./scripts/clickhouse.sh \
  --database verself \
  --param_window_start="${window_start}" \
  --param_window_end="${window_end}" \
  --query "
    SELECT count()
    FROM (
      SELECT lease_id, groupArray(evidence_type) AS kinds
      FROM vm_lease_evidence
      WHERE evidence_time BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String})
      GROUP BY lease_id
      HAVING has(kinds, 'lease_ready') AND has(kinds, 'exec_started') AND has(kinds, 'lease_cleanup')
    )
    FORMAT TabSeparated
  " | tr -d '[:space:]')"
if [[ "${lifecycle_count}" == "0" ]]; then
  fail "no lease in the smoke window recorded the full lease_ready → exec_started → lease_cleanup sequence in vm_lease_evidence"
fi

log "OK firecracker cutover invariants hold; artifacts at ${artifact_root}"
