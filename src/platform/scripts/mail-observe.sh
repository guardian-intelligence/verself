#!/usr/bin/env bash
set -euo pipefail

script_dir="$(CDPATH= cd -- "$(dirname "$0")" && pwd)"

mode="events"
limit=20
mailbox=""
direction=""

usage() {
  cat <<'EOF'
Usage:
  ./scripts/mail-observe.sh
  ./scripts/mail-observe.sh --metrics
  ./scripts/mail-observe.sh --mailbox ceo --direction inbound -n 10
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --metrics)
      mode="metrics"
      shift
      ;;
    --mailbox)
      mailbox="$2"
      shift 2
      ;;
    --direction)
      direction="$2"
      shift 2
      ;;
    -n|--limit)
      limit="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown option: $1" >&2
      usage >&2
      exit 1
      ;;
  esac
done

if ! [[ "$limit" =~ ^[0-9]+$ ]]; then
  echo "ERROR: --limit must be an integer" >&2
  exit 1
fi

if [[ "$mode" == "metrics" ]]; then
  query="
    SELECT
      MetricGroup,
      MetricName,
      CurrentValue,
      SampledAt
    FROM default.mail_metrics_latest
    ORDER BY MetricGroup, MetricName
    FORMAT Vertical
  "
else
  filters=()
  if [[ -n "$mailbox" ]]; then
    mailbox_escaped="${mailbox//\'/\'\'}"
    filters+=("MailboxAccount = '${mailbox_escaped}'")
  fi
  if [[ -n "$direction" ]]; then
    direction_escaped="${direction//\'/\'\'}"
    filters+=("Direction = '${direction_escaped}'")
  fi

  where_clause="1"
  if [[ ${#filters[@]} -gt 0 ]]; then
    where_clause="$(IFS=' AND '; printf '%s' "${filters[*]}")"
  fi

  query="
    SELECT
      Timestamp,
      Direction,
      EventType,
      nullIf(MailboxAccount, '') AS MailboxAccount,
      nullIf(Sender, '') AS Sender,
      nullIf(Subject, '') AS Subject,
      nullIf(EmailID, '') AS EmailID,
      nullIf(QueueID, '') AS QueueID,
      nullIf(ExternalID, '') AS ExternalID,
      UpsertedEmails,
      DestroyedEmails,
      UpsertedThreads,
      RecipientCount,
      Message
    FROM default.mail_events
    WHERE ${where_clause}
    ORDER BY Timestamp DESC
    LIMIT ${limit}
    FORMAT Vertical
  "
fi

exec "${script_dir}/clickhouse.sh" --query "$query"
