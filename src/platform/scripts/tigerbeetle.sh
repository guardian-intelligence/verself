#!/usr/bin/env bash
set -euo pipefail

# Drop into the TigerBeetle REPL against the live cluster on the worker.
#
# Usage:
#   tigerbeetle.sh                              # interactive REPL (Ctrl+D to exit)
#   tigerbeetle.sh --command 'query_accounts limit=10;'
#
# TigerBeetle has no ad-hoc query language — the REPL operates on typed ops:
# create_accounts, create_transfers, lookup_accounts, lookup_transfers,
# get_account_transfers, get_account_balances, query_accounts, query_transfers.
# See https://docs.tigerbeetle.com/reference/client/repl/ for the grammar.

inventory="${INVENTORY:-ansible/inventory/hosts.ini}"
remote_path="${TIGERBEETLE_PATH:-/opt/verself/profile/bin/tigerbeetle}"
tb_cluster="${TIGERBEETLE_CLUSTER_ID:-0}"
tb_addresses="${TIGERBEETLE_ADDRESSES:-127.0.0.1:3320}"
ssh_opts=(-o IPQoS=none -o StrictHostKeyChecking=no)

if [[ -n "${SSH_OPTS:-}" ]]; then
  read -r -a ssh_opts <<<"${SSH_OPTS}"
fi

if [[ ! -f "$inventory" ]]; then
  echo "ERROR: $inventory not found. Run 'cd ansible && ansible-playbook playbooks/provision.yml' first." >&2
  exit 1
fi

remote_host="$(grep -m1 'ansible_host=' "$inventory" | sed 's/.*ansible_host=\([^ ]*\).*/\1/')"
remote_user="$(grep -m1 'ansible_user=' "$inventory" | sed 's/.*ansible_user=\([^ ]*\).*/\1/')"

if [[ -z "$remote_host" ]]; then
  echo "ERROR: no ansible_host found in $inventory" >&2
  exit 1
fi

if [[ -z "$remote_user" ]]; then
  echo "ERROR: no ansible_user found in $inventory" >&2
  exit 1
fi

remote_path_q="$(printf '%q' "$remote_path")"

tb_argv=("repl" "--cluster=${tb_cluster}" "--addresses=${tb_addresses}")

interactive=true
while [[ $# -gt 0 ]]; do
  case "$1" in
    --command|-c)
      shift
      if [[ $# -eq 0 ]]; then
        echo "ERROR: --command requires an argument" >&2
        exit 1
      fi
      tb_argv+=("--command=$1")
      interactive=false
      shift
      ;;
    *)
      tb_argv+=("$1")
      shift
      ;;
  esac
done

remote_args_q=""
for a in "${tb_argv[@]}"; do
  remote_args_q+=" $(printf '%q' "$a")"
done

if $interactive; then
  # Interactive REPL: allocate a PTY so TigerBeetle can emit ANSI escapes.
  exec ssh "${ssh_opts[@]}" -t "${remote_user}@${remote_host}" \
    "sudo -u tigerbeetle bash -lc 'exec \"\$1\" \"\${@:2}\"' _ ${remote_path_q}${remote_args_q}"
fi

# Footgun: `tigerbeetle repl --command=<op>;` exits with status 1 on both success
# and failure — it treats the synthetic EOF after --command as an error. Mask
# that specific code to 0 so callers can use `set -e`; pass any other code through.
exec ssh "${ssh_opts[@]}" "${remote_user}@${remote_host}" \
  "sudo -u tigerbeetle bash -lc 'set +e; \"\$1\" \"\${@:2}\"; code=\$?; if [[ \$code -eq 1 ]]; then exit 0; fi; exit \$code' _ ${remote_path_q}${remote_args_q}"
