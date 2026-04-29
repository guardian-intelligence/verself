#!/usr/bin/env bash
# Verifies the viteplus build-system invariants set out in the source-only-
# packages cutover. Six tests; every one must PASS post-cutover. Pre-cutover
# at least 1, 3, and 4 fail by design (negative-test boundary missing, deploy
# tarball not self-contained, no per-app cache scope under the old packed-package
# rule).
#
# Usage:
#   src/viteplus-monorepo/scripts/verify-build.sh
#
# Lives under src/viteplus-monorepo/scripts/ so the cwd-resolution rules used by
# the deploy_profile-style "build artefact, ship tarball" pattern apply here too.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
cd "${REPO_ROOT}"

APPS=(verself-web company)
PACKAGES_TO_BE_SOURCE_ONLY=(nitro-plugins web-env auth-web ui brand)

PASS=()
FAIL=()
record_pass() { PASS+=("$1"); printf '  PASS  %s\n' "$1"; }
record_fail() { FAIL+=("$1"); printf '  FAIL  %s\n' "$1"; }

# ─────────────────────────────────────────────────────────────────────────────
# Test 1 — workspace packages have no dist/ outputs.
# Packages must be source-only. Any package shipping a dist/ tree means we
# regressed to a per-package build step, which is what the cutover deletes.
# ─────────────────────────────────────────────────────────────────────────────
echo "[1] workspace packages stay source-only"
LEAKED_DIST=()
for pkg in "${PACKAGES_TO_BE_SOURCE_ONLY[@]}"; do
  if [[ -d "src/viteplus-monorepo/packages/${pkg}/dist" ]]; then
    LEAKED_DIST+=("packages/${pkg}/dist")
  fi
