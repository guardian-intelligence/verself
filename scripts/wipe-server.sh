#!/usr/bin/env bash
# Wipe all forge-metal state from a provisioned server.
# Keeps the OS, Nix store, and SSH config intact.
# Usage: ./scripts/wipe-server.sh [host]
set -euo pipefail

HOST="${1:-$(grep -m1 'ansible_host=' ansible/inventory/hosts.ini | sed 's/.*ansible_host=\([^ ]*\).*/\1/')}"
USER="${2:-ubuntu}"

echo "Wiping forge-metal state on ${USER}@${HOST}..."

ssh -o StrictHostKeyChecking=no "${USER}@${HOST}" 'sudo bash -s' <<'EOF'
set -euo pipefail

echo "=== Stopping all forge-metal services ==="
systemctl stop caddy forgejo forgejo-runner hyperdx-api hyperdx-app \
  zitadel clickhouse-server postgresql tigerbeetle otelcol verdaccio \
  mongod containerd nftables 2>/dev/null || true

echo "=== Disabling all forge-metal services ==="
systemctl disable caddy forgejo forgejo-runner hyperdx-api hyperdx-app \
  zitadel clickhouse-server postgresql tigerbeetle otelcol verdaccio \
  mongod containerd nftables 2>/dev/null || true

echo "=== Removing systemd units ==="
rm -f /etc/systemd/system/caddy.service \
      /etc/systemd/system/forgejo.service \
      /etc/systemd/system/forgejo-runner.service \
      /etc/systemd/system/hyperdx-api.service \
      /etc/systemd/system/hyperdx-app.service \
      /etc/systemd/system/zitadel.service \
      /etc/systemd/system/clickhouse-server.service \
      /etc/systemd/system/postgresql.service \
      /etc/systemd/system/tigerbeetle.service \
      /etc/systemd/system/otelcol.service \
      /etc/systemd/system/verdaccio.service \
      /etc/systemd/system/mongod.service \
      /etc/systemd/system/containerd.service
systemctl daemon-reload

echo "=== Removing config directories ==="
for d in /etc/forgejo /etc/zitadel /etc/clickhouse-server /etc/caddy \
         /etc/otelcol /etc/credstore /etc/forge-metal /etc/verdaccio \
         /etc/containerd /etc/nftables.d /etc/wireguard; do
  [ -d "$d" ] && rm -r "$d"
done

echo "=== Removing data directories ==="
for d in /var/lib/tigerbeetle /var/lib/forgejo /var/lib/clickhouse \
         /var/lib/verdaccio /var/lib/forge-runner /var/lib/ci \
         /var/log/clickhouse-server /opt/forge-metal /opt/verdaccio \
         /var/lib/postgresql /var/lib/mongodb; do
  [ -d "$d" ] && rm -r "$d"
done

echo "=== Destroying ZFS pool ==="
zpool destroy forgepool 2>/dev/null || true

echo "=== Removing system users/groups ==="
for u in forgejo zitadel clickhouse tigerbeetle forge-runner verdaccio caddy; do
  userdel -r "$u" 2>/dev/null || true
  groupdel "$u" 2>/dev/null || true
done

echo "=== Removing sudoers and npm config ==="
rm -f /etc/sudoers.d/forge-runner /etc/npmrc

echo "=== Removing SSH hardening (will be re-applied) ==="
rm -f /etc/ssh/sshd_config.d/99-forge-metal.conf

echo "=== Done — server is wiped ==="
EOF

echo "Server wiped. Next steps:"
echo "  make guest-rootfs && make deploy-ci-artifacts && make deploy"
