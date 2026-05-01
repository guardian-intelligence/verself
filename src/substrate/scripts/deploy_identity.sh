#!/usr/bin/env bash
# deploy_identity.sh — derive deterministic deploy identity + OTel env vars.
#
# Sourced (not executed) by scripts that invoke ansible-playbook, to give
# every deploy a stable trace-id derived from (date, counter, host). The
# upstream community.general.opentelemetry Ansible callback honors the
# W3C TRACEPARENT env var and merges OTEL_RESOURCE_ATTRIBUTES onto every
# exported span's Resource — so once this has run, every subsequent trace
# from the playbook AND from verself_uri probes shares the same trace-id and
# carries verself.* on ResourceAttributes.
#
# Idempotent: if VERSELF_DEPLOY_ID and VERSELF_DEPLOY_RUN_KEY are already set
# by a caller that wants its own correlation id, those are preserved and the
# rest is derived from them.
#
# Usage:
#   source "${VERSELF_SUBSTRATE}/scripts/deploy_identity.sh"
#   ansible-playbook ...

set -eu

__deploy_identity_counter() {
  local day="$1"
  local host="$2"
  local cache_dir="${XDG_CACHE_HOME:-${HOME}/.cache}/verself/deploy-runs"
  mkdir -p "${cache_dir}"
  local lock_file="${cache_dir}/${day}.${host}.lock"
  local counter_file="${cache_dir}/${day}.${host}.counter"
  python3 - "${lock_file}" "${counter_file}" <<'PY'
import fcntl, pathlib, sys
lock_path = pathlib.Path(sys.argv[1])
counter_path = pathlib.Path(sys.argv[2])
with lock_path.open("a+") as lock_file:
    fcntl.flock(lock_file, fcntl.LOCK_EX)
    try:
        current = int(counter_path.read_text(encoding="utf-8").strip() or "0")
    except (FileNotFoundError, ValueError):
        current = 0
    current += 1
    counter_path.write_text(str(current), encoding="utf-8")
    print(f"{current:06d}")
PY
}

__deploy_identity_run_key() {
  if [ -n "${VERSELF_DEPLOY_RUN_KEY:-}" ]; then
    printf '%s' "${VERSELF_DEPLOY_RUN_KEY}"
    return
  fi
  local day host counter
  day="$(date -u +%Y-%m-%d)"
  host="$(hostname -s 2>/dev/null || hostname || echo controller)"
  # Strip shell-unfriendly chars from hostname for filename safety.
  host="$(printf '%s' "${host}" | tr -c 'A-Za-z0-9_.-' '_')"
  counter="$(__deploy_identity_counter "${day}" "${host}")"
  printf '%s.%s@%s' "${day}" "${counter}" "${host}"
}

__deploy_identity_uuid5() {
  python3 -c 'import sys, uuid; print(uuid.uuid5(uuid.NAMESPACE_URL, f"verself:{sys.argv[1]}"))' "$1"
}

__deploy_identity_span_id() {
  python3 -c 'import hashlib, sys; print(hashlib.sha256(sys.argv[1].encode()).hexdigest()[:16])' "$1"
}

VERSELF_DEPLOY_RUN_KEY="$(__deploy_identity_run_key)"
export VERSELF_DEPLOY_RUN_KEY
if [ -z "${VERSELF_DEPLOY_ID:-}" ]; then
  VERSELF_DEPLOY_ID="$(__deploy_identity_uuid5 "${VERSELF_DEPLOY_RUN_KEY}")"
fi
export VERSELF_DEPLOY_ID

# TRACEPARENT = 00-<32hex trace>-<16hex span>-01. The upstream Ansible OTel
# callback extracts this and makes its playbook span a child of it; verself_uri
# emits probes using the same trace-id so everything shares the root.
__trace_hex="$(printf '%s' "${VERSELF_DEPLOY_ID}" | tr -d '-')"
__span_hex="$(__deploy_identity_span_id "deploy-root:${VERSELF_DEPLOY_ID}")"
export TRACEPARENT="00-${__trace_hex}-${__span_hex}-01"

