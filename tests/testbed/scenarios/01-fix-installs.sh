#!/usr/bin/env bash
# Scenario: doctor --fix installs all tools, second run shows all OK.

scenario_name() { echo "doctor --fix installs everything"; }

scenario_setup() { :; }  # start from clean

scenario_test() {
  # First run: --fix should install dev-tools
  bmci doctor --fix 2>&1 || true

  echo "--- second run ---"

  # Second run: should show all OK
  bmci doctor 2>&1
}

scenario_verify() {
  local output="$1"
  local exit_code="$2"

  # The second doctor run should exit 0
  if [[ "$exit_code" -ne 0 ]]; then
    echo "  expected exit 0 after fix, got $exit_code"
    return 1
  fi

  # After the "--- second run ---" marker, every tool should show ✓
  local second_run
  second_run=$(echo "$output" | sed -n '/--- second run ---/,$p')

  local tools=(go tofu ansible sops age buf shellcheck jq)
  for tool in "${tools[@]}"; do
    if ! echo "$second_run" | grep -q "✓.*$tool\|$tool.*✓"; then
      # Check if tool appears with ✓ on the same line
      if ! echo "$second_run" | grep "$tool" | grep -q "✓"; then
        echo "  tool '$tool' not OK after fix"
        return 1
      fi
    fi
  done

  return 0
}
