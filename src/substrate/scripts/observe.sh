#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
self_path="${script_dir}/$(basename "${BASH_SOURCE[0]}")"
platform_root="$(cd "${script_dir}/.." && pwd)"
repo_root="$(cd "${platform_root}/../.." && pwd)"
inventory="${INVENTORY:-${platform_root}/ansible/inventory/${VERSELF_SITE:-prod}.ini}"

if [[ ! -f "${inventory}" ]]; then
  echo "ERROR: ${inventory} not found. Run 'aspect platform provision' first." >&2
  exit 1
fi

# Re-exec under the controller-side OTLP buffer agent. The observe CLI
# emits spans for each query it issues; the agent buffers them through
# scripts/with-otel-agent.sh's file_storage queue so observe-side latency
# never races the SSH tunnel teardown.
if [[ -z "${VERSELF_OTEL_AGENT_INNER:-}" ]]; then
  export VERSELF_OTEL_AGENT_INNER=1
  export VERSELF_ANSIBLE_INVENTORY="${inventory}"
  export VERSELF_DEPLOY_KIND="${VERSELF_DEPLOY_KIND:-observe}"
  exec "${script_dir}/with-otel-agent.sh" "${self_path}" "$@"
fi

export VERSELF_OBSERVE_RUN_ID="${VERSELF_OBSERVE_RUN_ID:-${VERSELF_DEPLOY_RUN_KEY}}"

cd "${repo_root}/src/otel"
exec go run ./cmd/observe --platform-root "${platform_root}" "$@"
