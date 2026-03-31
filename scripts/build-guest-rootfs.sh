#!/usr/bin/env bash
set -euo pipefail

# build-guest-rootfs.sh — Build an Alpine-based ext4 rootfs for Firecracker CI VMs.
# Replaces the former Nix ciGuestRootfs derivation. Standard Linux paths, standard SBOM.
#
# LEARNING: Nix rootfs had /nix/store/ symlink farms inside the guest. Alpine gives
# standard paths (/usr/bin/node, /usr/bin/git) that work natively with forgevm-init's
# PATH resolution and chroot-based golden image baking.
#
# Two-layer architecture:
#   Layer 1 (this script): base OS + packages + initdb → rootfs.ext4
#   Layer 2 (golden_image.yml): app code + npm install + DB seed → ZFS snapshot
#
# Requires: root, internet access, e2fsprogs. go only if no pre-built forgevm-init.
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
FIRECRACKER_VERSION=$(jq -r '.firecracker.version' "$VERSIONS")
ARCH=$(jq -r '.alpine.arch' "$VERSIONS")
FC_CI_VERSION="${FIRECRACKER_VERSION%.*}"

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

# --- Download Firecracker-compatible guest kernel ---
echo "→ downloading guest kernel for Firecracker CI v${FC_CI_VERSION}"
KERNEL_KEY=$(curl -fsSL "http://spec.ccfc.min.s3.amazonaws.com/?prefix=firecracker-ci/v${FC_CI_VERSION}/${ARCH}/vmlinux-&list-type=2" \
  | grep -oE "firecracker-ci/v${FC_CI_VERSION}/${ARCH}/vmlinux-[0-9]+\.[0-9]+\.[0-9]{1,3}" \
  | sort -V | tail -1)
if [[ -z "$KERNEL_KEY" ]]; then
  echo "ERROR: could not resolve Firecracker CI kernel for v${FC_CI_VERSION}/${ARCH}" >&2
  exit 1
fi
mkdir -p "$OUTPUT_DIR"
curl -fsSL -o "$OUTPUT_DIR/vmlinux" "https://s3.amazonaws.com/spec.ccfc.min/${KERNEL_KEY}"

# --- Install packages via chroot ---
echo "→ installing packages"
cp /etc/resolv.conf "$ROOTFS/etc/resolv.conf"
chroot "$ROOTFS" /bin/sh -c "apk update && apk add --no-cache bash coreutils curl git nodejs npm unzip ca-certificates postgresql"
if ! chroot "$ROOTFS" /bin/sh -c "apk add --no-cache bun" >/dev/null 2>&1; then
  echo "→ installing bun via upstream installer"
  chroot "$ROOTFS" /bin/sh -c "export BUN_INSTALL=/usr/local && curl -fsSL https://bun.sh/install | bash"
fi
chroot "$ROOTFS" /bin/sh -c "corepack enable || true"

# --- Install forgevm-init (static Go binary → /sbin/init) ---
# If a pre-built binary exists next to the script (e.g., scp'd by Makefile), use it.
# Otherwise, build from source (requires Go project checkout).
# LEARNING: Alpine creates /sbin/init as a busybox symlink. `cp` follows symlinks,
# so without this rm, cp overwrites /bin/busybox instead of replacing the symlink.
rm -f "$ROOTFS/sbin/init"
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
# Registry is injected at runtime by /usr/local/bin/forge-metal-ci-run.
NPMRC

# --- Create required directories ---
mkdir -p "$ROOTFS"/{etc/ci,home/runner,workspace,dev,proc,sys,run,tmp,dev/pts,dev/shm}

# --- Initialize PostgreSQL data directory ---
echo "→ initializing PostgreSQL"
mkdir -p "$ROOTFS/run/postgresql"
chown 70:70 "$ROOTFS/run/postgresql"  # postgres uid/gid on Alpine
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

# --- Write generic CI service runner ---
cat > "$ROOTFS/usr/local/bin/forge-metal-ci-run" << 'WRAPPER'
#!/bin/sh
# Start requested services, switch to the requested workdir, and exec the CI command.
set -e
services=""
workdir="/workspace"
registry="${FORGE_METAL_NPM_REGISTRY:-}"

while [ "$#" -gt 0 ]; do
  case "$1" in
    --services)
      services="$2"
      shift 2
      ;;
    --workdir)
      workdir="$2"
      shift 2
      ;;
    --)
      shift
      break
      ;;
    *)
      echo "unknown argument: $1" >&2
      exit 2
      ;;
  esac
done

if [ -z "$registry" ]; then
  gateway="$(ip route show default | awk '/default/ {print $3; exit}')"
  if [ -z "$gateway" ]; then
    echo "unable to determine host gateway for registry access" >&2
    exit 1
  fi
  registry="http://${gateway}:4873"
fi

export npm_config_registry="$registry"
export NPM_CONFIG_REGISTRY="$registry"

case ",$services," in
  *,postgres,*)
    mkdir -p /run/postgresql && chown postgres:postgres /run/postgresql
    su postgres -c "pg_ctl start -D /var/lib/postgresql/data -l /tmp/pg.log -w"
    ;;
esac

cd "$workdir"
exec "$@"
WRAPPER
chmod +x "$ROOTFS/usr/local/bin/forge-metal-ci-run"

# Backward-compatible wrapper for the earlier tracer bullet path.
cat > "$ROOTFS/ci-start.sh" << 'WRAPPER'
#!/bin/sh
exec /usr/local/bin/forge-metal-ci-run --services postgres --workdir /workspace -- "$@"
WRAPPER
chmod +x "$ROOTFS/ci-start.sh"

# --- Generate SBOM ---
echo "→ generating SBOM"
chroot "$ROOTFS" apk list --installed > "$OUTPUT_DIR/sbom.txt"

# --- Build ext4 image ---
echo "→ building ext4 image (4G)"
mke2fs -F -t ext4 -d "$ROOTFS" -L ciroot -b 4096 "$OUTPUT_DIR/rootfs.ext4" 4G

echo "✓ rootfs built: $OUTPUT_DIR/rootfs.ext4"
echo "✓ SBOM written: $OUTPUT_DIR/sbom.txt"
