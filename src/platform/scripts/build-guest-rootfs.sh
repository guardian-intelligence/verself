#!/usr/bin/env bash
set -euo pipefail

# build-guest-rootfs.sh — Build an Ubuntu 24.04 ext4 rootfs for Firecracker VMs.
#
# The image is the canonical runner substrate for the first public CI label:
# metal-4vcpu-ubuntu-2404. It contains the vm-bridge PID 1, guest telemetry,
# the official GitHub Actions runner, Go, Node.js, git, and the
# basic build tools expected by the forge-metal dogfood workflow.
#
# Two-layer architecture:
#   Layer 1 (this script): base OS + runner toolchain -> rootfs.ext4
#   Layer 2 (vm-orchestrator): ZFS clone per direct execution
#
# Requires: root, internet access, jq, curl, tar, mount, e2fsprogs. go only if no
# pre-built vm-bridge exists.
# Produces: guest/output/rootfs.ext4, guest/output/sbom.txt, guest/output/guest-artifacts.json

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

# Support two layouts:
# 1. Running from project root: scripts/build-guest-rootfs.sh -> guest/versions.json
# 2. Flat scp to /tmp: /tmp/build-guest-rootfs.sh + /tmp/versions.json
if [[ -f "$SCRIPT_DIR/../guest/versions.json" ]]; then
  PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
  VERSIONS="$PROJECT_ROOT/guest/versions.json"
  OUTPUT_DIR="$PROJECT_ROOT/guest/output"
elif [[ -f "$SCRIPT_DIR/versions.json" ]]; then
  PROJECT_ROOT="$SCRIPT_DIR"
  VERSIONS="$SCRIPT_DIR/versions.json"
  OUTPUT_DIR="$SCRIPT_DIR/guest/output"
else
  echo "ERROR: cannot find versions.json (looked in $SCRIPT_DIR/../guest/ and $SCRIPT_DIR/)" >&2
  exit 1
fi

if [[ $EUID -ne 0 ]]; then
  echo "ERROR: must run as root" >&2
  exit 1
fi

require_cmd() {
  local name="$1"
  command -v "$name" >/dev/null 2>&1 || {
    echo "ERROR: $name not in PATH" >&2
    exit 1
  }
}

require_cmd curl
require_cmd dumpe2fs
require_cmd jq
require_cmd mke2fs
require_cmd mount
require_cmd sha256sum
require_cmd tar

# go is only required if no pre-built vm-bridge exists.
if [[ ! -f "$SCRIPT_DIR/vm-bridge" ]] && ! command -v go >/dev/null 2>&1; then
  echo "ERROR: no pre-built vm-bridge and go not in PATH" >&2
  exit 1
fi

json_string() {
  jq -er "$1" "$VERSIONS"
}

download_checked() {
  local url="$1"
  local sha256="$2"
  local dest="$3"
  curl -fsSL -o "$dest" "$url"
  echo "$sha256  $dest" | sha256sum -c -
}

# Read version pins.
UBUNTU_BASE_URL=$(json_string '.ubuntu_base.url')
UBUNTU_BASE_SHA256=$(json_string '.ubuntu_base.sha256')
UBUNTU_BASE_VERSION=$(json_string '.ubuntu_base.version')
UBUNTU_BASE_ARCH=$(json_string '.ubuntu_base.arch')
ROOTFS_IMAGE_SIZE=$(json_string '.rootfs.size')

FIRECRACKER_VERSION=$(json_string '.firecracker.version')
GUEST_KERNEL_VERSION=$(json_string '.guest_kernel.version')
GUEST_KERNEL_ARCH=$(json_string '.guest_kernel.arch')
GUEST_KERNEL_URL=$(json_string '.guest_kernel.url')
GUEST_KERNEL_SHA256=$(json_string '.guest_kernel.sha256')
GUEST_KERNEL_CONFIG_URL=$(jq -er '.guest_kernel.config_url // empty' "$VERSIONS")
GUEST_KERNEL_CONFIG_SHA256=$(jq -er '.guest_kernel.config_sha256 // empty' "$VERSIONS")

GO_VERSION=$(json_string '.go.version')
GO_URL=$(json_string '.go.url')
GO_SHA256=$(json_string '.go.sha256')

NODEJS_VERSION=$(json_string '.nodejs.version')
NODEJS_URL=$(json_string '.nodejs.url')
NODEJS_SHA256=$(json_string '.nodejs.sha256')

