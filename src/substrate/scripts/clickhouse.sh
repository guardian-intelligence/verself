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
  echo "ERROR: $inventory not found. Run 'aspect provision apply' first." >&2
  exit 1
fi

remote_host="$(
  INVENTORY_PATH="$inventory" python3 <<'PY'
import os

path = os.environ["INVENTORY_PATH"]
first_host = ""
infra_host = ""
section = ""
with open(path, encoding="utf-8") as f:
    for raw in f:
        line = raw.strip()
        if not line or line.startswith("#"):
            continue
        if line.startswith("[") and line.endswith("]"):
            section = line[1:-1]
            continue
        first = line.split()[0]
        if section.endswith(":vars") or "=" in first:
            continue
        host = first
        for field in line.split()[1:]:
            if field.startswith("ansible_host="):
                host = field.split("=", 1)[1]
                break
        if not first_host:
            first_host = host
        if section == "infra" and not infra_host:
            infra_host = host
print(infra_host or first_host)
PY
)"
remote_user="$(grep -m1 'ansible_user=' "$inventory" | sed 's/.*ansible_user=\([^ ]*\).*/\1/' || true)"

if [[ -z "$remote_host" ]]; then
  echo "ERROR: could not resolve inventory host from $inventory" >&2
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
