#!/usr/bin/env bash
# Tests for `make doctor` state machine transitions.
# Run: bash tests/test_doctor.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/../scripts/lib/doctor.sh"

PASS=0
FAIL=0

assert_exit() {
  local test_name="$1"
  local expected_exit="$2"
  local actual_exit="$3"
  local output="$4"

  if [[ "$actual_exit" == "$expected_exit" ]]; then
    echo "  PASS  $test_name (exit=$actual_exit)"
    PASS=$((PASS + 1))
  else
    echo "  FAIL  $test_name"
    echo "        expected exit=$expected_exit, got exit=$actual_exit"
    echo "        output: $output"
    FAIL=$((FAIL + 1))
  fi
}

assert_output_contains() {
  local test_name="$1"
  local expected_substr="$2"
  local output="$3"

  if echo "$output" | grep -qF "$expected_substr"; then
    echo "  PASS  $test_name (contains \"$expected_substr\")"
    PASS=$((PASS + 1))
  else
    echo "  FAIL  $test_name"
    echo "        expected to contain: $expected_substr"
    echo "        actual: $output"
    FAIL=$((FAIL + 1))
  fi
}

# ── Test helpers: mock overrides ───────────────────────────────────

# Reset all mocks to "tool does not exist" state
reset_mocks() {
  _doctor_which()          { echo ""; }
  _doctor_version()        { echo ""; }
  _doctor_nix_store_path() { echo ""; }
  _doctor_is_nix_profile() { return 1; }
}

# ── Tests ──────────────────────────────────────────────────────────

echo "=== make doctor state machine tests ==="
echo ""

# ---------------------------------------------------------------
# State: IN PATH + VERSION MATCHES → SKIP (exit 0)
# ---------------------------------------------------------------
echo "Test 1: Tool in PATH, correct version → OK"
reset_mocks
_doctor_which()   { echo "/nix/store/abc-sops-3.12.2/bin/sops"; }
_doctor_version() { echo "3.12.2"; }

OUTPUT=$(check_tool "sops" "sops --version" "3.12.2" "pkgs.sops" 2>&1) && EXIT=$? || EXIT=$?
assert_exit "exits 0 (OK)" 0 "$EXIT" "$OUTPUT"
assert_output_contains "status is OK" "OK: sops 3.12.2" "$OUTPUT"

echo ""

# ---------------------------------------------------------------
# State: NOT IN PATH + NOT IN NIX STORE → MISSING (exit 1)
# ---------------------------------------------------------------
echo "Test 2: Tool not in PATH, not in nix store → MISSING"
reset_mocks
# defaults: which returns "", nix_store_path returns ""

OUTPUT=$(check_tool "sops" "sops --version" "3.12.2" "pkgs.sops" 2>&1) && EXIT=$? || EXIT=$?
assert_exit "exits 1 (MISSING)" 1 "$EXIT" "$OUTPUT"
assert_output_contains "status is MISSING" "MISSING" "$OUTPUT"
assert_output_contains "names the tool" "sops" "$OUTPUT"

echo ""

# ---------------------------------------------------------------
# State: NOT IN PATH + IN NIX STORE → INSTALLABLE (exit 2)
# ---------------------------------------------------------------
echo "Test 3: Tool not in PATH, available in nix store → INSTALLABLE"
reset_mocks
_doctor_nix_store_path() { echo "/nix/store/abc-forge-metal-dev-tools"; }

OUTPUT=$(check_tool "sops" "sops --version" "3.12.2" "pkgs.sops" 2>&1) && EXIT=$? || EXIT=$?
assert_exit "exits 2 (INSTALLABLE)" 2 "$EXIT" "$OUTPUT"
assert_output_contains "status is INSTALLABLE" "INSTALLABLE" "$OUTPUT"
assert_output_contains "mentions nix store" "nix store" "$OUTPUT"

echo ""

