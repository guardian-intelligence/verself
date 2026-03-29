#!/usr/bin/env bash
# Scenario: dev-tools installed (all OK), then fake system go shadows nix go.
# Expected: go = Conflict, remaining 7 = OK.

scenario_name() { echo "Mixed: dev-tools + system go conflict"; }

scenario_setup() { :; }  # nothing as testuser

scenario_setup_root() {
  # Create fake go at system path with wrong version (runs as root)
  mkdir -p /usr/local/go/bin
  cat > /usr/local/go/bin/go << 'GOEOF'
#!/bin/sh
echo "go version go1.21.0 linux/amd64"
GOEOF
  chmod +x /usr/local/go/bin/go
}

scenario_test() {
  # Install dev-tools first so all tools are available via nix
  nix profile install /workspace#dev-tools 2>&1

  # Put system go BEFORE nix in PATH
  export PATH="/usr/local/go/bin:$PATH"
  bmci doctor 2>&1
}

scenario_verify() {
  local output="$1"
  local exit_code="$2"

  # Should exit non-zero (go conflict)
  if [[ "$exit_code" -eq 0 ]]; then
    echo "  expected non-zero exit, got 0"
    return 1
  fi

  # go should be a conflict
  if ! echo "$output" | grep "go" | grep -q "⚠"; then
    echo "  expected go to show �� (conflict)"
    return 1
  fi

  # The other 7 tools should all show ✓
  local ok_tools=(tofu ansible sops age buf shellcheck jq)
  for tool in "${ok_tools[@]}"; do
    if ! echo "$output" | grep "$tool" | grep -q "✓"; then
      echo "  expected '$tool' to show ✓"
      return 1
    fi
  done

  # Summary should show exactly 1 conflict
  if ! echo "$output" | grep -q "1 conflict"; then
    echo "  expected '1 conflict' in summary"
    return 1
  fi

  return 0
}
