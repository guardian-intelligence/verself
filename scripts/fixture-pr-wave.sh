#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -lt 4 ]; then
  cat >&2 <<'USAGE'
usage: scripts/fixture-pr-wave.sh <host> <owner> <password> <prs_per_repo> [repo ...]

Creates a PR wave against the live Forgejo worker over SSH using the staged
fixture manifests already present on the host. It emits a compact summary of
created commits, workflow outcomes, and sampled host pressure.
USAGE
  exit 2
fi

host=$1
owner=$2
password=$3
prs_per_repo=$4
shift 4

if [ "$prs_per_repo" -lt 1 ]; then
  echo "prs_per_repo must be >= 1" >&2
  exit 2
fi

if [ "$#" -eq 0 ]; then
  repos=(
    next-bun-monorepo
    next-pnpm-postgres
    next-npm-workspaces
    next-npm-single-app
  )
else
  repos=("$@")
fi

ssh -o StrictHostKeyChecking=no "ubuntu@$host" \
  bash -s -- "$owner" "$password" "$prs_per_repo" "${repos[@]}" <<'EOF'
set -euo pipefail

export PATH="/opt/forge-metal/profile/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

owner=$1
password=$2
prs_per_repo=$3
shift 3
repos=("$@")

base_url='http://127.0.0.1:3000'
wave="wave-$(date -u +%Y%m%d-%H%M%S)"
wave_start_utc="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
monitor_file="/tmp/${wave}.monitor.tsv"
records_dir="$(mktemp -d)"

cleanup() {
  if [ -n "${monitor_pid:-}" ]; then
    kill "$monitor_pid" >/dev/null 2>&1 || true
    wait "$monitor_pid" 2>/dev/null || true
  fi
  rm -rf "$records_dir"
}
trap cleanup EXIT

token="$(curl -fsSu "$owner:$password" \
  -X POST "$base_url/api/v1/users/$owner/tokens" \
  -H 'Content-Type: application/json' \
  -d "{\"name\":\"$wave\",\"scopes\":[\"all\"]}" | jq -r '.sha1')"

if [ -z "$token" ] || [ "$token" = "null" ]; then
  echo "failed to create Forgejo API token" >&2
  exit 1
fi

(
  read_cpu_fields() {
    awk '/^cpu / {for (i = 2; i <= 11; i++) printf "%s ", $i; exit}' /proc/stat
  }

  sum_cpu_fields() {
    local sum=0
    local value
    for value in "$@"; do
      sum=$((sum + value))
    done
    printf '%s\n' "$sum"
  }

  read -r -a prev_cpu <<<"$(read_cpu_fields)"
  while :; do
    sleep 1
    read -r -a curr_cpu <<<"$(read_cpu_fields)"
    prev_total="$(sum_cpu_fields "${prev_cpu[@]}")"
    curr_total="$(sum_cpu_fields "${curr_cpu[@]}")"
    prev_idle=$((prev_cpu[3]))
    curr_idle=$((curr_cpu[3]))
    prev_iowait=$((prev_cpu[4]))
    curr_iowait=$((curr_cpu[4]))
    total_delta=$((curr_total - prev_total))
    idle_delta=$((curr_idle - prev_idle))
    iowait_delta=$((curr_iowait - prev_iowait))
    busy_delta=$((total_delta - idle_delta - iowait_delta))
    cpu_busy_pct="$(awk -v busy="$busy_delta" -v total="$total_delta" 'BEGIN { if (total <= 0) { print "0.00" } else { printf "%.2f", (100 * busy) / total } }')"
    cpu_iowait_pct="$(awk -v wait="$iowait_delta" -v total="$total_delta" 'BEGIN { if (total <= 0) { print "0.00" } else { printf "%.2f", (100 * wait) / total } }')"
    prev_cpu=("${curr_cpu[@]}")

    ts="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
    load1="$(cut -d' ' -f1 /proc/loadavg)"
    mem_available_kb="$(awk '/^MemAvailable:/ {print $2}' /proc/meminfo)"
    leases="$(sudo find /var/lib/ci/net/leases -maxdepth 1 -name '*.json' | wc -l)"
    taps="$(ip link show | grep -c 'fc-tap-' || true)"
    jailers="$(pgrep -fc jailer || true)"
    printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' "$ts" "$cpu_busy_pct" "$cpu_iowait_pct" "$mem_available_kb" "$load1" "$leases" "$taps" "$jailers"
  done
) >"$monitor_file" &
monitor_pid=$!

