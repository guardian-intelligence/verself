#!/usr/bin/env bash
# Fast path for shipping console UI changes: build locally, rsync
# .output/, restart the service. No Ansible, no Zitadel reconcile, no
# OIDC/env/systemd/nftables rechecks.
#
# Safe when you've only touched:
#   - src/viteplus-monorepo/apps/console/src/**
#   - src/viteplus-monorepo/packages/**  (UI + env + auth-web)
#
# NOT safe (run the Ansible role via `ansible-playbook ... --tags console`):
#   - Go API changed (regenerates __generated/**)
#   - console.service.j2 or env.j2 templates changed
#   - nftables rules changed
#   - OIDC app / Zitadel roles changed
#   - First-ever deploy (needs credstore + systemd unit + Zitadel app creation)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=src/platform/scripts/lib/verification-context.sh
source "${SCRIPT_DIR}/lib/verification-context.sh"
verification_context_init "${BASH_SOURCE[0]}"

REMOTE="${VERIFICATION_REMOTE_USER}@${VERIFICATION_REMOTE_HOST}"
MONOREPO_DIR="${VERIFICATION_REPO_ROOT}/src/viteplus-monorepo"
LOCAL_OUTPUT="${MONOREPO_DIR}/apps/console/.output/"
REMOTE_OUTPUT="/opt/console/apps/console/.output/"
SERVICE_HOST="127.0.0.1"
SERVICE_PORT="4244"

step() { printf '\n\033[1;34m==> %s\033[0m\n' "$*"; }

step "Build @verself/console (local, dependency-aware via vp)"
# vp run <pkg>#<script> builds the target's workspace dependencies first, then
# the target itself. Uses Vite+'s build cache, so unchanged packages skip.
pushd "${MONOREPO_DIR}" >/dev/null
vp run "@verself/console#build"
popd >/dev/null

if [[ ! -d "${LOCAL_OUTPUT}" ]]; then
  echo "build did not produce ${LOCAL_OUTPUT}" >&2
  exit 1
fi

step "Rsync .output/ -> ${REMOTE}:${REMOTE_OUTPUT}"
# -a preserves perms/times, -z compresses over ssh, --delete trims stale files,
# --inplace rewrites files in place so we don't briefly have .output + .output.tmp
# on the filesystem (also faster for large artefacts that barely change).
rsync -az --delete --inplace \
  -e "ssh ${VERIFICATION_SSH_OPTS[*]}" \
  "${LOCAL_OUTPUT}" \
  "${REMOTE}:${REMOTE_OUTPUT}"

step "Restart console"
verification_ssh sudo systemctl restart console

step "Wait for readiness on ${SERVICE_HOST}:${SERVICE_PORT}"
verification_ssh bash -s <<EOF
for attempt in \$(seq 1 60); do
  if curl -fsS -o /dev/null "http://${SERVICE_HOST}:${SERVICE_PORT}/"; then
    exit 0
  fi
  sleep 0.25
done
echo "console did not become ready on ${SERVICE_HOST}:${SERVICE_PORT}" >&2
exit 1
EOF

step "Done"
