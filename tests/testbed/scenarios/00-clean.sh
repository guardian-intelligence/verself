#!/usr/bin/env bash
# Scenario: clean container with no dev tools installed.
# Expected: all 8 tools report as Missing or Installable.

scenario_name() { echo "No tools installed → all missing"; }

scenario_setup() { :; }  # nothing — clean state

scenario_test() {
  bmci doctor 2>&1
}

scenario_verify() {
  local output="$1"
  local exit_code="$2"

  # Should exit non-zero (tools are missing)
  if [[ "$exit_code" -eq 0 ]]; then
    echo "  expected non-zero exit, got 0"
    return 1
  fi

  # Every tool in the manifest should appear as missing or not-in-PATH
  local tools=(go tofu ansible sops age buf shellcheck jq)
  for tool in "${tools[@]}"; do
    if ! echo "$output" | grep -q "$tool"; then
      echo "  tool '$tool' not mentioned in output"
      return 1
    fi
  done

  # Should NOT have any ✓ (OK) lines
  if echo "$output" | grep -q "✓"; then
    echo "  unexpected OK tool in clean state"
    return 1
  fi

  return 0
}
