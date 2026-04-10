#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

mode="${SANDBOX_INNER_MODE:-dev}"

case "${mode}" in
  dev)
    "${script_dir}/run-rent-local-dev.sh"
    ;;
  verify)
    "${script_dir}/verify-rent-ui-local.sh"
    ;;
  *)
    echo "usage: SANDBOX_INNER_MODE=dev|verify $0" >&2
    exit 1
    ;;
esac