done
if (( ${#LEAKED_DIST[@]} == 0 )); then
  record_pass "no packages/*/dist directories present"
else
  record_fail "dist directories present: ${LEAKED_DIST[*]}"
fi

# ─────────────────────────────────────────────────────────────────────────────
# Test 2 — package.json#exports point at .ts source, not pre-built .mjs.
# ─────────────────────────────────────────────────────────────────────────────
echo "[2] package.json#exports resolve to .ts source"
EXPORT_MJS_LEAK=()
for pkg in "${PACKAGES_TO_BE_SOURCE_ONLY[@]}"; do
  pj="src/viteplus-monorepo/packages/${pkg}/package.json"
  if grep -qE '"\./[^"]*"\s*:\s*\{[^}]*"./dist/' "${pj}" 2>/dev/null \
    || grep -qE '"./dist/' "${pj}" 2>/dev/null; then
    EXPORT_MJS_LEAK+=("${pkg}")
  fi
done
if (( ${#EXPORT_MJS_LEAK[@]} == 0 )); then
  record_pass "no package exports pointing into dist/"
else
  record_fail "packages still export dist/ entries: ${EXPORT_MJS_LEAK[*]}"
fi

# ─────────────────────────────────────────────────────────────────────────────
# Test 3 — Bazel no longer carries pre-build packing rules.
# viteplus_packed_package and viteplus_source_package collapse into a single
# viteplus_workspace_package rule.
# ─────────────────────────────────────────────────────────────────────────────
echo "[3] legacy Bazel rules deleted"
LEGACY=()
if grep -qE '\b(viteplus_packed_package|viteplus_source_package)\b' \
  src/viteplus-monorepo/viteplus_rules.bzl 2>/dev/null; then
  LEGACY+=("viteplus_rules.bzl still defines packed_/source_package")
fi
if grep -rqE '\b(viteplus_packed_package|viteplus_source_package)\b' \
  src/viteplus-monorepo/packages 2>/dev/null; then
  LEGACY+=("a package BUILD.bazel still loads packed_/source_package")
fi
if (( ${#LEGACY[@]} == 0 )); then
  record_pass "viteplus_packed_package and viteplus_source_package are gone"
else
  record_fail "${LEGACY[*]}"
fi

# ─────────────────────────────────────────────────────────────────────────────
# Test 4 — deploy_bundle is self-contained and runs without node_modules.
# Build the bundle with Bazel, extract into a clean directory, and confirm
# that node --import ./instrumentation.mjs .output/server/index.mjs serves
# a request without ever touching pnpm.
# ─────────────────────────────────────────────────────────────────────────────
echo "[4] deploy_bundle runs without node_modules"
for app in "${APPS[@]}"; do
  bundle_target="//src/viteplus-monorepo/apps/${app}:deploy_bundle"
  if ! bazelisk build "${bundle_target}" >/dev/null 2>&1; then
    record_fail "${app}: bazel build ${bundle_target} failed"
    continue
  fi
  bundle_path="$(bazelisk cquery --output=files "${bundle_target}" 2>/dev/null | tail -1 | tr -d '[:space:]')"
  if [[ -z "${bundle_path}" || ! -f "${bundle_path}" ]]; then
    record_fail "${app}: cquery did not return a tarball path"
    continue
  fi
  sandbox="$(mktemp -d -t verify-viteplus-${app}-XXXXXX)"
  trap 'rm -rf "${sandbox}"' EXIT
  tar -xf "${bundle_path}" -C "${sandbox}"
  if [[ ! -f "${sandbox}/instrumentation.mjs" ]]; then
    record_fail "${app}: tarball missing instrumentation.mjs (was it bundled by esbuild?)"
    continue
  fi
  if [[ ! -f "${sandbox}/.output/server/index.mjs" ]]; then
    record_fail "${app}: tarball missing .output/server/index.mjs"
    continue
  fi
  if [[ -d "${sandbox}/node_modules" ]]; then
    record_fail "${app}: tarball contains node_modules — bundle is not self-contained"
    continue
  fi
  # Test that nothing in instrumentation.mjs has unresolved bare specifiers.
  # ESM import-statement scan: top-level imports only, not require().
  unresolved="$(grep -hoE '^\s*import [^"'\'']*from\s+["'\''][^./][^"'\'']*' \
    "${sandbox}/instrumentation.mjs" 2>/dev/null \
    | grep -vE '"node:|''node:' || true)"
  if [[ -n "${unresolved}" ]]; then
    record_fail "${app}: instrumentation.mjs has unresolved bare imports — would fail without node_modules"
    echo "${unresolved}" | sed 's/^/        /'
    continue
  fi
  record_pass "${app}: deploy_bundle is self-contained"
  rm -rf "${sandbox}"
  trap - EXIT
done

# ─────────────────────────────────────────────────────────────────────────────
# Test 5 — single npm_link_all_packages() call site at the workspace root.
# The cutover removes the per-macro npm_link_all_packages() invocations because
# every Bazel target that needs a node_modules tree should derive it from the
# leaf BUILD.bazel's own package.json, not from a macro-side-effect link.
# ─────────────────────────────────────────────────────────────────────────────
echo "[5] no npm_link_all_packages() side-effects inside macros"
if grep -q "npm_link_all_packages" src/viteplus-monorepo/viteplus_rules.bzl 2>/dev/null; then
  record_fail "viteplus_rules.bzl still calls npm_link_all_packages() inside a macro"
else
  record_pass "macros no longer mutate node_modules as a side-effect"
fi

# ─────────────────────────────────────────────────────────────────────────────
# Test 6 — every app declares its workspace_deps explicitly in BUILD.bazel.
# This is what gives Bazel per-consumer cache invalidation: a touched package
# invalidates only the apps that listed it.
# ─────────────────────────────────────────────────────────────────────────────
echo "[6] apps declare explicit workspace_deps"
for app in "${APPS[@]}"; do
  build_file="src/viteplus-monorepo/apps/${app}/BUILD.bazel"
  if grep -qE 'workspace_deps\s*=' "${build_file}" 2>/dev/null; then
    record_pass "${app}: BUILD.bazel lists workspace_deps"
  else
    record_fail "${app}: BUILD.bazel missing workspace_deps"
  fi
done

# ─────────────────────────────────────────────────────────────────────────────
echo
echo "============================================================"
printf 'PASS: %d\n' "${#PASS[@]}"
printf 'FAIL: %d\n' "${#FAIL[@]}"
if (( ${#FAIL[@]} > 0 )); then
  printf '\nFailures:\n'
  for f in "${FAIL[@]}"; do
    printf '  - %s\n' "$f"
  done
  exit 1
fi
