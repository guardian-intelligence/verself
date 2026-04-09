#!/usr/bin/env bash
# Inject a test email directly into the local Stalwart SMTP listener over SSH.
#
# Usage:
#   ./scripts/mail-send-local.sh -t ceo -s "Test subject" -b "hello"
#   ./scripts/mail-send-local.sh -t demo@example.com -s "Test subject" < body.txt
set -euo pipefail

TARGET=""
SUBJECT=""
BODY="${BODY:-}"
FROM_ADDRESS="${FROM_ADDRESS:-}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    -t|--to) TARGET="$2"; shift 2 ;;
    -s|--subject) SUBJECT="$2"; shift 2 ;;
    -b|--body) BODY="$2"; shift 2 ;;
    -f|--from) FROM_ADDRESS="$2"; shift 2 ;;
    -h|--help)
      sed -n '2,7p' "$0" | sed 's/^# \?//'
      exit 0
      ;;
    *)
      echo "Unknown option: $1" >&2
      exit 1
      ;;
  esac
done

if [[ -z "$BODY" && ! -t 0 ]]; then
  BODY="$(cat)"
fi

if [[ -z "$TARGET" ]]; then
  echo "ERROR: --to is required" >&2
  exit 1
fi
if [[ -z "$SUBJECT" ]]; then
  echo "ERROR: --subject is required" >&2
  exit 1
fi
if [[ -z "$BODY" ]]; then
  echo "ERROR: body is required (use --body or stdin)" >&2
  exit 1
fi

inventory="${INVENTORY:-ansible/inventory/hosts.ini}"
main_file="${MAIN_FILE:-ansible/group_vars/all/main.yml}"
ssh_opts=(-o IPQoS=none -o StrictHostKeyChecking=no)

if [[ ! -f "$inventory" ]]; then
  echo "ERROR: $inventory not found." >&2
  exit 1
fi
if [[ ! -f "$main_file" ]]; then
  echo "ERROR: $main_file not found." >&2
  exit 1
fi

domain="$(awk '/^forge_metal_domain:/ {gsub(/"/, "", $2); print $2}' "$main_file")"
resend_subdomain="$(awk '/^resend_subdomain:/ {gsub(/"/, "", $2); print $2}' "$main_file")"
remote_host="$(grep -m1 'ansible_host=' "$inventory" | sed 's/.*ansible_host=\([^ ]*\).*/\1/')"
remote_user="$(grep -m1 'ansible_user=' "$inventory" | sed 's/.*ansible_user=\([^ ]*\).*/\1/')"

if [[ -z "$domain" || -z "$remote_host" || -z "$remote_user" ]]; then
  echo "ERROR: could not resolve domain or SSH target" >&2
  exit 1
fi

if [[ -z "$FROM_ADDRESS" ]]; then
  if [[ -n "$resend_subdomain" ]]; then
    FROM_ADDRESS="noreply@${resend_subdomain}.${domain}"
  else
    FROM_ADDRESS="noreply@${domain}"
  fi
fi

if [[ "$TARGET" == "ceo" || "$TARGET" == "agents" ]]; then
  to_address="${TARGET}@${domain}"
else
  to_address="$TARGET"
fi

request_payload="$(python3 - "$FROM_ADDRESS" "$to_address" "$SUBJECT" "$BODY" <<'PY'
import json
import sys

from_address, to_address, subject, body = sys.argv[1:]
print(json.dumps({
    "from_address": from_address,
    "to_address": to_address,
    "subject": subject,
    "body": body,
}))
PY
)"

read -r -d '' REMOTE_SCRIPT <<'PYEOF' || true
import json
import smtplib
import sys
from email.message import EmailMessage

payload = json.load(sys.stdin)

msg = EmailMessage()
msg["From"] = payload["from_address"]
msg["To"] = payload["to_address"]
msg["Subject"] = payload["subject"]
msg.set_content(payload["body"])

with smtplib.SMTP("127.0.0.1", 25, timeout=5) as smtp:
    smtp.send_message(msg)

print(f"sent via local SMTP: {payload['from_address']} -> {payload['to_address']}")
PYEOF

exec ssh "${ssh_opts[@]}" "${remote_user}@${remote_host}" \
  "python3 -c $(printf '%q' "$REMOTE_SCRIPT")" <<<"$request_payload"
