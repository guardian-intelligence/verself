#!/usr/bin/env bash
set -euo pipefail

# build-substrate.sh — build the slim Firecracker guest substrate.
#
# The substrate is the boot image every lease clones from. It carries
# the kernel boot path (vm-bridge as /sbin/init), guest telemetry, and
# a minimal Ubuntu userspace — and nothing else. Workload toolchains
# (GitHub Actions runner, Forgejo runner, language toolchains, customer
# images) live in their own ext4 artefacts and are mounted into the
# guest read-only at lease boot via firecracker.images +
# runner_class_filesystem_mounts.
#
# Inputs (staged alongside this script by the guest_rootfs Ansible role):
#   ubuntu-base.tar.gz       Pinned Ubuntu base rootfs tarball (Bazel-vendored)
#   vmlinux                  Pinned guest kernel (Bazel-vendored)
#   vmlinux.config           Kernel config for VIRTIO_VSOCKETS / HW_RANDOM_VIRTIO assertions
#   vm-bridge                PID 1 inside the guest (Bazel-built)
#   vm-guest-telemetry       Telemetry agent (Bazel-built)
#   versions.json            Pinned versions metadata
#
# Outputs (in OUTPUT_DIR, default ./guest/output):
#   substrate.ext4           The slim boot image
#   vmlinux                  Pass-through for vm-orchestrator's --kernel-path
#   vmlinux.config           Pass-through for kernel-config audit
#   sbom.txt                 dpkg-query of every package in the substrate
#   manifest.json            sha256 + bytes for substrate.ext4 + kernel
#
# Requires: root, jq, mkfs.ext4 (e2fsprogs), tar, mount, sha256sum.
# Does NOT require internet — every byte is staged ahead of time.

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

if [[ -n "${SUBSTRATE_INPUTS:-}" ]]; then
  INPUTS="$SUBSTRATE_INPUTS"
else
  INPUTS="$SCRIPT_DIR"
fi
OUTPUT_DIR="${SUBSTRATE_OUTPUT_DIR:-$SCRIPT_DIR/guest/output}"

if [[ $EUID -ne 0 ]]; then
  echo "ERROR: must run as root" >&2
  exit 1
fi

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "ERROR: $1 not in PATH" >&2
    exit 1
  }
}

require_cmd jq
require_cmd mkfs.ext4
require_cmd mount
require_cmd sha256sum
require_cmd tar

UBUNTU_BASE_TARBALL="$INPUTS/ubuntu-base.tar.gz"
VMLINUX_SRC="$INPUTS/vmlinux"
VMLINUX_CONFIG_SRC="$INPUTS/vmlinux.config"
VM_BRIDGE_SRC="$INPUTS/vm-bridge"
VM_GUEST_TELEMETRY_SRC="$INPUTS/vm-guest-telemetry"
VERSIONS_JSON="$INPUTS/versions.json"

for path in "$UBUNTU_BASE_TARBALL" "$VMLINUX_SRC" "$VMLINUX_CONFIG_SRC" "$VM_BRIDGE_SRC" "$VM_GUEST_TELEMETRY_SRC" "$VERSIONS_JSON"; do
  [[ -f "$path" ]] || {
    echo "ERROR: missing input $path" >&2
    exit 1
  }
done

UBUNTU_BASE_VERSION=$(jq -er '.ubuntu_base.version' "$VERSIONS_JSON")
GUEST_KERNEL_VERSION=$(jq -er '.guest_kernel.version' "$VERSIONS_JSON")

# Validate the Bazel-vendored kernel still has the features
# vm-orchestrator + vm-bridge depend on. Catch upstream drift loudly
# rather than discovering at first guest boot that vsock disappeared.
grep -q '^CONFIG_VIRTIO_VSOCKETS=y$' "$VMLINUX_CONFIG_SRC" || {
  echo "ERROR: guest kernel is missing CONFIG_VIRTIO_VSOCKETS=y" >&2
  exit 1
}
grep -q '^CONFIG_HW_RANDOM_VIRTIO=y$' "$VMLINUX_CONFIG_SRC" || {
  echo "ERROR: guest kernel is missing CONFIG_HW_RANDOM_VIRTIO=y" >&2
  exit 1
}