create_pr() {
  local repo=$1
  local idx=$2
  local manifest change_path change_find branch replace tmp sha pr_json pr_number

  manifest="/opt/forge-metal/test-fixtures/$repo/.forge-metal/ci.toml"
  change_path="$(awk -F'"' '/^pr_change_path/ {print $2}' "$manifest")"
  change_find="$(awk -F'"' '/^pr_change_find/ {print $2}' "$manifest")"
  branch="load/${wave}/${repo}/${idx}"
  replace="${change_find} [${wave}-${idx}]"
  tmp="$(mktemp -d)"

  git clone --depth 1 "http://$owner:$token@127.0.0.1:3000/$owner/$repo.git" "$tmp" >/dev/null 2>&1
  git -C "$tmp" checkout -b "$branch" >/dev/null 2>&1

  python3 - "$tmp/$change_path" "$change_find" "$replace" <<'PY'
from pathlib import Path
import sys

path = Path(sys.argv[1])
needle = sys.argv[2]
replacement = sys.argv[3]
data = path.read_text()
if needle not in data:
    raise SystemExit(f"missing marker in {path}")
path.write_text(data.replace(needle, replacement, 1))
PY

  git -C "$tmp" config user.name "$owner"
  git -C "$tmp" config user.email 'forge-metal-load@local'
  git -C "$tmp" add -A
  git -C "$tmp" commit -m "load: ${wave} ${repo} ${idx}" >/dev/null 2>&1
  sha="$(git -C "$tmp" rev-parse HEAD)"
  git -C "$tmp" push origin "HEAD:$branch" >/dev/null 2>&1

  pr_json="$(curl -fsSu "$owner:$password" \
    -X POST "$base_url/api/v1/repos/$owner/$repo/pulls" \
    -H 'Content-Type: application/json' \
    -d "{\"title\":\"load wave ${wave} ${repo} ${idx}\",\"head\":\"${branch}\",\"base\":\"main\"}")"
  pr_number="$(printf '%s' "$pr_json" | jq -r '.number')"
  printf '%s\t%s\t%s\t%s\n' "$repo" "$sha" "$branch" "$pr_number"

  rm -rf "$tmp"
}

wait_for_run() {
  local repo=$1
  local sha=$2
  local deadline line status conclusion run_id
  deadline=$((SECONDS + 900))

  while [ "$SECONDS" -lt "$deadline" ]; do
    line="$(curl -fsSu "$owner:$password" \
      "$base_url/api/v1/repos/$owner/$repo/actions/runs" | \
      jq -r --arg sha "$sha" \
        '.workflow_runs[] | select(.commit_sha == $sha) | "\(.id)\t\(.status)\t\(.conclusion)"' | head -n1)"

    if [ -n "$line" ]; then
      IFS=$'\t' read -r run_id status conclusion <<EOF_RUN
$line
EOF_RUN
      if [ "$status" = "success" ] || [ "$conclusion" = "success" ]; then
        printf '%s\t%s\t%s\t%s\n' "$repo" "$sha" "$run_id" "success"
        return 0
      fi
      if [ "$status" = "failure" ] || { [ "$status" = "completed" ] && [ -n "$conclusion" ] && [ "$conclusion" != "success" ]; }; then
        printf '%s\t%s\t%s\t%s:%s\n' "$repo" "$sha" "$run_id" "$status" "$conclusion"
        return 1
      fi
    fi
    sleep 2
  done

  printf '%s\t%s\t%s\t%s\n' "$repo" "$sha" "-" "timeout"
  return 124
}