GITHUB_ACTIONS_RUNNER_VERSION=$(json_string '.github_actions_runner.version')
GITHUB_ACTIONS_RUNNER_URL=$(json_string '.github_actions_runner.url')
GITHUB_ACTIONS_RUNNER_SHA256=$(json_string '.github_actions_runner.sha256')

case "$UBUNTU_BASE_ARCH:$GUEST_KERNEL_ARCH" in
  amd64:x86_64) ;;
  *)
    echo "ERROR: guest kernel arch ($GUEST_KERNEL_ARCH) does not match Ubuntu base arch ($UBUNTU_BASE_ARCH)" >&2
    exit 1
    ;;
esac

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

echo "-> downloading Ubuntu base rootfs"
UBUNTU_TARBALL="$WORKDIR/ubuntu-base.tar.gz"
download_checked "$UBUNTU_BASE_URL" "$UBUNTU_BASE_SHA256" "$UBUNTU_TARBALL"

echo "-> extracting Ubuntu base rootfs"
tar -xzf "$UBUNTU_TARBALL" --numeric-owner -C "$ROOTFS"

echo "-> downloading pinned guest kernel"
KERNEL_TMP="$WORKDIR/vmlinux"
download_checked "$GUEST_KERNEL_URL" "$GUEST_KERNEL_SHA256" "$KERNEL_TMP"
install -m 0644 "$KERNEL_TMP" "$OUTPUT_DIR/vmlinux"

if [[ -n "$GUEST_KERNEL_CONFIG_URL" ]]; then
  KERNEL_CONFIG_TMP="$WORKDIR/vmlinux.config"
  download_checked "$GUEST_KERNEL_CONFIG_URL" "$GUEST_KERNEL_CONFIG_SHA256" "$KERNEL_CONFIG_TMP"
  grep -q '^CONFIG_VIRTIO_VSOCKETS=y$' "$KERNEL_CONFIG_TMP" || {
    echo "ERROR: guest kernel is missing CONFIG_VIRTIO_VSOCKETS=y" >&2
    exit 1
  }
  grep -q '^CONFIG_HW_RANDOM_VIRTIO=y$' "$KERNEL_CONFIG_TMP" || {
    echo "ERROR: guest kernel is missing CONFIG_HW_RANDOM_VIRTIO=y" >&2
    exit 1
  }
  install -m 0644 "$KERNEL_CONFIG_TMP" "$OUTPUT_DIR/vmlinux.config"
fi

echo "-> preparing chroot"
mkdir -p "$ROOTFS/usr/local/bin" "$ROOTFS/usr/sbin" "$ROOTFS/opt/forge-metal"
rm -f "$ROOTFS/etc/resolv.conf"
install -m 0644 /etc/resolv.conf "$ROOTFS/etc/resolv.conf"
cat > "$ROOTFS/usr/sbin/policy-rc.d" <<'POLICY'
#!/bin/sh
exit 101
POLICY
chmod 0755 "$ROOTFS/usr/sbin/policy-rc.d"
mount_chroot_filesystems

echo "-> installing Ubuntu packages"
run_chroot apt-get update
run_chroot apt-get install -y --no-install-recommends \
  bash \
  build-essential \
  ca-certificates \
  curl \
  git \
  iproute2 \
  jq \
  make \
  openssh-client \
  pkg-config \
  python3 \
  sudo \
  tar \
  unzip \
  xz-utils \
  zip \
  zstd

echo "-> creating runner account"
if ! chroot "$ROOTFS" getent group runner >/dev/null; then
  chroot "$ROOTFS" groupadd --gid 1000 runner
fi
if ! chroot "$ROOTFS" id runner >/dev/null 2>&1; then
  chroot "$ROOTFS" useradd --uid 1000 --gid 1000 --home-dir /home/runner --shell /bin/bash --create-home runner
fi

echo "-> installing Go $GO_VERSION"
GO_TARBALL="$WORKDIR/go.tar.gz"
download_checked "$GO_URL" "$GO_SHA256" "$GO_TARBALL"
rm -rf "$ROOTFS/usr/local/go"
tar -xzf "$GO_TARBALL" -C "$ROOTFS/usr/local"
ln -sf /usr/local/go/bin/go "$ROOTFS/usr/local/bin/go"
ln -sf /usr/local/go/bin/gofmt "$ROOTFS/usr/local/bin/gofmt"

