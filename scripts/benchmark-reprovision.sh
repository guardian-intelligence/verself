#!/usr/bin/env bash
# Benchmark wipe + reprovision cycle.
# Runs N iterations of the full ci-e2e pipeline and records timing.
#
# Usage: ./scripts/benchmark-reprovision.sh [iterations] [nix_profile_path]
# Example: ./scripts/benchmark-reprovision.sh 3

set -euo pipefail

ITERATIONS=${1:-3}
PROFILE_PATH=${2:-$(nix build .#server-profile --no-link --print-out-paths)}
RESULTS_FILE="results/reprovision-bench-$(date +%Y%m%d-%H%M%S).json"
ANSIBLE_DIR="$(cd "$(dirname "$0")/../ansible" && pwd)"

mkdir -p results

echo "========================================"
echo "forge-metal reprovision benchmark"
echo "  Iterations:   $ITERATIONS"
echo "  Nix profile:  $PROFILE_PATH"
echo "  Results:      $RESULTS_FILE"
echo "========================================"

# JSON array to collect results
echo "[" > "$RESULTS_FILE"

for i in $(seq 1 "$ITERATIONS"); do
    echo ""
    echo "=== Iteration $i/$ITERATIONS ==="
    echo ""

    ITER_START=$(date +%s%N)

    # Phase 1: Wipe
    echo "  [wipe] starting..."
    WIPE_START=$(date +%s%N)
    ansible-playbook -i "$ANSIBLE_DIR/inventory/hosts.ini" \
        "$ANSIBLE_DIR/playbooks/ci-e2e.yml" \
        -e "nix_server_profile_path=$PROFILE_PATH" \
        --tags never 2>/dev/null || true  # no-op, we run phases manually

    # Actually run the wipe + reprovision + verify via ci-e2e.yml
    PHASE_START=$(date +%s%N)
    ansible-playbook -i "$ANSIBLE_DIR/inventory/hosts.ini" \
        "$ANSIBLE_DIR/playbooks/ci-e2e.yml" \
        -e "nix_server_profile_path=$PROFILE_PATH" \
        2>&1 | tee "/tmp/forge-metal-bench-iter-${i}.log"
    PHASE_END=$(date +%s%N)

    TOTAL_MS=$(( (PHASE_END - PHASE_START) / 1000000 ))

    echo "  [total] ${TOTAL_MS}ms"

    # Append to results
    if [ "$i" -gt 1 ]; then echo "," >> "$RESULTS_FILE"; fi
    cat >> "$RESULTS_FILE" <<EOF
  {
    "iteration": $i,
    "total_ms": $TOTAL_MS,
    "timestamp": "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  }
EOF
done

echo "]" >> "$RESULTS_FILE"

echo ""
echo "========================================"
echo "RESULTS"
echo "========================================"
echo ""

# Print summary
python3 -c "
import json, sys

with open('$RESULTS_FILE') as f:
    results = json.load(f)

times = [r['total_ms'] for r in results]
print(f'Iterations: {len(times)}')
for r in results:
    print(f\"  #{r['iteration']}: {r['total_ms']}ms ({r['total_ms']/1000:.1f}s)\")
print()
print(f'Min:     {min(times)}ms ({min(times)/1000:.1f}s)')
print(f'Max:     {max(times)}ms ({max(times)/1000:.1f}s)')
print(f'Mean:    {sum(times)/len(times):.0f}ms ({sum(times)/len(times)/1000:.1f}s)')
if len(times) > 1:
    times.sort()
    p50 = times[len(times)//2]
    print(f'Median:  {p50}ms ({p50/1000:.1f}s)')
print()
print(f'Results: $RESULTS_FILE')
"
