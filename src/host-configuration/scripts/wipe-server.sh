#!/usr/bin/env bash
# Wipe all verself state from a provisioned server.
# Keeps the OS, apt packages, and SSH config intact.
# Usage: ./scripts/wipe-server.sh [host]
set -euo pipefail

HOST="${1:-$(grep -m1 'ansible_host=' "ansible/inventory/${VERSELF_SITE:-prod}.ini" | sed 's/.*ansible_host=\([^ ]*\).*/\1/')}"
USER="${2:-ubuntu}"

echo "Wiping verself state on ${USER}@${HOST}..."

ssh -o StrictHostKeyChecking=no "${USER}@${HOST}" 'sudo bash -s' <<'EOF'
set -euo pipefail

echo "=== Stopping all verself services ==="
systemctl stop haproxy haproxy-lego-renew.timer forgejo grafana \
  zitadel clickhouse-server postgresql tigerbeetle otelcol verdaccio \
  containerd nftables 2>/dev/null || true

echo "=== Disabling all verself services ==="
systemctl disable haproxy haproxy-lego-renew.timer forgejo grafana \
  zitadel clickhouse-server postgresql tigerbeetle otelcol verdaccio \
  containerd nftables 2>/dev/null || true

echo "=== Removing systemd units ==="
rm -f /etc/systemd/system/haproxy.service \
      /etc/systemd/system/haproxy-lego-renew.service \
      /etc/systemd/system/haproxy-lego-renew.timer \
      /etc/systemd/system/forgejo.service \
      /etc/systemd/system/grafana.service \
      /etc/systemd/system/zitadel.service \
      /etc/systemd/system/clickhouse-server.service \
      /etc/systemd/system/postgresql.service \
      /etc/systemd/system/tigerbeetle.service \
      /etc/systemd/system/otelcol.service \
      /etc/systemd/system/verdaccio.service \
      /etc/systemd/system/containerd.service
systemctl daemon-reload

echo "=== Removing config directories ==="
for d in /etc/forgejo /etc/zitadel /etc/grafana /etc/clickhouse-server /etc/haproxy \
         /etc/otelcol /etc/credstore /etc/verself /etc/verdaccio \
         /etc/containerd /etc/nftables.d /etc/wireguard; do
  [ -d "$d" ] && rm -r "$d"
done

echo "=== Removing data directories ==="
for d in /var/lib/tigerbeetle /var/lib/forgejo /var/lib/grafana /var/lib/clickhouse \
         /var/lib/verdaccio /var/lib/verself/guest-images /var/lib/haproxy /var/lib/lego \
         /var/log/clickhouse-server /opt/verself /opt/verdaccio \
         /var/log/grafana /var/lib/postgresql; do
  [ -d "$d" ] && rm -r "$d"
done

echo "=== Destroying ZFS pool ==="
zpool destroy vspool 2>/dev/null || true

echo "=== Removing system users/groups ==="
for u in forgejo zitadel grafana clickhouse tigerbeetle verdaccio haproxy; do
  userdel -r "$u" 2>/dev/null || true
  groupdel "$u" 2>/dev/null || true
done

echo "=== Removing sudoers and npm config ==="
rm -f /etc/npmrc

echo "=== Removing SSH hardening (will be re-applied) ==="
rm -f /etc/ssh/sshd_config.d/99-verself.conf

echo "=== Done — server is wiped ==="
EOF

echo "Server wiped. Next steps:"
echo "  aspect substrate guest-rootfs"
echo "  aspect deploy"