WORKDIR=$(mktemp -d)
ROOTFS="$WORKDIR/rootfs"
MOUNT_POINTS=()

unmount_chroot_mounts() {
  local idx mount_point
  for ((idx=${#MOUNT_POINTS[@]}-1; idx>=0; idx--)); do
    mount_point="${MOUNT_POINTS[$idx]}"
    if mountpoint -q "$mount_point"; then
      umount -l "$mount_point"
    fi
  done
  MOUNT_POINTS=()
}

cleanup() {
  unmount_chroot_mounts || true
  rm -rf "$WORKDIR"
}
trap cleanup EXIT

mount_chroot_filesystems() {
  mkdir -p "$ROOTFS/proc" "$ROOTFS/sys" "$ROOTFS/dev" "$ROOTFS/run"
  mount -t proc proc "$ROOTFS/proc"
  MOUNT_POINTS+=("$ROOTFS/proc")
  mount --rbind /sys "$ROOTFS/sys"
  mount --make-rslave "$ROOTFS/sys"
  MOUNT_POINTS+=("$ROOTFS/sys")
  mount --rbind /dev "$ROOTFS/dev"
  mount --make-rslave "$ROOTFS/dev"
  MOUNT_POINTS+=("$ROOTFS/dev")
  mount --bind /run "$ROOTFS/run"
  mount --make-rslave "$ROOTFS/run"
  MOUNT_POINTS+=("$ROOTFS/run")
}

run_chroot() {
  chroot "$ROOTFS" /usr/bin/env DEBIAN_FRONTEND=noninteractive "$@"
}

mkdir -p "$ROOTFS" "$OUTPUT_DIR"

echo "-> extracting Ubuntu base rootfs ($UBUNTU_BASE_VERSION)"
tar -xzf "$UBUNTU_BASE_TARBALL" --numeric-owner -C "$ROOTFS"

echo "-> staging kernel artefacts"
install -m 0644 "$VMLINUX_SRC" "$OUTPUT_DIR/vmlinux"
install -m 0644 "$VMLINUX_CONFIG_SRC" "$OUTPUT_DIR/vmlinux.config"

echo "-> preparing chroot"
mkdir -p "$ROOTFS/usr/local/bin" "$ROOTFS/usr/sbin"
rm -f "$ROOTFS/etc/resolv.conf"
install -m 0644 /etc/resolv.conf "$ROOTFS/etc/resolv.conf"
cat > "$ROOTFS/etc/hosts" <<'HOSTS'
127.0.0.1 localhost
::1 localhost ip6-localhost ip6-loopback
HOSTS
cat > "$ROOTFS/etc/nsswitch.conf" <<'NSSWITCH'
passwd:         files systemd
group:          files systemd
shadow:         files systemd
gshadow:        files systemd

hosts:          files dns
networks:       files

protocols:      db files
services:       db files
ethers:         db files
rpc:            db files
NSSWITCH
cat > "$ROOTFS/usr/sbin/policy-rc.d" <<'POLICY'
#!/bin/sh
exit 101
POLICY
chmod 0755 "$ROOTFS/usr/sbin/policy-rc.d"
mount_chroot_filesystems

# Substrate package set: enough Ubuntu userspace to boot, run vm-bridge,
# resolve DNS, accept SSH connections, and install/exec workload binaries
# from toolchain-image mounts. Anything language-specific (go, node,
# python beyond stock) must NOT live here — it goes in a workload
# toolchain image. The libicu74/libssl3/libkrb5-3/zlib1g entries cover
# the runtime deps every workload runner we ship today (GHA, Forgejo)
# expects from its installdependencies.sh equivalent.
echo "-> installing minimal Ubuntu userspace"
run_chroot apt-get update
run_chroot apt-get install -y --no-install-recommends \
  bash \
  ca-certificates \
  curl \
  git \
  iproute2 \
  jq \
  libicu74 \
  libkrb5-3 \
  libssl3 \
  openssh-client \
  python3 \
  sudo \
  tar \
  unzip \
  xz-utils \
  zlib1g \
  zstd

echo "-> installing vm-bridge as PID 1"
rm -f "$ROOTFS/sbin/init"
install -D -m 0755 "$VM_BRIDGE_SRC" "$ROOTFS/sbin/init"
install -D -m 0755 "$VM_BRIDGE_SRC" "$ROOTFS/usr/local/bin/vm-bridge"

echo "-> installing vm-guest-telemetry"
install -D -m 0755 "$VM_GUEST_TELEMETRY_SRC" "$ROOTFS/usr/local/bin/vm-guest-telemetry"

# Workspace + /home are write targets inside the lease clone; vm-bridge
# additionally mounts /tmp, /run, /dev/shm as tmpfs at boot. /workspace
# is part of the rwroot clone so writes survive within the lease but
# disappear when the lease is destroyed.
#
# Toolchain mount points (e.g. /opt/actions-runner, /opt/forgejo-runner,
# /opt/hostedtoolcache) are deliberately NOT pre-created here.
# vm-bridge.mountFilesystems creates each mount point on demand from the
# LeaseSpec.FilesystemMounts list, and baking workload-specific paths
# into the substrate would re-couple the platform binary to the GitHub
# Actions / Forgejo conventions we just split out.
echo "-> creating substrate write targets"
mkdir -p "$ROOTFS/workspace" "$ROOTFS/home"
chmod 1777 "$ROOTFS/workspace"

cat > "$ROOTFS/etc/profile.d/verself-base.sh" <<'PROFILE'
export PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
PROFILE
chmod 0644 "$ROOTFS/etc/profile.d/verself-base.sh"

run_chroot git config --system --add safe.directory '*'
run_chroot apt-get clean
rm -rf "$ROOTFS/var/lib/apt/lists/"*
rm -f "$ROOTFS/usr/sbin/policy-rc.d"

echo "-> generating SBOM"
DPKG_INSTALLED_TMP="$WORKDIR/dpkg-installed.txt"
# shellcheck disable=SC2016
chroot "$ROOTFS" dpkg-query -W -f='${binary:Package}\t${Version}\n' > "$DPKG_INSTALLED_TMP"
PACKAGE_COUNT=$(wc -l < "$DPKG_INSTALLED_TMP" | tr -d '[:space:]')
cp "$DPKG_INSTALLED_TMP" "$OUTPUT_DIR/sbom.txt"

INIT_SHA256=$(sha256sum "$ROOTFS/sbin/init" | awk '{print $1}')
INIT_BYTES=$(stat -c '%s' "$ROOTFS/sbin/init")
VM_GUEST_TELEMETRY_SHA256=$(sha256sum "$ROOTFS/usr/local/bin/vm-guest-telemetry" | awk '{print $1}')
VM_GUEST_TELEMETRY_BYTES=$(stat -c '%s' "$ROOTFS/usr/local/bin/vm-guest-telemetry")
{
  echo
  echo "# custom_components"
  echo "file path=/sbin/init component=vm-bridge sha256=$INIT_SHA256 bytes=$INIT_BYTES"
  echo "file path=/usr/local/bin/vm-bridge component=vm-bridge sha256=$INIT_SHA256 bytes=$INIT_BYTES"
  echo "file path=/usr/local/bin/vm-guest-telemetry component=vm-guest-telemetry sha256=$VM_GUEST_TELEMETRY_SHA256 bytes=$VM_GUEST_TELEMETRY_BYTES"
} >> "$OUTPUT_DIR/sbom.txt"

unmount_chroot_mounts

# Sized at 2 GiB; the substrate fully populated lands around 400-500 MiB
# today, leaving headroom for adding apt packages without resizing
# every clone. The zvol's volsize at clone time is independent of this
# (the clone inherits the source-zvol's volsize, which the seed RPC
# sets from firecracker.images[].size_bytes in the catalog).
SUBSTRATE_SIZE_MIB=2048
echo "-> building ext4 image (${SUBSTRATE_SIZE_MIB} MiB)"
truncate -s "${SUBSTRATE_SIZE_MIB}M" "$OUTPUT_DIR/substrate.ext4"
mkfs.ext4 -F -L substrate -d "$ROOTFS" "$OUTPUT_DIR/substrate.ext4" >/dev/null

ROOTFS_SHA256=$(sha256sum "$OUTPUT_DIR/substrate.ext4" | awk '{print $1}')
ROOTFS_APPARENT_BYTES=$(stat -c '%s' "$OUTPUT_DIR/substrate.ext4")
# du -s for the summary (one line, total bytes); without -s du recurses
# and the resulting multi-line stream breaks downstream jq parsing.
ROOTFS_USED_BYTES=$(du -sb "$ROOTFS" | awk '{print $1}')
KERNEL_BYTES=$(stat -c '%s' "$OUTPUT_DIR/vmlinux")
KERNEL_SHA256=$(sha256sum "$OUTPUT_DIR/vmlinux" | awk '{print $1}')
SBOM_BYTES=$(stat -c '%s' "$OUTPUT_DIR/sbom.txt")
SBOM_SHA256=$(sha256sum "$OUTPUT_DIR/sbom.txt" | awk '{print $1}')

jq -n \
  --arg built_at_utc "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  --arg ubuntu_base_version "$UBUNTU_BASE_VERSION" \
  --arg guest_kernel_version "$GUEST_KERNEL_VERSION" \
  --arg substrate_size_mib "$SUBSTRATE_SIZE_MIB" \
  --arg substrate_apparent_bytes "$ROOTFS_APPARENT_BYTES" \
  --arg substrate_used_bytes "$ROOTFS_USED_BYTES" \
  --arg substrate_sha256 "$ROOTFS_SHA256" \
  --arg kernel_sha256 "$KERNEL_SHA256" \
  --arg kernel_bytes "$KERNEL_BYTES" \
  --arg sbom_sha256 "$SBOM_SHA256" \
  --arg sbom_bytes "$SBOM_BYTES" \
  --arg package_count "$PACKAGE_COUNT" \
  --arg init_sha256 "$INIT_SHA256" \
  --arg init_bytes "$INIT_BYTES" \
  --arg vm_guest_telemetry_sha256 "$VM_GUEST_TELEMETRY_SHA256" \
  --arg vm_guest_telemetry_bytes "$VM_GUEST_TELEMETRY_BYTES" \
  '{
    schema_version: 3,
    built_at_utc: $built_at_utc,
    ubuntu_base_version: $ubuntu_base_version,
    guest_kernel_version: $guest_kernel_version,
    substrate_size_mib: ($substrate_size_mib | tonumber),
    substrate_apparent_bytes: ($substrate_apparent_bytes | tonumber),
    substrate_used_bytes: ($substrate_used_bytes | tonumber),
    substrate_sha256: $substrate_sha256,
    kernel_sha256: $kernel_sha256,
    kernel_bytes: ($kernel_bytes | tonumber),
    sbom_sha256: $sbom_sha256,
    sbom_bytes: ($sbom_bytes | tonumber),
    package_count: ($package_count | tonumber),
    init_sha256: $init_sha256,
    init_bytes: ($init_bytes | tonumber),
    vm_guest_telemetry_sha256: $vm_guest_telemetry_sha256,
    vm_guest_telemetry_bytes: ($vm_guest_telemetry_bytes | tonumber)
  }' > "$OUTPUT_DIR/manifest.json"

echo "OK substrate built: $OUTPUT_DIR/substrate.ext4"
echo "OK SBOM written:    $OUTPUT_DIR/sbom.txt"
echo "OK manifest:        $OUTPUT_DIR/manifest.json"
