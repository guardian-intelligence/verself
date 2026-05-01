#!/usr/bin/env bash
set -euo pipefail

# Run psql against a managed PostgreSQL database on the worker.
#
# Usage:
#   pg.sh <database>                      # interactive psql shell
#   pg.sh <database> --query 'SELECT 1'   # one-off query (psql -c)
#   pg.sh --list                          # \l equivalent, lists databases
#
# Authenticates as the postgres superuser using the SOPS-stored
# postgresql_admin_password. The script intentionally does not validate the
# database name against a hardcoded enum — psql's own error is authoritative
# and `pg.sh --list` shows the live state.

inventory="${INVENTORY:-ansible/inventory/${VERSELF_SITE:-prod}.ini}"
secrets_file="${SOPS_SECRETS_FILE:-ansible/group_vars/all/secrets.sops.yml}"
remote_path="${PSQL_PATH:-/opt/verself/profile/bin/psql}"
pg_host="${PG_HOST:-127.0.0.1}"
pg_port="${PG_PORT:-5432}"
pg_user="${PG_USER:-postgres}"
ssh_opts=(-o IPQoS=none -o StrictHostKeyChecking=no)

if [[ -n "${SSH_OPTS:-}" ]]; then
  read -r -a ssh_opts <<<"${SSH_OPTS}"
fi

if [[ ! -f "$inventory" ]]; then
  echo "ERROR: $inventory not found. Run 'aspect provision apply' first." >&2
  exit 1
fi

if [[ ! -f "$secrets_file" ]]; then
  echo "ERROR: $secrets_file not found. Run 'aspect dev sops-init' first." >&2
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

pg_password="$(sops -d --extract '["postgresql_admin_password"]' "$secrets_file")"
remote_password_q="$(printf '%q' "$pg_password")"
remote_path_q="$(printf '%q' "$remote_path")"

list_mode=false
database=""
extra_args=()
while [[ $# -gt 0 ]]; do
  case "$1" in
    --list|-l)
      list_mode=true
      shift
      ;;
    --query|-c)
      shift
      if [[ $# -eq 0 ]]; then
        echo "ERROR: --query requires an argument" >&2
        exit 1
      fi
      extra_args+=("-c" "$1")
      shift
      ;;
    --)
      shift
      extra_args+=("$@")
      break
      ;;
    -*)
      extra_args+=("$1")
      shift
      ;;
    *)
      if [[ -z "$database" ]]; then
        database="$1"
      else
        extra_args+=("$1")
      fi
      shift
      ;;
  esac
done

psql_argv=("--host=${pg_host}" "--port=${pg_port}" "--username=${pg_user}")

if $list_mode; then
  psql_argv+=("--list")
else
  if [[ -z "$database" ]]; then
    echo "ERROR: database name is required (use --list to see databases)" >&2
    exit 1
  fi
  psql_argv+=("--dbname=${database}")
  psql_argv+=("${extra_args[@]}")
fi

remote_args_q=""
for a in "${psql_argv[@]}"; do
  remote_args_q+=" $(printf '%q' "$a")"
done

# Allocate a PTY only when there are no -c/--list flags (i.e. interactive).
tty_flag=""
if ! $list_mode && [[ ${#extra_args[@]} -eq 0 ]]; then
  tty_flag="-t"
fi

exec ssh "${ssh_opts[@]}" ${tty_flag} "${remote_user}@${remote_host}" \
  "sudo env PGPASSWORD=${remote_password_q} bash -lc 'exec \"\$1\" \"\${@:2}\"' _ ${remote_path_q}${remote_args_q}"
