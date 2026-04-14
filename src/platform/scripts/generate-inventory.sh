#!/usr/bin/env bash
# Generate Ansible inventory from OpenTofu outputs.
# Usage: ./scripts/generate-inventory.sh [terraform_dir]
set -euo pipefail

TF_DIR="${1:-terraform}"
INVENTORY="ansible/inventory/hosts.ini"

# Read outputs as JSON
worker_ips=$(cd "$TF_DIR" && tofu output -json worker_ips 2>/dev/null | jq -r '.[]' 2>/dev/null) || true
infra_ips=$(cd "$TF_DIR" && tofu output -json infra_ips 2>/dev/null | jq -r '.[]' 2>/dev/null) || true

if [ -z "$worker_ips" ] && [ -z "$infra_ips" ]; then
  echo "Error: No IPs found in tofu outputs. Is infrastructure provisioned?" >&2
  exit 1
fi

# Read cluster_name from tfvars for hostname prefix
if [ -f "$TF_DIR/terraform.tfvars.json" ]; then
  cluster=$(jq -r '.cluster_name // "dev"' "$TF_DIR/terraform.tfvars.json")
else
  cluster="dev"
fi

mkdir -p "$(dirname "$INVENTORY")"

{
  echo "[workers]"
  i=0
  for ip in $worker_ips; do
    echo "fm-${cluster}-w${i} ansible_host=${ip}"
    i=$((i + 1))
  done

  echo ""
  echo "[infra]"
  if [ -n "$infra_ips" ]; then
    i=0
    for ip in $infra_ips; do
      echo "fm-${cluster}-i${i} ansible_host=${ip}"
      i=$((i + 1))
    done
  else
    # Single-node dev: workers double as infra
    i=0
    for ip in $worker_ips; do
      echo "fm-${cluster}-w${i} ansible_host=${ip}"
      i=$((i + 1))
    done
  fi

  echo ""
  echo "[all:vars]"
  echo "ansible_user=ubuntu"
  echo "ansible_python_interpreter=/usr/bin/python3"
} > "$INVENTORY"

echo "Inventory written to $INVENTORY:"
cat "$INVENTORY"
