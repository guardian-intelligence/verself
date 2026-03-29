#!/usr/bin/env bash
# Tool health-check library for `make doctor`.
# Sourced by doctor.sh and tests. No side effects on source.

# check_tool <binary_name> <version_cmd> <expected_version> <nix_attr>
#
# Checks a single tool against the expected state.
#
# Exit codes:
#   0 — tool is present and version matches (SKIP)
#   1 — tool is missing, not in nix store (MISSING)
#   2 — tool is missing, but available in nix store (INSTALLABLE)
#   3 — tool is present, wrong version, nix-managed (UPGRADABLE)
#   4 — tool is present, wrong version, not nix-managed (CONFLICT)
#
# Output: single-line status message.
#
# Environment overrides (for testing):
#   _DOCTOR_WHICH   — function replacing `which`
#   _DOCTOR_VERSION — function replacing version command evaluation
#   _DOCTOR_NIX_STORE_PATH — function replacing nix store path check
#   _DOCTOR_IS_NIX_PROFILE — function replacing nix profile membership check
check_tool() {
  local binary="$1"
  local version_cmd="$2"
  local expected="$3"
  local nix_attr="$4"

  # Is it in PATH?
  local bin_path
  bin_path=$(_doctor_which "$binary")
  if [[ -z "$bin_path" ]]; then
    # Not in PATH — is it in the nix store (already built)?
    local store_path
    store_path=$(_doctor_nix_store_path "$nix_attr")
    if [[ -n "$store_path" ]]; then
      echo "INSTALLABLE: $binary not in PATH, available in nix store"
      return 2
    else
      echo "MISSING: $binary not in PATH, not in nix store"
      return 1
    fi
  fi

  # In PATH — check version
  local actual_version
  actual_version=$(_doctor_version "$version_cmd")
  if [[ "$actual_version" == "$expected" ]]; then
    echo "OK: $binary $expected"
    return 0
  fi

  # Wrong version — is it nix-managed?
  if _doctor_is_nix_profile "$bin_path"; then
    echo "UPGRADABLE: $binary $actual_version (want $expected, nix-managed)"
    return 3
  else
    echo "CONFLICT: $binary $actual_version (want $expected, from $bin_path)"
    return 4
  fi
}

# Default implementations (overridden in tests)
_doctor_which() {
  command -v "$1" 2>/dev/null || echo ""
}

_doctor_version() {
  eval "$1" 2>/dev/null || echo ""
}

_doctor_nix_store_path() {
  nix path-info ".#dev-tools" 2>/dev/null | head -1 || echo ""
}

_doctor_is_nix_profile() {
  [[ "$1" == /nix/store/* ]]
}
