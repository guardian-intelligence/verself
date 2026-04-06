#!/usr/bin/env bash
set -euo pipefail

# build-guest-rootfs.sh — Build an Alpine-based ext4 rootfs for Firecracker CI VMs.
# Standard Linux paths, standard SBOM.
#
# LEARNING: Nix rootfs had /nix/store/ symlink farms inside the guest. Alpine gives
# standard paths (/usr/bin/node, /usr/bin/git) that work natively with fast-sandbox-init's
# PATH resolution and chroot-based golden image baking.
#
# Two-layer architecture:
#   Layer 1 (this script): base OS + packages + initdb -> rootfs.ext4
#   Layer 2 (repo warm): repo checkout + prepare command + optional DB setup -> ZFS snapshot
#
# Requires: root, internet access, e2fsprogs. go only if no pre-built fast-sandbox-init.
# Produces: ci/output/rootfs.ext4, ci/output/sbom.txt, ci/output/guest-artifacts.json

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

# Support two layouts:
# 1. Running from project root: scripts/build-guest-rootfs.sh → ci/versions.json
# 2. Flat scp to /tmp: /tmp/build-guest-rootfs.sh + /tmp/versions.json
if [[ -f "$SCRIPT_DIR/../ci/versions.json" ]]; then
  PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
  VERSIONS="$PROJECT_ROOT/ci/versions.json"
  OUTPUT_DIR="$PROJECT_ROOT/ci/output"
elif [[ -f "$SCRIPT_DIR/versions.json" ]]; then
  PROJECT_ROOT="$SCRIPT_DIR"
  VERSIONS="$SCRIPT_DIR/versions.json"
  OUTPUT_DIR="$SCRIPT_DIR/ci/output"
else
  echo "ERROR: cannot find versions.json (looked in $SCRIPT_DIR/../ci/ and $SCRIPT_DIR/)" >&2
  exit 1
fi

if [[ $EUID -ne 0 ]]; then
  echo "ERROR: must run as root" >&2
  exit 1
fi

command -v jq >/dev/null 2>&1 || { echo "ERROR: jq not in PATH" >&2; exit 1; }
command -v mke2fs >/dev/null 2>&1 || { echo "ERROR: mke2fs (e2fsprogs) not in PATH" >&2; exit 1; }
command -v dumpe2fs >/dev/null 2>&1 || { echo "ERROR: dumpe2fs (e2fsprogs) not in PATH" >&2; exit 1; }
# go is only required if no pre-built fast-sandbox-init exists
if [[ ! -f "$SCRIPT_DIR/fast-sandbox-init" ]] && ! command -v go >/dev/null 2>&1; then
  echo "ERROR: no pre-built fast-sandbox-init and go not in PATH" >&2; exit 1
fi

# Read version pins
ALPINE_URL=$(jq -r '.alpine.url' "$VERSIONS")
ALPINE_SHA256=$(jq -r '.alpine.sha256' "$VERSIONS")
ALPINE_VERSION=$(jq -r '.alpine.version' "$VERSIONS")
ARCH=$(jq -r '.alpine.arch' "$VERSIONS")
FIRECRACKER_VERSION=$(jq -r '.firecracker.version' "$VERSIONS")
GUEST_KERNEL_VERSION=$(jq -r '.guest_kernel.version' "$VERSIONS")
GUEST_KERNEL_ARCH=$(jq -r '.guest_kernel.arch' "$VERSIONS")
GUEST_KERNEL_URL=$(jq -r '.guest_kernel.url' "$VERSIONS")
GUEST_KERNEL_SHA256=$(jq -r '.guest_kernel.sha256' "$VERSIONS")
GUEST_KERNEL_CONFIG_URL=$(jq -r '.guest_kernel.config_url // empty' "$VERSIONS")
GUEST_KERNEL_CONFIG_SHA256=$(jq -r '.guest_kernel.config_sha256 // empty' "$VERSIONS")

if [[ "$GUEST_KERNEL_ARCH" != "$ARCH" ]]; then
  echo "ERROR: guest kernel arch ($GUEST_KERNEL_ARCH) does not match Alpine arch ($ARCH)" >&2
  exit 1
fi

WORKDIR=$(mktemp -d)
trap 'rm -rf "$WORKDIR"' EXIT

ROOTFS="$WORKDIR/rootfs"
mkdir -p "$ROOTFS"

# --- Download and verify Alpine minirootfs ---
echo "→ downloading Alpine minirootfs"
TARBALL="$WORKDIR/alpine.tar.gz"
curl -fsSL -o "$TARBALL" "$ALPINE_URL"

echo "$ALPINE_SHA256  $TARBALL" | sha256sum -c - || {
  echo "ERROR: SHA256 mismatch" >&2
  exit 1
}

echo "→ extracting rootfs"
tar -xzf "$TARBALL" -C "$ROOTFS"