echo "-> installing Node.js $NODEJS_VERSION"
NODEJS_TARBALL="$WORKDIR/nodejs.tar.xz"
download_checked "$NODEJS_URL" "$NODEJS_SHA256" "$NODEJS_TARBALL"
rm -rf "$ROOTFS/opt/forge-metal/nodejs"
mkdir -p "$ROOTFS/opt/forge-metal/nodejs"
tar -xJf "$NODEJS_TARBALL" -C "$ROOTFS/opt/forge-metal/nodejs" --strip-components=1
for binary in node npm npx corepack; do
  ln -sf "/opt/forge-metal/nodejs/bin/$binary" "$ROOTFS/usr/local/bin/$binary"
done
run_chroot /usr/local/bin/corepack enable

echo "-> installing GitHub Actions runner $GITHUB_ACTIONS_RUNNER_VERSION"
GITHUB_RUNNER_TARBALL="$WORKDIR/actions-runner.tar.gz"
download_checked "$GITHUB_ACTIONS_RUNNER_URL" "$GITHUB_ACTIONS_RUNNER_SHA256" "$GITHUB_RUNNER_TARBALL"
rm -rf "$ROOTFS/opt/actions-runner"
mkdir -p "$ROOTFS/opt/actions-runner"
tar -xzf "$GITHUB_RUNNER_TARBALL" -C "$ROOTFS/opt/actions-runner"
run_chroot /bin/bash -lc 'cd /opt/actions-runner && ./bin/installdependencies.sh'

echo "-> installing vm-bridge"
rm -f "$ROOTFS/sbin/init"
if [[ -f "$SCRIPT_DIR/vm-bridge" ]]; then
  install -D -m 0755 "$SCRIPT_DIR/vm-bridge" "$ROOTFS/sbin/init"
elif command -v go >/dev/null 2>&1 && [[ -f "$PROJECT_ROOT/../vm-orchestrator/go.mod" ]]; then
  CGO_ENABLED=0 go build -ldflags='-s -w' -o "$ROOTFS/sbin/init" "$PROJECT_ROOT/../vm-orchestrator/cmd/vm-bridge"
else
  echo "ERROR: no pre-built vm-bridge and no Go project found at $PROJECT_ROOT" >&2
  exit 1
fi
install -D -m 0755 "$ROOTFS/sbin/init" "$ROOTFS/usr/local/bin/vm-bridge"

echo "-> installing vm-guest-telemetry"
VM_GUEST_TELEMETRY_SRC="$SCRIPT_DIR/vm-guest-telemetry"
if [[ ! -f "$VM_GUEST_TELEMETRY_SRC" ]]; then
  if command -v zig >/dev/null 2>&1 && [[ -f "$PROJECT_ROOT/../vm-guest-telemetry/build.zig" ]]; then
    (
      cd "$PROJECT_ROOT/../vm-guest-telemetry"
      zig build -Doptimize=ReleaseSafe
    )
    VM_GUEST_TELEMETRY_SRC="$PROJECT_ROOT/../vm-guest-telemetry/zig-out/bin/vm-guest-telemetry"
  else
    echo "ERROR: missing vm-guest-telemetry binary" >&2
    exit 1
  fi
fi
install -D -m 0755 "$VM_GUEST_TELEMETRY_SRC" "$ROOTFS/usr/local/bin/vm-guest-telemetry"

echo "-> finalizing guest filesystem"
mkdir -p "$ROOTFS"/{home/runner,workspace,tmp,dev/pts,dev/shm,opt/hostedtoolcache}
mkdir -p "$ROOTFS/home/runner/.cache" "$ROOTFS/home/runner/.config" "$ROOTFS/home/runner/work"
chown -R 1000:1000 "$ROOTFS/home/runner" "$ROOTFS/workspace" "$ROOTFS/opt/actions-runner" "$ROOTFS/opt/hostedtoolcache"
chmod 1777 "$ROOTFS/tmp"

cat > "$ROOTFS/etc/sudoers.d/runner" <<'SUDOERS'
runner ALL=(ALL) NOPASSWD:ALL
SUDOERS
chmod 0440 "$ROOTFS/etc/sudoers.d/runner"

cat > "$ROOTFS/etc/npmrc" <<'NPMRC'
# Registry is injected at runtime by vm-bridge.
NPMRC

