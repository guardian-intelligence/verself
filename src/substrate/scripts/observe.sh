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

# Re-exec inside a verself-deploy SSH-forwarded OTLP channel so the
# observe CLI's per-query spans hit the bare-metal otelcol on the same
# tunnel deploy spans use.
if [[ -z "${VERSELF_OTEL_INNER:-}" ]]; then
  export VERSELF_OTEL_INNER=1
  export VERSELF_DEPLOY_KIND="${VERSELF_DEPLOY_KIND:-observe}"
  bin="${repo_root}/bazel-bin/src/deployment-tooling/cmd/verself-deploy/verself-deploy_/verself-deploy"
  if [[ ! -x "${bin}" ]]; then
    echo "[observe] building //src/deployment-tooling/cmd/verself-deploy" >&2
    (cd "${repo_root}" && bazelisk build --config=remote-writer //src/deployment-tooling/cmd/verself-deploy:verself-deploy)
  fi
  exec "${bin}" with-otel --site="${VERSELF_SITE:-prod}" --repo-root="${repo_root}" -- "${self_path}" "$@"
fi

export VERSELF_OBSERVE_RUN_ID="${VERSELF_OBSERVE_RUN_ID:-${VERSELF_DEPLOY_RUN_KEY:-}}"

cd "${repo_root}/src/otel"
exec go run ./cmd/observe --substrate-root "${substrate_root}" "$@"