# --- Download explicitly pinned guest kernel ---
echo "→ downloading pinned guest kernel"
mkdir -p "$OUTPUT_DIR"
KERNEL_TMP="$WORKDIR/vmlinux"
curl -fsSL -o "$KERNEL_TMP" "$GUEST_KERNEL_URL"
echo "$GUEST_KERNEL_SHA256  $KERNEL_TMP" | sha256sum -c - || {
  echo "ERROR: guest kernel SHA256 mismatch" >&2
  exit 1
}
install -m 0644 "$KERNEL_TMP" "$OUTPUT_DIR/vmlinux"

if [[ -n "$GUEST_KERNEL_CONFIG_URL" ]]; then
  KERNEL_CONFIG_TMP="$WORKDIR/vmlinux.config"
  curl -fsSL -o "$KERNEL_CONFIG_TMP" "$GUEST_KERNEL_CONFIG_URL"
  if [[ -n "$GUEST_KERNEL_CONFIG_SHA256" ]]; then
    echo "$GUEST_KERNEL_CONFIG_SHA256  $KERNEL_CONFIG_TMP" | sha256sum -c - || {
      echo "ERROR: guest kernel config SHA256 mismatch" >&2
      exit 1
    }
  fi

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

# --- Install packages via chroot ---
echo "→ installing packages"
cp /etc/resolv.conf "$ROOTFS/etc/resolv.conf"
chroot "$ROOTFS" /bin/sh -c "apk update && apk add --no-cache bash coreutils curl git nodejs npm unzip ca-certificates postgresql"
if ! chroot "$ROOTFS" /bin/sh -c "apk add --no-cache bun" >/dev/null 2>&1; then
  echo "→ installing bun via upstream installer"
  chroot "$ROOTFS" /bin/sh -c "export BUN_INSTALL=/usr/local && curl -fsSL https://bun.sh/install | bash"
fi
chroot "$ROOTFS" /bin/sh -c "corepack enable || true"

# --- Install fast-sandbox-init (static Go binary → /sbin/init) ---
# If a pre-built binary exists next to the script (for example, scp'd by Makefile), use it.
# Otherwise, build from source (requires Go project checkout).
# LEARNING: Alpine creates /sbin/init as a busybox symlink. `cp` follows symlinks,
# so without this rm, cp overwrites /bin/busybox instead of replacing the symlink.
rm -f "$ROOTFS/sbin/init"
if [[ -f "$SCRIPT_DIR/fast-sandbox-init" ]]; then
  echo "→ using pre-built fast-sandbox-init"
  cp "$SCRIPT_DIR/fast-sandbox-init" "$ROOTFS/sbin/init"
elif command -v go >/dev/null 2>&1 && [[ -f "$PROJECT_ROOT/../fast-sandbox/go.mod" ]]; then
  echo "→ building fast-sandbox-init from source"
  CGO_ENABLED=0 go build -ldflags='-s -w' -o "$ROOTFS/sbin/init" "$PROJECT_ROOT/../fast-sandbox/cmd/fast-sandbox-init"
else
  echo "ERROR: no pre-built fast-sandbox-init and no Go project found at $PROJECT_ROOT" >&2
  exit 1
fi

# --- Install homestead-smelter guest agent ---
# The Zig guest agent is a required part of the Firecracker image. Prefer a
# pre-built binary staged beside the script; fall back to building from source
# only when the repo checkout and Zig are available on the build host.
SMELTER_GUEST_SRC="$SCRIPT_DIR/homestead-smelter-guest"
if [[ ! -f "$SMELTER_GUEST_SRC" ]]; then
  if command -v zig >/dev/null 2>&1 && [[ -f "$PROJECT_ROOT/../homestead-smelter/build.zig" ]]; then
    echo "→ building homestead-smelter-guest from source"
    (
      cd "$PROJECT_ROOT/../homestead-smelter"
      zig build -Doptimize=ReleaseSafe
    )
    SMELTER_GUEST_SRC="$PROJECT_ROOT/../homestead-smelter/zig-out/bin/homestead-smelter-guest"
  else
    echo "ERROR: missing homestead-smelter-guest binary" >&2
    exit 1
  fi
fi
echo "→ installing homestead-smelter-guest"
install -D -m 0755 "$SMELTER_GUEST_SRC" "$ROOTFS/usr/local/bin/homestead-smelter-guest"

# --- Essential config ---
cat > "$ROOTFS/etc/passwd" <<'PASSWD'
root:x:0:0:root:/root:/bin/bash
postgres:x:70:70:PostgreSQL:/var/lib/postgresql:/bin/sh
runner:x:1000:1000:runner:/home/runner:/bin/bash
nobody:x:65534:65534:nobody:/nonexistent:/usr/sbin/nologin
PASSWD

cat > "$ROOTFS/etc/group" <<'GROUP'
root:x:0:
postgres:x:70:
runner:x:1000:
nogroup:x:65534:
GROUP