printf 'wave\t%s\n' "$wave"
printf 'wave_start_utc\t%s\n' "$wave_start_utc"

create_rc=0
pids=()
for repo in "${repos[@]}"; do
  for idx in $(seq 1 "$prs_per_repo"); do
    create_pr "$repo" "$idx" >"$records_dir/${repo}-${idx}.created.tsv" &
    pids+=("$!")
  done
done

for pid in "${pids[@]}"; do
  if ! wait "$pid"; then
    create_rc=1
  fi
done

if [ "$create_rc" -ne 0 ]; then
  echo "one or more PR creations failed" >&2
  exit "$create_rc"
fi

printf 'created_prs\n'
cat "$records_dir"/*.created.tsv | sort

status_rc=0
for created in "$records_dir"/*.created.tsv; do
  IFS=$'\t' read -r repo sha branch pr_number <"$created"
  if ! wait_for_run "$repo" "$sha" >"$records_dir/$(basename "$created" .created.tsv).result.tsv"; then
    status_rc=1
  fi
done

wave_end_utc="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
kill "$monitor_pid" >/dev/null 2>&1 || true
wait "$monitor_pid" 2>/dev/null || true
unset monitor_pid

printf 'results\n'
cat "$records_dir"/*.result.tsv | sort

peak_cpu_busy_pct="$(awk 'max < $2 { max = $2 } END { print max + 0 }' "$monitor_file")"
peak_cpu_iowait_pct="$(awk 'max < $3 { max = $3 } END { print max + 0 }' "$monitor_file")"
min_mem_available_kb="$(awk 'NR == 1 || min > $4 { min = $4 } END { print min + 0 }' "$monitor_file")"
peak_load1="$(awk 'max < $5 { max = $5 } END { print max + 0 }' "$monitor_file")"
peak_leases="$(awk 'max < $6 { max = $6 } END { print max + 0 }' "$monitor_file")"
peak_taps="$(awk 'max < $7 { max = $7 } END { print max + 0 }' "$monitor_file")"
peak_jailers="$(awk 'max < $8 { max = $8 } END { print max + 0 }' "$monitor_file")"

printf 'wave_end_utc\t%s\n' "$wave_end_utc"
printf 'monitor_file\t%s\n' "$monitor_file"
printf 'peak_cpu_busy_pct\t%s\n' "$peak_cpu_busy_pct"
printf 'peak_cpu_iowait_pct\t%s\n' "$peak_cpu_iowait_pct"
printf 'min_mem_available_kb\t%s\n' "$min_mem_available_kb"
printf 'peak_load1\t%s\n' "$peak_load1"
printf 'peak_leases\t%s\n' "$peak_leases"
printf 'peak_taps\t%s\n' "$peak_taps"
printf 'peak_jailers\t%s\n' "$peak_jailers"

expected_jobs=$((${#repos[@]} * prs_per_repo))
required_overlap=2
if [ "$expected_jobs" -lt 2 ]; then
  required_overlap=1
fi

if [ "$peak_leases" -lt "$required_overlap" ]; then
  echo "expected at least ${required_overlap} overlapping active leases, observed ${peak_leases}" >&2
  exit 1
fi
if [ "$peak_taps" -lt "$required_overlap" ]; then
  echo "expected at least ${required_overlap} overlapping tap devices, observed ${peak_taps}" >&2
  exit 1
fi
if [ "$peak_jailers" -lt "$required_overlap" ]; then
  echo "expected at least ${required_overlap} overlapping jailers, observed ${peak_jailers}" >&2
  exit 1
fi

exit "$status_rc"
EOF