# Git metadata lands as Resource attributes on every span emitted by the
# callback, so they survive into ClickHouse ResourceAttributes for reporting.
__repo_root="$(git rev-parse --show-toplevel 2>/dev/null || echo '')"
__git() { git -C "${__repo_root}" "$@" 2>/dev/null || true; }
VERSELF_COMMIT_SHA="$(__git rev-parse HEAD)"
VERSELF_BRANCH="$(__git rev-parse --abbrev-ref HEAD)"
VERSELF_COMMIT_MESSAGE="$(__git log -1 --format=%s)"
VERSELF_AUTHOR="$(__git log -1 --format=%ae)"
export VERSELF_COMMIT_SHA
export VERSELF_BRANCH
export VERSELF_COMMIT_MESSAGE
export VERSELF_AUTHOR
if [ -n "$(__git status --porcelain)" ]; then
  export VERSELF_DIRTY="true"
else
  export VERSELF_DIRTY="false"
fi
export VERSELF_DEPLOY_KIND="${VERSELF_DEPLOY_KIND:-ansible-playbook}"

# Per-deploy site/sha/scope are populated by the aspect deploy task before
# sourcing this script. They flow onto every span via OTEL_RESOURCE_ATTRIBUTES
# so a `verself.deploy_run_key` join lets queries filter by site or scope
# without re-reading deploy_events.
export VERSELF_SITE="${VERSELF_SITE:-}"
export VERSELF_DEPLOY_SHA="${VERSELF_DEPLOY_SHA:-${VERSELF_COMMIT_SHA}}"
export VERSELF_DEPLOY_SCOPE="${VERSELF_DEPLOY_SCOPE:-all}"

# OTLP endpoint: scripts/with-otel-agent.sh sets VERSELF_OTLP_ENDPOINT to
# the controller-side agent's fixed receiver port. The fallback exists so
# this script remains sourceable in contexts that only need the
# correlation IDs (record-deploy-event.sh) and never actually export.
export OTEL_EXPORTER_OTLP_ENDPOINT="http://${VERSELF_OTLP_ENDPOINT:-127.0.0.1:14317}"
export OTEL_SERVICE_NAME="ansible"

# OTEL_RESOURCE_ATTRIBUTES members are comma-separated key=value pairs. The
# SDK decodes percent-encoded values on read. Commit messages can contain
# commas, equals signs, etc. — percent-encode with python to stay within
# the spec.
OTEL_RESOURCE_ATTRIBUTES="$(python3 - <<'PY'
import os, urllib.parse as up
parts = [
    ("verself.deploy_id", os.environ["VERSELF_DEPLOY_ID"]),
    ("verself.deploy_run_key", os.environ["VERSELF_DEPLOY_RUN_KEY"]),
    ("verself.commit_sha", os.environ["VERSELF_COMMIT_SHA"]),
    ("verself.branch", os.environ["VERSELF_BRANCH"]),
    ("verself.commit_message", os.environ["VERSELF_COMMIT_MESSAGE"]),
    ("verself.author", os.environ["VERSELF_AUTHOR"]),
    ("verself.dirty", os.environ["VERSELF_DIRTY"]),
    ("verself.deploy_kind", os.environ["VERSELF_DEPLOY_KIND"]),
    ("verself.site", os.environ.get("VERSELF_SITE", "")),
    ("verself.deploy_sha", os.environ.get("VERSELF_DEPLOY_SHA", "")),
    ("verself.deploy_scope", os.environ.get("VERSELF_DEPLOY_SCOPE", "")),
]
print(",".join(f"{k}={up.quote(v, safe='')}" for k, v in parts if v))
PY
)"
export OTEL_RESOURCE_ATTRIBUTES

unset -f __deploy_identity_counter __deploy_identity_run_key __deploy_identity_uuid5 __deploy_identity_span_id __git
unset __repo_root __trace_hex __span_hex