echo "nameserver 8.8.8.8" > "$ROOTFS/etc/resolv.conf"
cat > "$ROOTFS/etc/npmrc" <<'NPMRC'
# Registry is injected at runtime by fast-sandbox-init.
NPMRC

# --- Create required directories ---
mkdir -p "$ROOTFS"/{etc/ci,home/runner,workspace,dev,proc,sys,run,tmp,dev/pts,dev/shm}

# --- Initialize PostgreSQL data directory ---
echo "→ initializing PostgreSQL"
# LEARNING: initdb needs /dev/null which doesn't exist in an unpacked tarball.
# mknod fails because Alpine's /dev has conflicting entries. bind-mount works.
mount --bind /dev "$ROOTFS/dev"
chroot "$ROOTFS" su postgres -c "initdb -D /var/lib/postgresql/data --no-locale --encoding=UTF8"
umount "$ROOTFS/dev"
# Configure: listen on localhost, no SSL, trust local connections
echo "listen_addresses = 'localhost'" >> "$ROOTFS/var/lib/postgresql/data/postgresql.conf"
echo "unix_socket_directories = '/run/postgresql'" >> "$ROOTFS/var/lib/postgresql/data/postgresql.conf"
sed -i 's/^local.*all.*all.*trust/local all all trust/' "$ROOTFS/var/lib/postgresql/data/pg_hba.conf"
echo "host all all 127.0.0.1/32 trust" >> "$ROOTFS/var/lib/postgresql/data/pg_hba.conf"

# --- Generate SBOM ---
echo "→ generating SBOM"
APK_INSTALLED_TMP="$WORKDIR/apk-installed.txt"
chroot "$ROOTFS" apk list --installed > "$APK_INSTALLED_TMP"
PACKAGE_COUNT=$(wc -l < "$APK_INSTALLED_TMP" | tr -d '[:space:]')
cp "$APK_INSTALLED_TMP" "$OUTPUT_DIR/sbom.txt"

INIT_SHA256=$(sha256sum "$ROOTFS/sbin/init" | awk '{print $1}')
INIT_BYTES=$(stat -c '%s' "$ROOTFS/sbin/init")
SMELTER_GUEST_PRESENT=false
SMELTER_GUEST_SHA256=""
SMELTER_GUEST_BYTES=0
if [[ -f "$ROOTFS/usr/local/bin/homestead-smelter-guest" ]]; then
  SMELTER_GUEST_PRESENT=true
  SMELTER_GUEST_SHA256=$(sha256sum "$ROOTFS/usr/local/bin/homestead-smelter-guest" | awk '{print $1}')
  SMELTER_GUEST_BYTES=$(stat -c '%s' "$ROOTFS/usr/local/bin/homestead-smelter-guest")
fi

{
  echo
  echo "# custom_components"
  echo "file path=/sbin/init component=fast-sandbox-init sha256=$INIT_SHA256 bytes=$INIT_BYTES"
  if [[ "$SMELTER_GUEST_PRESENT" == "true" ]]; then
    echo "file path=/usr/local/bin/homestead-smelter-guest component=homestead-smelter-guest sha256=$SMELTER_GUEST_SHA256 bytes=$SMELTER_GUEST_BYTES"
  fi
} >> "$OUTPUT_DIR/sbom.txt"

# --- Build ext4 image ---
echo "→ building ext4 image (4G)"
mke2fs -F -t ext4 -d "$ROOTFS" -L ciroot -b 4096 "$OUTPUT_DIR/rootfs.ext4" 4G

# --- Emit guest artifact manifest ---
echo "→ computing guest artifact metrics"
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
  --arg alpine_version "$ALPINE_VERSION" \
  --arg firecracker_version "$FIRECRACKER_VERSION" \
  --arg guest_kernel_version "$GUEST_KERNEL_VERSION" \
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
  --arg homestead_smelter_guest_present "$SMELTER_GUEST_PRESENT" \
  --arg homestead_smelter_guest_sha256 "$SMELTER_GUEST_SHA256" \
  --arg homestead_smelter_guest_bytes "$SMELTER_GUEST_BYTES" \
  '{
    schema_version: 1,
    built_at_utc: $built_at_utc,
    alpine_version: $alpine_version,
    firecracker_version: $firecracker_version,
    guest_kernel_version: $guest_kernel_version,
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
    homestead_smelter_guest_present: ($homestead_smelter_guest_present == "true"),
    homestead_smelter_guest_sha256: (if $homestead_smelter_guest_sha256 == "" then null else $homestead_smelter_guest_sha256 end),
    homestead_smelter_guest_bytes: (if $homestead_smelter_guest_present == "true" then ($homestead_smelter_guest_bytes | tonumber) else null end)
  }' > "$OUTPUT_DIR/guest-artifacts.json"

echo "✓ rootfs built: $OUTPUT_DIR/rootfs.ext4"
echo "✓ SBOM written: $OUTPUT_DIR/sbom.txt"
echo "✓ guest artifact metrics written: $OUTPUT_DIR/guest-artifacts.json"