cat > "$ROOTFS/etc/profile.d/forge-metal-toolchain.sh" <<'PROFILE'
export PATH=/usr/local/go/bin:/opt/forge-metal/nodejs/bin:$PATH
PROFILE
chmod 0644 "$ROOTFS/etc/profile.d/forge-metal-toolchain.sh"

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
GO_BINARY_SHA256=$(sha256sum "$ROOTFS/usr/local/go/bin/go" | awk '{print $1}')
GO_BINARY_BYTES=$(stat -c '%s' "$ROOTFS/usr/local/go/bin/go")
NODE_BINARY_SHA256=$(sha256sum "$ROOTFS/opt/forge-metal/nodejs/bin/node" | awk '{print $1}')
NODE_BINARY_BYTES=$(stat -c '%s' "$ROOTFS/opt/forge-metal/nodejs/bin/node")
GITHUB_RUNNER_SHA256=$(sha256sum "$ROOTFS/opt/actions-runner/run.sh" | awk '{print $1}')
GITHUB_RUNNER_BYTES=$(stat -c '%s' "$ROOTFS/opt/actions-runner/run.sh")
{
  echo
  echo "# custom_components"
  echo "file path=/sbin/init component=vm-bridge sha256=$INIT_SHA256 bytes=$INIT_BYTES"
  echo "file path=/usr/local/bin/vm-bridge component=vm-bridge sha256=$INIT_SHA256 bytes=$INIT_BYTES"
  echo "file path=/usr/local/bin/vm-guest-telemetry component=vm-guest-telemetry sha256=$VM_GUEST_TELEMETRY_SHA256 bytes=$VM_GUEST_TELEMETRY_BYTES"
  echo "file path=/usr/local/go/bin/go component=go version=$GO_VERSION sha256=$GO_BINARY_SHA256 bytes=$GO_BINARY_BYTES"
  echo "file path=/opt/forge-metal/nodejs/bin/node component=nodejs version=$NODEJS_VERSION sha256=$NODE_BINARY_SHA256 bytes=$NODE_BINARY_BYTES"
  echo "file path=/opt/actions-runner/run.sh component=github-actions-runner version=$GITHUB_ACTIONS_RUNNER_VERSION sha256=$GITHUB_RUNNER_SHA256 bytes=$GITHUB_RUNNER_BYTES"
} >> "$OUTPUT_DIR/sbom.txt"

unmount_chroot_mounts

echo "-> building ext4 image ($ROOTFS_IMAGE_SIZE)"
mke2fs -F -t ext4 -d "$ROOTFS" -L guestroot -b 4096 "$OUTPUT_DIR/rootfs.ext4" "$ROOTFS_IMAGE_SIZE"

echo "-> computing guest artifact metrics"
ROOTFS_TREE_BYTES=$(du -sb "$ROOTFS" | awk '{print $1}')
ROOTFS_APPARENT_BYTES=$(stat -c '%s' "$OUTPUT_DIR/rootfs.ext4")
ROOTFS_ALLOCATED_BYTES=$(( $(stat -c '%b' "$OUTPUT_DIR/rootfs.ext4") * $(stat -c '%B' "$OUTPUT_DIR/rootfs.ext4") ))
read -r ROOTFS_FILESYSTEM_BYTES ROOTFS_USED_BYTES ROOTFS_FREE_BYTES <<<"$(dumpe2fs -h "$OUTPUT_DIR/rootfs.ext4" 2>/dev/null | awk -F: '
/Block count:/ {gsub(/^[ \t]+/, "", $2); block_count=$2}
/Free blocks:/ {gsub(/^[ \t]+/, "", $2); free_blocks=$2}
/Block size:/ {gsub(/^[ \t]+/, "", $2); block_size=$2}
END {
  total = block_count * block_size
  free = free_blocks * block_size
  used = total - free
  printf "%.0f %.0f %.0f", total, used, free
}')"
ROOTFS_SHA256=$(sha256sum "$OUTPUT_DIR/rootfs.ext4" | awk '{print $1}')
KERNEL_BYTES=$(stat -c '%s' "$OUTPUT_DIR/vmlinux")
KERNEL_SHA256=$(sha256sum "$OUTPUT_DIR/vmlinux" | awk '{print $1}')
SBOM_BYTES=$(stat -c '%s' "$OUTPUT_DIR/sbom.txt")
SBOM_SHA256=$(sha256sum "$OUTPUT_DIR/sbom.txt" | awk '{print $1}')

