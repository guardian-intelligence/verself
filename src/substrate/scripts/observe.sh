#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
self_path="${script_dir}/$(basename "${BASH_SOURCE[0]}")"
substrate_root="$(cd "${script_dir}/.." && pwd)"
repo_root="$(cd "${substrate_root}/../.." && pwd)"
inventory="${INVENTORY:-${substrate_root}/ansible/inventory/${VERSELF_SITE:-prod}.ini}"

if [[ ! -f "${inventory}" ]]; then
  echo "ERROR: ${inventory} not found. Run 'aspect provision apply' first." >&2
  exit 1
fi

# Re-exec under the verself-deploy controller-side OTLP agent. The
# observe CLI emits spans for each query it issues; the agent's
# file_storage queue decouples drain from process exit so observe-side
# latency never races SSH tunnel teardown.
if [[ -z "${VERSELF_OTEL_AGENT_INNER:-}" ]]; then
  export VERSELF_OTEL_AGENT_INNER=1
  export VERSELF_DEPLOY_KIND="${VERSELF_DEPLOY_KIND:-observe}"
  bin="${repo_root}/bazel-bin/src/deployment-tooling/cmd/verself-deploy/verself-deploy_/verself-deploy"
  if [[ ! -x "${bin}" ]]; then
    echo "[observe] building //src/deployment-tooling/cmd/verself-deploy" >&2
    (cd "${repo_root}" && bazelisk build --config=remote-writer //src/deployment-tooling/cmd/verself-deploy:verself-deploy)
  fi
  exec "${bin}" with-agent --site="${VERSELF_SITE:-prod}" --repo-root="${repo_root}" -- "${self_path}" "$@"
fi

export VERSELF_OBSERVE_RUN_ID="${VERSELF_OBSERVE_RUN_ID:-${VERSELF_DEPLOY_RUN_KEY:-}}"

cd "${repo_root}/src/otel"
exec go run ./cmd/observe --substrate-root "${substrate_root}" "$@"
