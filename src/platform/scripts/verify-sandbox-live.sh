#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/../../.." && pwd)"
platform_root="${repo_root}/src/platform"
inventory="${platform_root}/ansible/inventory/hosts.ini"
vars_file="${platform_root}/ansible/group_vars/all/main.yml"

run_id="${VERIFICATION_RUN_ID:-sandbox-live-$(date -u +%Y%m%dT%H%M%SZ)}"
artifact_root="${VERIFICATION_ARTIFACT_ROOT:-${repo_root}/artifacts/sandbox-live}"
artifact_dir="${artifact_root}/${run_id}"

mkdir -p "${artifact_dir}"

domain="$(awk -F'"' '/^forge_metal_domain:/{print $2}' "${vars_file}")"
remote_host="$(grep -m1 'ansible_host=' "${inventory}" | sed 's/.*ansible_host=\([^ ]*\).*/\1/')"
remote_user="$(grep -m1 'ansible_user=' "${inventory}" | sed 's/.*ansible_user=\([^ ]*\).*/\1/')"

"${script_dir}/ensure-verification-repo.sh" "${artifact_dir}/repo.json"

(
  cd "${platform_root}/ansible"
  ansible-playbook -i inventory/hosts.ini playbooks/verification-reset.yml
  ansible-playbook -i inventory/hosts.ini playbooks/seed-demo.yml
)

demo_password="$(
  ssh -o IPQoS=none -o StrictHostKeyChecking=no "${remote_user}@${remote_host}" \
    "sudo cat /etc/credstore/seed-demo/demo-user-password"
)"

verification_repo_url="$(
  python3 - "${artifact_dir}/repo.json" <<'PY'
import json
import sys

with open(sys.argv[1], "r", encoding="utf-8") as fh:
    repo = json.load(fh)
print(repo["loopback_repo_url"])
PY
)"

set +e
TEST_PASSWORD="${demo_password}" \
BASE_URL="${BASE_URL:-https://rentasandbox.${domain}}" \
VERIFICATION_RUN_ID="${run_id}" \
VERIFICATION_RUN_JSON_PATH="${artifact_dir}/run.json" \
VERIFICATION_REPO_URL="${verification_repo_url}" \
VERIFICATION_REPO_REF="refs/heads/main" \
VERIFICATION_LOG_MARKER="FORGE_METAL_VERIFICATION_NEXT_BUN_COMPLETE" \
pnpm -C "${repo_root}/src/viteplus-monorepo" --filter @forge-metal/rent-a-sandbox \
  exec playwright test e2e/repo-exec-live.spec.ts --project=chromium --output "${artifact_dir}/playwright-results"
playwright_status=$?
set -e

if [[ -f "${artifact_dir}/run.json" ]]; then
  "${script_dir}/collect-sandbox-verification-evidence.sh" "${artifact_dir}/run.json" "${artifact_dir}/evidence"
fi

exit "${playwright_status}"
