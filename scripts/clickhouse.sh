#!/usr/bin/env bash
set -euo pipefail

inventory="${INVENTORY:-ansible/inventory/hosts.ini}"
credentials_file="${CLICKHOUSE_PASSWORD_FILE:-ansible/.credentials/clickhouse_password}"
remote_path="${CLICKHOUSE_CLIENT_PATH:-/opt/forge-metal/profile/bin/clickhouse-client}"
ssh_opts=(-o StrictHostKeyChecking=no)

if [[ -n "${SSH_OPTS:-}" ]]; then
  read -r -a ssh_opts <<<"${SSH_OPTS}"
fi

if [[ ! -f "$inventory" ]]; then
  echo "ERROR: $inventory not found. Run 'make provision' first." >&2
  exit 1
fi

if [[ ! -f "$credentials_file" ]]; then
  echo "ERROR: $credentials_file not found. Run 'make deploy' first." >&2
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

clickhouse_password="$(<"$credentials_file")"
remote_password_q="$(printf '%q' "$clickhouse_password")"
remote_path_q="$(printf '%q' "$remote_path")"

if [[ $# -eq 0 ]]; then
  exec ssh "${ssh_opts[@]}" -t "${remote_user}@${remote_host}" \
    "sudo env CLICKHOUSE_PASSWORD=${remote_password_q} bash -lc 'exec \"\$1\" --user default --password \"\$CLICKHOUSE_PASSWORD\"' _ ${remote_path_q}"
fi

remote_args=()
for arg in "$@"; do
  remote_args+=("$(printf '%q' "$arg")")
done

exec ssh "${ssh_opts[@]}" "${remote_user}@${remote_host}" \
  "sudo env CLICKHOUSE_PASSWORD=${remote_password_q} bash -lc 'exec \"\$1\" --user default --password \"\$CLICKHOUSE_PASSWORD\" \"\${@:2}\"' _ ${remote_path_q}${remote_args:+ ${remote_args[*]}}"
