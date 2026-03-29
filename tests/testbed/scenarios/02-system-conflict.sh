#!/usr/bin/env bash
# Scenario: fake system tools at wrong versions create Conflict status.

scenario_name() { echo "System tools at wrong versions → Conflict"; }

scenario_setup() { :; }  # no user-level setup needed

scenario_setup_root() {
  # Create fake go binary that reports wrong version
  mkdir -p /usr/local/go/bin
  cat > /usr/local/go/bin/go << 'GOEOF'
#!/bin/sh
echo "go version go1.21.0 linux/amd64"
GOEOF
  chmod +x /usr/local/go/bin/go

  # Create fake shellcheck that reports wrong version
  cat > /usr/local/bin/shellcheck << 'SCEOF'
#!/bin/sh
echo "ShellCheck - shell script analysis tool"
echo "version: 0.9.0"
SCEOF
  chmod +x /usr/local/bin/shellcheck
}

scenario_test() {
  export PATH="/usr/local/go/bin:/usr/local/bin:$PATH"
  bmci doctor 2>&1
}

scenario_verify() {
  local output="$1"
  local exit_code="$2"

  # Should exit non-zero (conflicts exist)
  if [[ "$exit_code" -eq 0 ]]; then
    echo "  expected non-zero exit, got 0"
    return 1
  fi

  # go should show as conflict with 1.21.0
  if ! echo "$output" | grep "go" | grep -q "1.21.0"; then
    echo "  expected go conflict at 1.21.0"
    return 1
  fi

  # shellcheck should show as conflict with 0.9.0
  if ! echo "$output" | grep "shellcheck" | grep -q "0.9.0"; then
    echo "  expected shellcheck conflict at 0.9.0"
    return 1
  fi

  # Both should show ⚠ (conflict/upgradable marker)
  if ! echo "$output" | grep "go" | grep -q "⚠"; then
    echo "  expected ⚠ marker for go"
    return 1
  fi

  return 0
}
