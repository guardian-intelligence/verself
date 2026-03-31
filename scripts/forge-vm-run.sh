#!/usr/bin/env bash
# forge-vm-run.sh — Run a command inside a Firecracker microVM with a ZFS zvol clone.
# Usage: sudo forge-vm-run.sh <work-dir> <command>
# Requires root. Handles cleanup on exit/signal.
set -euo pipefail

WORK_DIR="${1:?Usage: forge-vm-run.sh <work-dir> <command>}"
CMD="${2:?Usage: forge-vm-run.sh <work-dir> <command>}"

# --- Constants ---
POOL="benchpool"
# HACK: golden-zvol2 because golden-zvol is stuck in "dataset is busy" state.
# TODO: destroy golden-zvol (may need server reboot to release), rename to golden-zvol.
GOLDEN_SNAP="${POOL}/golden-zvol2@ready"
CI_PREFIX="${POOL}/ci"
KERNEL="/var/lib/ci/vmlinux"
# LEARNING: Nix-packaged firecracker at /opt/forge-metal/profile/bin/ is dynamically
# linked against /nix/store/ paths — unusable inside the jailer's chroot.
# Static binaries at /usr/local/bin/ work. These were manually deployed.
FC_BIN="/usr/local/bin/firecracker"
JAILER_BIN="/usr/local/bin/jailer"
JAILER_ROOT="/srv/jailer"
JAILER_UID=10000
JAILER_GID=10000
VCPUS=2
MEM_MIB=1024
GUEST_IP="172.16.0.2"
HOST_IP="172.16.0.1/30"
GUEST_CIDR="172.16.0.0/30"
GUEST_MASK="255.255.255.252"
GUEST_GW="172.16.0.1"
GUEST_MAC="06:00:AC:10:00:02"

# --- Generate job ID ---
JOB_ID="$(cat /proc/sys/kernel/random/uuid)"
DATASET="${CI_PREFIX}/${JOB_ID}"
DEV_PATH="/dev/zvol/${DATASET}"
JAIL="${JAILER_ROOT}/firecracker/${JOB_ID}/root"
SOCK="${JAIL}/run/firecracker.sock"
TAP="tap-${JOB_ID:0:11}"
SERIAL_LOG="$(mktemp /tmp/fc-serial-XXXXXX.log)"
MOUNT_DIR=""
JAILER_PID=""
HOST_IFACE=""

echo "[forge-vm] job=${JOB_ID}"

# --- Cleanup trap ---
cleanup() {
    set +e
    [ -n "$JAILER_PID" ] && kill "$JAILER_PID" 2>/dev/null && wait "$JAILER_PID" 2>/dev/null
    [ -n "$HOST_IFACE" ] && iptables -t nat -D POSTROUTING -o "$HOST_IFACE" -s "$GUEST_CIDR" -j MASQUERADE 2>/dev/null
    ip link del "$TAP" 2>/dev/null
    [ -n "$MOUNT_DIR" ] && umount "$MOUNT_DIR" 2>/dev/null && rmdir "$MOUNT_DIR" 2>/dev/null
    zfs destroy "$DATASET" 2>/dev/null
    rm -rf "${JAILER_ROOT}/firecracker/${JOB_ID}" 2>/dev/null
    rm -f "$SERIAL_LOG" 2>/dev/null
}
trap cleanup EXIT INT TERM

# --- 1. Clone golden zvol ---
zfs clone \
    -o "forge:job_id=${JOB_ID}" \
    -o "forge:created_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    "$GOLDEN_SNAP" "$DATASET"
echo "[forge-vm] zvol cloned"

# --- 2. Wait for device node ---
for _ in $(seq 1 60); do
    [ -e "$DEV_PATH" ] && break
    sleep 0.05
done
[ -e "$DEV_PATH" ] || { echo "FATAL: $DEV_PATH did not appear" >&2; exit 1; }

# --- 3. Mount, write job config, unmount ---
MOUNT_DIR="$(mktemp -d /tmp/fc-mount-XXXXXX)"
mount "$DEV_PATH" "$MOUNT_DIR"
mkdir -p "${MOUNT_DIR}/etc/ci"
cat > "${MOUNT_DIR}/etc/ci/job.json" <<JOBEOF
{"command":["bash","-c",$(printf '%s' "$CMD" | python3 -c 'import sys,json; print(json.dumps(sys.stdin.read()))')],"env":{"CI":"true"},"work_dir":"${WORK_DIR}"}
JOBEOF
umount "$MOUNT_DIR"
rmdir "$MOUNT_DIR"
MOUNT_DIR=""
echo "[forge-vm] job config written"

# --- 4. Set up jail ---
mkdir -p "$JAIL" "${JAIL}/run"
ln "$KERNEL" "${JAIL}/vmlinux" 2>/dev/null || cp "$KERNEL" "${JAIL}/vmlinux"

