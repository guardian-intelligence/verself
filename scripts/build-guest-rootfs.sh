#!/usr/bin/env bash
set -euo pipefail

# build-guest-rootfs.sh — Build an Alpine-based ext4 rootfs for Firecracker CI VMs.
# Requires: root, go in PATH, internet access, e2fsprogs.
# Produces: ci/output/rootfs.ext4, ci/output/sbom.txt

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
# go is only required if no pre-built forgevm-init exists
if [[ ! -f "$SCRIPT_DIR/forgevm-init" ]] && ! command -v go >/dev/null 2>&1; then
  echo "ERROR: no pre-built forgevm-init and go not in PATH" >&2; exit 1
fi

# Read version pins
ALPINE_URL=$(jq -r '.alpine.url' "$VERSIONS")
ALPINE_SHA256=$(jq -r '.alpine.sha256' "$VERSIONS")

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

# --- Install packages via chroot ---
echo "→ installing packages"
cp /etc/resolv.conf "$ROOTFS/etc/resolv.conf"
chroot "$ROOTFS" /bin/sh -c "apk update && apk add --no-cache bash coreutils git nodejs npm ca-certificates"

# --- Install forgevm-init (static Go binary → /sbin/init) ---
# If a pre-built binary exists next to the script (e.g., scp'd by Makefile), use it.
# Otherwise, build from source (requires Go project checkout).
rm -f "$ROOTFS/sbin/init"  # Remove Alpine's busybox symlink
if [[ -f "$SCRIPT_DIR/forgevm-init" ]]; then
  echo "→ using pre-built forgevm-init"
  cp "$SCRIPT_DIR/forgevm-init" "$ROOTFS/sbin/init"
elif command -v go >/dev/null 2>&1 && [[ -f "$PROJECT_ROOT/go.mod" ]]; then
  echo "→ building forgevm-init from source"
  CGO_ENABLED=0 go build -ldflags='-s -w' -o "$ROOTFS/sbin/init" "$PROJECT_ROOT/cmd/forgevm-init"
else
  echo "ERROR: no pre-built forgevm-init and no Go project found at $PROJECT_ROOT" >&2
  exit 1
fi

# --- Essential config ---
cat > "$ROOTFS/etc/passwd" <<'PASSWD'
root:x:0:0:root:/root:/bin/bash
runner:x:1000:1000:runner:/home/runner:/bin/bash
nobody:x:65534:65534:nobody:/nonexistent:/usr/sbin/nologin
PASSWD

cat > "$ROOTFS/etc/group" <<'GROUP'
root:x:0:
runner:x:1000:
nogroup:x:65534:
GROUP

echo "nameserver 8.8.8.8" > "$ROOTFS/etc/resolv.conf"

# --- Create required directories ---
mkdir -p "$ROOTFS"/{etc/ci,home/runner,workspace,dev,proc,sys,run,tmp,dev/pts,dev/shm}

# --- Generate SBOM ---
echo "→ generating SBOM"
mkdir -p "$OUTPUT_DIR"
chroot "$ROOTFS" apk list --installed > "$OUTPUT_DIR/sbom.txt"

# --- Build ext4 image ---
echo "→ building ext4 image (4G)"
mke2fs -t ext4 -d "$ROOTFS" -L ciroot -b 4096 "$OUTPUT_DIR/rootfs.ext4" 4G

echo "✓ rootfs built: $OUTPUT_DIR/rootfs.ext4"
echo "✓ SBOM written: $OUTPUT_DIR/sbom.txt"
