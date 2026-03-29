#!/usr/bin/env bash
# Tests for Cloudflare token classification.
# Run: bash tests/test_cloudflare_token.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/../scripts/lib/cloudflare.sh"

DOMAIN="anveio.com"
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
    echo "  PASS  $test_name (output contains \"$expected_substr\")"
    PASS=$((PASS + 1))
  else
    echo "  FAIL  $test_name"
    echo "        expected output to contain: $expected_substr"
    echo "        actual output: $output"
    FAIL=$((FAIL + 1))
  fi
}

# ── Fixtures ───────────────────────────────────────────────────────

# 1. Valid zone-scoped DNS token — the happy path
FIXTURE_VALID_DNS=$(cat <<'JSON'
{
  "success": true,
  "errors": [],
  "result": [{
    "id": "abc123",
    "name": "anveio.com",
    "status": "active",
    "permissions": ["#dns_records:edit", "#dns_records:read", "#zone:read"]
  }],
  "result_info": {"count": 1, "total_count": 1}
}
JSON
)

# 2. Valid token but only read permissions (e.g. "Read zone DNS" template)
FIXTURE_READ_ONLY=$(cat <<'JSON'
{
  "success": true,
  "errors": [],
  "result": [{
    "id": "abc123",
    "name": "anveio.com",
    "status": "active",
    "permissions": ["#dns_records:read", "#zone:read"]
  }],
  "result_info": {"count": 1, "total_count": 1}
}
JSON
)

# 3. Valid token scoped to a different zone (can't see this domain)
FIXTURE_WRONG_ZONE=$(cat <<'JSON'
{
  "success": true,
  "errors": [],
  "result": [],
  "result_info": {"count": 0, "total_count": 0}
}
JSON
)

# 4. Completely invalid / expired token
FIXTURE_INVALID=$(cat <<'JSON'
{
  "success": false,
  "errors": [{"code": 1000, "message": "Invalid API Token"}],
  "result": null
}
JSON
)

# 5. Valid token with zone access but no DNS permissions at all (e.g. Workers token)
FIXTURE_NO_DNS=$(cat <<'JSON'
{
  "success": true,
  "errors": [],
  "result": [{
    "id": "abc123",
    "name": "anveio.com",
    "status": "active",
    "permissions": ["#zone:read"]
  }],
  "result_info": {"count": 1, "total_count": 1}
}
JSON
)

# 6. Malformed / empty response (network error, timeout, etc.)
FIXTURE_EMPTY='{}'

# ── Tests ──────────────────────────────────────────────────────────

echo "=== Cloudflare token classification tests ==="
echo ""

# --- Test 1: Valid DNS edit token ---
echo "Test 1: Valid DNS edit token"
OUTPUT=$(classify_cf_token "$FIXTURE_VALID_DNS" "$DOMAIN" 2>&1) && EXIT=$? || EXIT=$?
assert_exit "exits 0" 0 "$EXIT" "$OUTPUT"
assert_output_contains "reports valid" "Valid DNS token" "$OUTPUT"

echo ""

# --- Test 2: Read-only DNS token ---
echo "Test 2: Read-only DNS token (missing #dns_records:edit)"
OUTPUT=$(classify_cf_token "$FIXTURE_READ_ONLY" "$DOMAIN" 2>&1) && EXIT=$? || EXIT=$?
assert_exit "exits 2" 2 "$EXIT" "$OUTPUT"
assert_output_contains "mentions missing permission" "lacks DNS edit permission" "$OUTPUT"
assert_output_contains "tells user which template" "Edit zone DNS" "$OUTPUT"
assert_output_contains "shows current permissions" "#dns_records:read" "$OUTPUT"

echo ""

# --- Test 3: Token scoped to wrong zone ---
echo "Test 3: Token scoped to wrong zone (empty result)"
OUTPUT=$(classify_cf_token "$FIXTURE_WRONG_ZONE" "$DOMAIN" 2>&1) && EXIT=$? || EXIT=$?
assert_exit "exits 2" 2 "$EXIT" "$OUTPUT"
assert_output_contains "mentions zone access" "does not have access to zone" "$OUTPUT"
assert_output_contains "tells user to scope to domain" "$DOMAIN" "$OUTPUT"

echo ""

# --- Test 4: Invalid / expired token ---
echo "Test 4: Invalid or expired token"
OUTPUT=$(classify_cf_token "$FIXTURE_INVALID" "$DOMAIN" 2>&1) && EXIT=$? || EXIT=$?
assert_exit "exits 1" 1 "$EXIT" "$OUTPUT"
assert_output_contains "reports invalid" "Invalid API token" "$OUTPUT"

echo ""

# --- Test 5: Zone access but no DNS permissions at all ---
echo "Test 5: Zone access but no DNS permissions (e.g. Workers token)"
OUTPUT=$(classify_cf_token "$FIXTURE_NO_DNS" "$DOMAIN" 2>&1) && EXIT=$? || EXIT=$?
assert_exit "exits 2" 2 "$EXIT" "$OUTPUT"
assert_output_contains "mentions missing permission" "lacks DNS edit permission" "$OUTPUT"
assert_output_contains "shows current permissions" "#zone:read" "$OUTPUT"

echo ""

# --- Test 6: Malformed / empty response ---
echo "Test 6: Malformed response (network error)"
OUTPUT=$(classify_cf_token "$FIXTURE_EMPTY" "$DOMAIN" 2>&1) && EXIT=$? || EXIT=$?
assert_exit "exits 1" 1 "$EXIT" "$OUTPUT"

echo ""

# ── Summary ────────────────────────────────────────────────────────
echo "=== Results: $PASS passed, $FAIL failed ==="
if [[ "$FAIL" -gt 0 ]]; then
  exit 1
fi