# Device major/minor (stat outputs hex)
read -r HEX_MAJ HEX_MIN < <(stat -L -c '%t %T' "$DEV_PATH")
MAJOR=$((16#$HEX_MAJ))
MINOR=$((16#$HEX_MIN))
mknod "${JAIL}/rootfs" b "$MAJOR" "$MINOR"

touch "${JAIL}/metrics.json"
chown "$JAILER_UID:$JAILER_GID" "${JAIL}/vmlinux" "${JAIL}/rootfs" "${JAIL}/metrics.json"
echo "[forge-vm] jail ready"

# --- 5. Set up network ---
HOST_IFACE="$(ip route show default | awk '/dev/{print $5}' | head -1)"
: "${HOST_IFACE:=eth0}"

ip tuntap add "$TAP" mode tap
ip addr add "$HOST_IP" dev "$TAP"
ip link set "$TAP" up
sysctl -qw net.ipv4.ip_forward=1
iptables -t nat -A POSTROUTING -o "$HOST_IFACE" -s "$GUEST_CIDR" -j MASQUERADE
echo "[forge-vm] network ready tap=${TAP} iface=${HOST_IFACE}"

# --- 6. Start jailer ---
"$JAILER_BIN" \
    --id "$JOB_ID" \
    --exec-file "$FC_BIN" \
    --uid "$JAILER_UID" --gid "$JAILER_GID" \
    --chroot-base-dir "$JAILER_ROOT" \
    -- --api-sock /run/firecracker.sock \
    > "$SERIAL_LOG" 2>&1 &
JAILER_PID=$!
echo "[forge-vm] jailer pid=${JAILER_PID}"

# --- 7. Wait for API socket ---
for _ in $(seq 1 100); do
    [ -S "$SOCK" ] && break
    sleep 0.05
done
[ -S "$SOCK" ] || { echo "FATAL: API socket did not appear" >&2; exit 1; }
# Wait until socket is connectable (not just exists)
for _ in $(seq 1 50); do
    if curl -s --unix-socket "$SOCK" http://localhost/ >/dev/null 2>&1; then
        break
    fi
    sleep 0.05
done

# --- 8. Configure VM via API (6 PUT calls) ---
api_put() {
    local path="$1" body="$2"
    local resp
    resp=$(curl -s -w '\n%{http_code}' --unix-socket "$SOCK" \
        -X PUT "http://localhost/${path}" \
        -H 'Content-Type: application/json' \
        -d "$body" 2>&1)
    local code
    code=$(echo "$resp" | tail -1)
    if [ "$code" -ge 300 ] 2>/dev/null; then
        echo "FATAL: PUT /${path} returned HTTP ${code}: $(echo "$resp" | head -n -1)" >&2
        exit 1
    fi
}

BOOT_ARGS="root=/dev/vda rw console=ttyS0 reboot=k panic=1 ip=${GUEST_IP}::${GUEST_GW}:${GUEST_MASK}::eth0:off init=/sbin/init"

api_put "metrics"             '{"metrics_path":"/metrics.json"}'
api_put "boot-source"         "{\"kernel_image_path\":\"/vmlinux\",\"boot_args\":\"${BOOT_ARGS}\"}"
api_put "drives/rootfs"       '{"drive_id":"rootfs","path_on_host":"/rootfs","is_root_device":true,"is_read_only":false}'
api_put "machine-config"      "{\"vcpu_count\":${VCPUS},\"mem_size_mib\":${MEM_MIB},\"smt\":false}"
api_put "network-interfaces/eth0" "{\"iface_id\":\"eth0\",\"host_dev_name\":\"${TAP}\",\"guest_mac\":\"${GUEST_MAC}\"}"
api_put "actions"             '{"action_type":"InstanceStart"}'
echo "[forge-vm] VM started"

# --- 9. Wait for VM exit ---
wait "$JAILER_PID" 2>/dev/null || true
JAILER_PID=""

# --- 10. Parse guest exit code ---
GUEST_EXIT=$(grep -oP 'FORGEVM_EXIT_CODE=\K[0-9]+' "$SERIAL_LOG" 2>/dev/null || echo "1")

# --- 11. ZFS written bytes ---
ZFS_WRITTEN=$(zfs get -H -p -o value written "$DATASET" 2>/dev/null || echo "0")

echo "[forge-vm] exit_code=${GUEST_EXIT} zfs_written=${ZFS_WRITTEN}"

# Print serial output for CI visibility
echo "=== Serial Console Output ==="
cat "$SERIAL_LOG"
echo "=== End Serial Output ==="

# Cleanup happens via trap
exit "${GUEST_EXIT}"
