#!/usr/bin/env bash
set -euo pipefail

inventory="${INVENTORY:-ansible/inventory/${VERSELF_SITE:-prod}.ini}"
remote_path="${CLICKHOUSE_CLIENT_PATH:-/opt/verself/profile/bin/clickhouse-client}"
remote_config_path="${CLICKHOUSE_CLIENT_CONFIG_PATH:-/etc/clickhouse-client/operator.xml}"
remote_run_as_user="${CLICKHOUSE_RUN_AS_USER:-clickhouse_operator}"
remote_db_user="${CLICKHOUSE_DB_USER:-clickhouse_operator}"
ssh_opts=(-o IPQoS=none -o StrictHostKeyChecking=no -o BatchMode=yes -o ConnectTimeout=5)

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
remote_config_path_q="$(printf '%q' "$remote_config_path")"
remote_run_as_user_q="$(printf '%q' "$remote_run_as_user")"
remote_db_user_q="$(printf '%q' "$remote_db_user")"

if [[ $# -eq 0 ]]; then
  echo "ERROR: interactive ClickHouse shells are not supported. Use: aspect db ch query --query='SELECT 1'" >&2
  exit 2
fi

remote_args=()
for arg in "$@"; do
  remote_args+=("$(printf '%q' "$arg")")
done

exec ssh "${ssh_opts[@]}" "${remote_user}@${remote_host}" \
  "sudo -u ${remote_run_as_user_q} bash -lc 'exec \"\$1\" --config-file \"\$2\" --user \"\$3\" \"\${@:4}\"' _ ${remote_path_q} ${remote_config_path_q} ${remote_db_user_q}${remote_args:+ ${remote_args[*]}}"