# ---------------------------------------------------------------
# State: IN PATH + WRONG VERSION + NIX-MANAGED → UPGRADABLE (exit 3)
# ---------------------------------------------------------------
echo "Test 4: Tool in PATH, wrong version, nix-managed → UPGRADABLE"
reset_mocks
_doctor_which()          { echo "/nix/store/old-sops-3.10.0/bin/sops"; }
_doctor_version()        { echo "3.10.0"; }
_doctor_is_nix_profile() { [[ "$1" == /nix/store/* ]]; }

OUTPUT=$(check_tool "sops" "sops --version" "3.12.2" "pkgs.sops" 2>&1) && EXIT=$? || EXIT=$?
assert_exit "exits 3 (UPGRADABLE)" 3 "$EXIT" "$OUTPUT"
assert_output_contains "status is UPGRADABLE" "UPGRADABLE" "$OUTPUT"
assert_output_contains "shows actual version" "3.10.0" "$OUTPUT"
assert_output_contains "shows wanted version" "3.12.2" "$OUTPUT"
assert_output_contains "says nix-managed" "nix-managed" "$OUTPUT"

echo ""

# ---------------------------------------------------------------
# State: IN PATH + WRONG VERSION + NOT NIX-MANAGED → CONFLICT (exit 4)
# ---------------------------------------------------------------
echo "Test 5: Tool in PATH, wrong version, system-managed → CONFLICT"
reset_mocks
_doctor_which()          { echo "/usr/bin/sops"; }
_doctor_version()        { echo "3.10.0"; }
_doctor_is_nix_profile() { return 1; }  # not nix-managed

OUTPUT=$(check_tool "sops" "sops --version" "3.12.2" "pkgs.sops" 2>&1) && EXIT=$? || EXIT=$?
assert_exit "exits 4 (CONFLICT)" 4 "$EXIT" "$OUTPUT"
assert_output_contains "status is CONFLICT" "CONFLICT" "$OUTPUT"
assert_output_contains "shows actual version" "3.10.0" "$OUTPUT"
assert_output_contains "shows wanted version" "3.12.2" "$OUTPUT"
assert_output_contains "shows conflicting path" "/usr/bin/sops" "$OUTPUT"

echo ""

# ---------------------------------------------------------------
# Edge: IN PATH + version command fails → treat as wrong version
# ---------------------------------------------------------------
echo "Test 6: Tool in PATH, version command fails → version mismatch"
reset_mocks
_doctor_which()          { echo "/usr/local/bin/sops"; }
_doctor_version()        { echo ""; }  # version cmd returns empty
_doctor_is_nix_profile() { return 1; }

OUTPUT=$(check_tool "sops" "sops --version" "3.12.2" "pkgs.sops" 2>&1) && EXIT=$? || EXIT=$?
assert_exit "exits 4 (CONFLICT — can't determine version)" 4 "$EXIT" "$OUTPUT"
assert_output_contains "status is CONFLICT" "CONFLICT" "$OUTPUT"

echo ""

# ---------------------------------------------------------------
# Edge: Multiple tools, different states
# ---------------------------------------------------------------
echo "Test 7: Batch — mixed states across tools"

RESULTS=()

# Tool 1: go — OK
reset_mocks
_doctor_which()   { echo "/nix/store/abc-go/bin/go"; }
_doctor_version() { echo "1.25.0"; }
_doctor_is_nix_profile() { [[ "$1" == /nix/store/* ]]; }
OUTPUT=$(check_tool "go" "go version" "1.25.0" "pkgs.go_1_25" 2>&1) && EXIT=$? || EXIT=$?
RESULTS+=("go:$EXIT")
assert_exit "go is OK" 0 "$EXIT" "$OUTPUT"

# Tool 2: sops — MISSING
reset_mocks
OUTPUT=$(check_tool "sops" "sops --version" "3.12.2" "pkgs.sops" 2>&1) && EXIT=$? || EXIT=$?
RESULTS+=("sops:$EXIT")
assert_exit "sops is MISSING" 1 "$EXIT" "$OUTPUT"

# Tool 3: ansible — CONFLICT
reset_mocks
_doctor_which()          { echo "/usr/bin/ansible"; }
_doctor_version()        { echo "2.14.0"; }
_doctor_is_nix_profile() { return 1; }
OUTPUT=$(check_tool "ansible" "ansible --version" "2.16.3" "pkgs.ansible" 2>&1) && EXIT=$? || EXIT=$?
RESULTS+=("ansible:$EXIT")
assert_exit "ansible is CONFLICT" 4 "$EXIT" "$OUTPUT"

# Verify we got the right mix
RESULT_STR="${RESULTS[*]}"
if [[ "$RESULT_STR" == "go:0 sops:1 ansible:4" ]]; then
  echo "  PASS  batch produces correct state vector"
  PASS=$((PASS + 1))
else
  echo "  FAIL  batch state vector"
  echo "        expected: go:0 sops:1 ansible:4"
  echo "        actual:   $RESULT_STR"
  FAIL=$((FAIL + 1))
fi

echo ""

# ── Summary ────────────────────────────────────────────────────────

echo "=== Results: $PASS passed, $FAIL failed ==="
if [[ "$FAIL" -gt 0 ]]; then
  exit 1
fi
