#!/usr/bin/env bash
# Send a test email through Resend.
#
# Usage:
#   ./scripts/mail-send.sh -t agents -s "Test subject" -b "hello"
#   ./scripts/mail-send.sh -t ceo -s "Test subject" < body.txt
#   ./scripts/mail-send.sh -t user@example.com -s "Direct address" -b "hello"
set -euo pipefail

TARGET=""
SUBJECT=""
BODY="${BODY:-}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    -t|--to) TARGET="$2"; shift 2 ;;
    -s|--subject) SUBJECT="$2"; shift 2 ;;
    -b|--body) BODY="$2"; shift 2 ;;
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

main_file="${MAIN_FILE:-ansible/group_vars/all/main.yml}"
secrets_file="${SOPS_SECRETS_FILE:-ansible/group_vars/all/secrets.sops.yml}"

if [[ ! -f "$main_file" ]]; then
  echo "ERROR: $main_file not found." >&2
  exit 1
fi
if [[ ! -f "$secrets_file" ]]; then
  echo "ERROR: $secrets_file not found." >&2
  exit 1
fi

domain="$(awk '/^forge_metal_domain:/ {gsub(/"/, "", $2); print $2}' "$main_file")"
resend_subdomain="$(awk '/^resend_subdomain:/ {gsub(/"/, "", $2); print $2}' "$main_file")"
from_name="$(awk '/^resend_sender_name:/ {$1=""; sub(/^ /, ""); gsub(/"/, "", $0); print $0}' "$main_file")"

if [[ -z "$domain" || -z "$resend_subdomain" || -z "$from_name" ]]; then
  echo "ERROR: could not resolve Resend config from $main_file" >&2
  exit 1
fi

from_address="noreply@${resend_subdomain}.${domain}"
if [[ "$TARGET" == "ceo" || "$TARGET" == "agents" ]]; then
  to_address="${TARGET}@${domain}"
else
  to_address="$TARGET"
fi

api_key="$(sops -d --extract '["resend_api_key"]' "$secrets_file")"
if [[ -z "$api_key" ]]; then
  echo "ERROR: resend_api_key is empty in $secrets_file" >&2
  exit 1
fi

payload="$(python3 - "$from_name" "$from_address" "$to_address" "$SUBJECT" "$BODY" <<'PY'
import json
import sys

from_name, from_address, to_address, subject, body = sys.argv[1:]
print(json.dumps({
    "from": f"{from_name} <{from_address}>",
    "to": [to_address],
    "subject": subject,
    "text": body,
}))
PY
)"

response="$(
  curl -fsS https://api.resend.com/emails \
    -H "Authorization: Bearer $api_key" \
    -H "Content-Type: application/json" \
    -d "$payload"
)"

python3 - "$to_address" "$from_address" "$response" <<'PY'
import json
import sys

to_address, from_address, raw = sys.argv[1:]
resp = json.loads(raw)
print(f"sent via Resend: {from_address} -> {to_address}")
if "id" in resp:
    print(f"resend_id: {resp['id']}")
else:
    print(raw)
PY