jq -n \
  --arg built_at_utc "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  --arg ubuntu_base_version "$UBUNTU_BASE_VERSION" \
  --arg ubuntu_base_arch "$UBUNTU_BASE_ARCH" \
  --arg firecracker_version "$FIRECRACKER_VERSION" \
  --arg guest_kernel_version "$GUEST_KERNEL_VERSION" \
  --arg rootfs_image_size "$ROOTFS_IMAGE_SIZE" \
  --arg rootfs_sha256 "$ROOTFS_SHA256" \
  --arg kernel_sha256 "$KERNEL_SHA256" \
  --arg sbom_sha256 "$SBOM_SHA256" \
  --arg rootfs_tree_bytes "$ROOTFS_TREE_BYTES" \
  --arg rootfs_apparent_bytes "$ROOTFS_APPARENT_BYTES" \
  --arg rootfs_allocated_bytes "$ROOTFS_ALLOCATED_BYTES" \
  --arg rootfs_filesystem_bytes "$ROOTFS_FILESYSTEM_BYTES" \
  --arg rootfs_used_bytes "$ROOTFS_USED_BYTES" \
  --arg rootfs_free_bytes "$ROOTFS_FREE_BYTES" \
  --arg kernel_bytes "$KERNEL_BYTES" \
  --arg sbom_bytes "$SBOM_BYTES" \
  --arg package_count "$PACKAGE_COUNT" \
  --arg init_sha256 "$INIT_SHA256" \
  --arg init_bytes "$INIT_BYTES" \
  --arg vm_guest_telemetry_sha256 "$VM_GUEST_TELEMETRY_SHA256" \
  --arg vm_guest_telemetry_bytes "$VM_GUEST_TELEMETRY_BYTES" \
  --arg go_version "$GO_VERSION" \
  --arg go_sha256 "$GO_BINARY_SHA256" \
  --arg go_bytes "$GO_BINARY_BYTES" \
  --arg nodejs_version "$NODEJS_VERSION" \
  --arg nodejs_sha256 "$NODE_BINARY_SHA256" \
  --arg nodejs_bytes "$NODE_BINARY_BYTES" \
  --arg github_actions_runner_version "$GITHUB_ACTIONS_RUNNER_VERSION" \
  --arg github_actions_runner_sha256 "$GITHUB_RUNNER_SHA256" \
  --arg github_actions_runner_bytes "$GITHUB_RUNNER_BYTES" \
  '{
    schema_version: 2,
    built_at_utc: $built_at_utc,
    ubuntu_base_version: $ubuntu_base_version,
    ubuntu_base_arch: $ubuntu_base_arch,
    firecracker_version: $firecracker_version,
    guest_kernel_version: $guest_kernel_version,
    rootfs_image_size: $rootfs_image_size,
    rootfs_sha256: $rootfs_sha256,
    rootfs_tree_bytes: ($rootfs_tree_bytes | tonumber),
    rootfs_apparent_bytes: ($rootfs_apparent_bytes | tonumber),
    rootfs_allocated_bytes: ($rootfs_allocated_bytes | tonumber),
    rootfs_filesystem_bytes: ($rootfs_filesystem_bytes | tonumber),
    rootfs_used_bytes: ($rootfs_used_bytes | tonumber),
    rootfs_free_bytes: ($rootfs_free_bytes | tonumber),
    kernel_sha256: $kernel_sha256,
    kernel_bytes: ($kernel_bytes | tonumber),
    sbom_sha256: $sbom_sha256,
    sbom_bytes: ($sbom_bytes | tonumber),
    package_count: ($package_count | tonumber),
    init_sha256: $init_sha256,
    init_bytes: ($init_bytes | tonumber),
    vm_guest_telemetry_present: true,
    vm_guest_telemetry_sha256: $vm_guest_telemetry_sha256,
    vm_guest_telemetry_bytes: ($vm_guest_telemetry_bytes | tonumber),
    go_version: $go_version,
    go_sha256: $go_sha256,
    go_bytes: ($go_bytes | tonumber),
    nodejs_version: $nodejs_version,
    nodejs_sha256: $nodejs_sha256,
    nodejs_bytes: ($nodejs_bytes | tonumber),
    github_actions_runner_version: $github_actions_runner_version,
    github_actions_runner_sha256: $github_actions_runner_sha256,
    github_actions_runner_bytes: ($github_actions_runner_bytes | tonumber)
  }' > "$OUTPUT_DIR/guest-artifacts.json"

echo "OK rootfs built: $OUTPUT_DIR/rootfs.ext4"
echo "OK SBOM written: $OUTPUT_DIR/sbom.txt"
echo "OK guest artifact metrics written: $OUTPUT_DIR/guest-artifacts.json"
