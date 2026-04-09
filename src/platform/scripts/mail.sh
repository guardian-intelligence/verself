#!/usr/bin/env bash
# Query mailbox-service operator APIs through the generated OpenAPI client.
#
# Usage:
#   ./scripts/mail.sh                            # List agents@ inbox (recent 10)
#   ./scripts/mail.sh -u ceo                     # List ceo@ inbox
#   ./scripts/mail.sh -u ceo -n 20               # List 20 most recent from ceo@
#   ./scripts/mail.sh -c                         # Extract latest verification/2FA code from agents@
#   ./scripts/mail.sh -r ID                      # Read full email by synced email ID from agents@
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/../../.." && pwd)"
inventory="${INVENTORY:-ansible/inventory/hosts.ini}"
if [[ "${inventory}" != /* ]]; then
  inventory="${repo_root}/src/platform/${inventory}"
fi

if [[ $# -gt 0 ]]; then
  case "$1" in
    accounts|mailboxes|list|read|code|help|-h|--help)
      cd "${repo_root}/src/mailbox-service"
      exec go run ./cmd/mailbox-tool --inventory "${inventory}" "$@"
      ;;
  esac
fi

MAILBOX_ACCOUNT="agents"
MODE="list"
EMAIL_ID=""
LIMIT=10

while [[ $# -gt 0 ]]; do
  case "$1" in
    -u|--user) MAILBOX_ACCOUNT="$2"; shift 2 ;;
    -c|--code) MODE="code"; shift ;;
    -r|--read) MODE="read"; EMAIL_ID="$2"; shift 2 ;;
    -n|--limit) LIMIT="$2"; shift 2 ;;
    -h|--help)
      sed -n '2,9p' "$0" | sed 's/^# \?//'
      exit 0
      ;;
    *)
      echo "Unknown option: $1" >&2
      exit 1
      ;;
  esac
done

if [[ "$MAILBOX_ACCOUNT" != "agents" && "$MAILBOX_ACCOUNT" != "ceo" ]]; then
  echo "ERROR: unknown mailbox account '$MAILBOX_ACCOUNT' (valid: agents, ceo)" >&2
  exit 1
fi

tool_args=(go run ./cmd/mailbox-tool --inventory "${inventory}")
case "$MODE" in
  list)
    tool_args+=(list --account "${MAILBOX_ACCOUNT}" --limit "${LIMIT}")
    ;;
  read)
    if [[ -z "$EMAIL_ID" ]]; then
      echo "ERROR: -r requires an email ID" >&2
      exit 1
    fi
    tool_args+=(read --account "${MAILBOX_ACCOUNT}" --id "${EMAIL_ID}")
    ;;
  code)
    tool_args+=(code --account "${MAILBOX_ACCOUNT}")
    ;;
  *)
    echo "ERROR: unknown mode '${MODE}'" >&2
    exit 1
    ;;
esac

cd "${repo_root}/src/mailbox-service"
exec "${tool_args[@]}"
