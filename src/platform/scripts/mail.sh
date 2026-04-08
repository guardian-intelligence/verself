#!/usr/bin/env bash
# Query Stalwart JMAP for inbox contents.
#
# Usage:
#   ./scripts/mail.sh                            # List ceo@ inbox (recent 10)
#   ./scripts/mail.sh -u bernoulli.agent         # List agent inbox
#   ./scripts/mail.sh -u bernoulli.agent -n 20   # List 20 most recent
#   ./scripts/mail.sh -u bernoulli.agent -c      # Extract latest verification/2FA code
#   ./scripts/mail.sh -u bernoulli.agent -r ID   # Read full email by JMAP ID
set -euo pipefail

STALWART_USER="ceo"
MODE="list"
EMAIL_ID=""
LIMIT=10

while [[ $# -gt 0 ]]; do
  case "$1" in
    -u|--user) STALWART_USER="$2"; shift 2 ;;
    -c|--code) MODE="code"; shift ;;
    -r|--read) MODE="read"; EMAIL_ID="$2"; shift 2 ;;
    -n|--limit) LIMIT="$2"; shift 2 ;;
    -h|--help)
      sed -n '2,7p' "$0" | sed 's/^# \?//'
      exit 0 ;;
    *) echo "Unknown option: $1" >&2; exit 1 ;;
  esac
done

inventory="${INVENTORY:-ansible/inventory/hosts.ini}"
secrets_file="${SOPS_SECRETS_FILE:-ansible/group_vars/all/secrets.sops.yml}"
ssh_opts=(-o IPQoS=none -o StrictHostKeyChecking=no)

if [[ ! -f "$inventory" ]]; then
  echo "ERROR: $inventory not found." >&2
  exit 1
fi

remote_host="$(grep -m1 'ansible_host=' "$inventory" | sed 's/.*ansible_host=\([^ ]*\).*/\1/')"
remote_user="$(grep -m1 'ansible_user=' "$inventory" | sed 's/.*ansible_user=\([^ ]*\).*/\1/')"

if [[ -z "$remote_host" || -z "$remote_user" ]]; then
  echo "ERROR: cannot parse inventory" >&2
  exit 1
fi

# Resolve password: agents use stalwart_agent_password, ceo uses seed demo password.
if [[ "$STALWART_USER" == "ceo" ]]; then
  password="${STALWART_CEO_PASSWORD:-SandboxDemo2026!#}"
else
  password="$(sops -d --extract '["stalwart_agent_password"]' "$secrets_file")"
fi

# Build the python3 script to run on the remote host.
# It queries JMAP with basic auth over loopback, then formats output.
read -r -d '' REMOTE_SCRIPT << 'PYEOF' || true
import json, sys, urllib.request, base64, re

user = sys.argv[1]
password = sys.argv[2]
mode = sys.argv[3]
email_id = sys.argv[4] if len(sys.argv) > 4 else ""
limit = int(sys.argv[5]) if len(sys.argv) > 5 else 10

base = "http://127.0.0.1:8090"
creds = base64.b64encode(f"{user}:{password}".encode()).decode()

def jmap(calls):
    body = json.dumps({
        "using": ["urn:ietf:params:jmap:core", "urn:ietf:params:jmap:mail"],
        "methodCalls": calls,
    }).encode()
    req = urllib.request.Request(f"{base}/jmap", data=body,
        headers={"Content-Type": "application/json",
                 "Authorization": f"Basic {creds}"})
    with urllib.request.urlopen(req) as r:
        return json.loads(r.read())

# Get account ID from session
req = urllib.request.Request(f"{base}/jmap/session",
    headers={"Authorization": f"Basic {creds}"})
with urllib.request.urlopen(req) as r:
    session = json.loads(r.read())
acct = list(session.get("accounts", {}).keys())[0]

if mode == "list":
    resp = jmap([
        ["Email/query", {"accountId": acct, "sort": [{"property": "receivedAt", "isAscending": False}], "limit": limit}, "q"],
        ["Email/get", {"accountId": acct, "#ids": {"resultOf": "q", "name": "Email/query", "path": "/ids"},
                       "properties": ["id", "subject", "receivedAt", "from", "preview"]}, "g"],
    ])
    emails = resp["methodResponses"][1][1].get("list", [])
    if not emails:
        print("(empty inbox)")
        sys.exit(0)
    for e in emails:
        fr = e.get("from", [{}])[0].get("email", "?")
        ts = e.get("receivedAt", "?")[:19]
        subj = e.get("subject", "(no subject)")
        print(f"  {e['id']}  {ts}  {fr:30s}  {subj}")

elif mode == "read":
    if not email_id:
        print("ERROR: -r requires an email ID", file=sys.stderr)
        sys.exit(1)
    resp = jmap([
        ["Email/get", {"accountId": acct, "ids": [email_id],
                       "properties": ["subject", "receivedAt", "from", "to", "textBody", "bodyValues"],
                       "fetchTextBodyValues": True}, "g"],
    ])
    emails = resp["methodResponses"][0][1].get("list", [])
    if not emails:
        print(f"Email {email_id} not found", file=sys.stderr)
        sys.exit(1)
    e = emails[0]
    fr = e.get("from", [{}])[0].get("email", "?")
    to = ", ".join(r.get("email", "?") for r in e.get("to", []))
    print(f"From:    {fr}")
    print(f"To:      {to}")
    print(f"Date:    {e.get('receivedAt', '?')}")
    print(f"Subject: {e.get('subject', '')}")
    print("---")
    for part in e.get("textBody", []):
        pid = part.get("partId", "")
        body = e.get("bodyValues", {}).get(pid, {}).get("value", "")
        print(body)

elif mode == "code":
    resp = jmap([
        ["Email/query", {"accountId": acct, "sort": [{"property": "receivedAt", "isAscending": False}], "limit": 5}, "q"],
        ["Email/get", {"accountId": acct, "#ids": {"resultOf": "q", "name": "Email/query", "path": "/ids"},
                       "properties": ["subject", "receivedAt", "from", "textBody", "bodyValues"],
                       "fetchTextBodyValues": True}, "g"],
    ])
    emails = resp["methodResponses"][1][1].get("list", [])
    code_patterns = [
        r'\b(\d{6})\b',
        r'(?:code|otp|token|pin)[:\s]+(\S+)',
        r'(?:verification|confirm)[:\s]+(\S+)',
    ]
    for e in emails:
        subj = e.get("subject", "")
        body = ""
        for part in e.get("textBody", []):
            pid = part.get("partId", "")
            body += e.get("bodyValues", {}).get(pid, {}).get("value", "")
        text = subj + " " + body
        for pat in code_patterns:
            m = re.search(pat, text, re.IGNORECASE)
            if m:
                fr = e.get("from", [{}])[0].get("email", "?")
                ts = e.get("receivedAt", "?")[:19]
                print(f"  {m.group(1)}  ({ts} from {fr}: {subj})")
                sys.exit(0)
    print("No verification code found in recent emails", file=sys.stderr)
    sys.exit(1)
PYEOF

exec ssh "${ssh_opts[@]}" "${remote_user}@${remote_host}" \
  "python3 -c $(printf '%q' "$REMOTE_SCRIPT") $(printf '%q' "$STALWART_USER") $(printf '%q' "$password") $(printf '%q' "$MODE") $(printf '%q' "$EMAIL_ID") $(printf '%q' "$LIMIT")"
