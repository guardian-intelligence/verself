#!/usr/bin/env bash
# ZFS-backed test harness for bmci doctor.
#
# Creates a minimal Ubuntu container on a file-backed ZFS pool.
# Host /nix/store is bind-mounted read-only; the container only writes
# nix profile state to the ZFS dataset. Rollback is instant.
#
# WARNING: Do not run nix-collect-garbage on the host while a scenario
# is in progress — the daemon could race and GC store paths the
# container is about to reference.
#
# Usage:
#   sudo ./tests/testbed/testbed.sh setup     # one-time (~30s)
#   sudo ./tests/testbed/testbed.sh shell      # interactive container
#   sudo ./tests/testbed/testbed.sh run [N|all] # run scenario(s)
#   sudo ./tests/testbed/testbed.sh destroy    # tear down
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

POOL_NAME="testpool"
IMG_DIR="/var/lib/forge-test"
IMG_PATH="${IMG_DIR}/zfs.img"
IMG_SIZE="5G"
DATASET="${POOL_NAME}/rootfs"
ROOTFS="${IMG_DIR}/rootfs"
CONTAINER_USER="testuser"
CONTAINER_UID=1000

# ── Helpers ───────────────────────────────────────────────────────────

die() { echo "ERROR: $*" >&2; exit 1; }

pool_exists() { zpool list "$POOL_NAME" &>/dev/null; }

dataset_exists() { zfs list "$DATASET" &>/dev/null; }

snap_exists() { zfs list -t snapshot "${DATASET}@${1}" &>/dev/null; }

require_pool() {
  pool_exists || die "Pool '$POOL_NAME' does not exist. Run: $0 setup"
}

require_snap() {
  snap_exists "$1" || die "Snapshot '${DATASET}@${1}' does not exist."
}

# Launch a command inside the container via systemd-nspawn.
# Usage: nspawn_exec [--interactive] [--root] <command>
nspawn_exec() {
  local interactive=false
  local run_user="$CONTAINER_USER"
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --interactive) interactive=true; shift ;;
      --root) run_user=root; shift ;;
      *) break ;;
    esac
  done

  local nspawn_args=(
    --directory="$ROOTFS"
    --bind-ro=/nix/store:/nix/store
    --bind-ro=/nix/var/nix/daemon-socket:/nix/var/nix/daemon-socket
    --bind-ro=/nix/var/nix/profiles:/nix/var/nix/profiles
    --bind-ro="${REPO_ROOT}:/workspace"
    --bind-ro="${REPO_ROOT}/bmci:/usr/local/bin/bmci"
    --private-users=no
    --register=no
    --as-pid2
    --chdir=/workspace
    --setenv=NIX_REMOTE=daemon
    --setenv=NIX_CONFIG="experimental-features = nix-command flakes
warn-dirty = false"
  )

  # Set HOME based on user
  local home_dir="/home/${CONTAINER_USER}"
  if [[ "$run_user" == "root" ]]; then
    home_dir="/root"
  fi
  nspawn_args+=(--setenv=HOME="$home_dir")

  # PATH: nix profile bins first, then system
  local container_path="/home/${CONTAINER_USER}/.nix-profile/bin"
  container_path="${container_path}:/nix/var/nix/profiles/default/bin"
  container_path="${container_path}:/usr/local/bin:/usr/bin:/bin"
  nspawn_args+=(--setenv=PATH="$container_path")

  if $interactive; then
    systemd-nspawn "${nspawn_args[@]}" --user="$run_user" /bin/bash
  else
    systemd-nspawn "${nspawn_args[@]}" --user="$run_user" \
      /bin/bash -c "$1"
  fi
}

# ── Subcommands ───────────────────────────────────────────────────────

cmd_setup() {
  if pool_exists; then
    die "Pool '$POOL_NAME' already exists. Run '$0 destroy' first."
  fi

  echo "=== Creating ZFS pool ==="
  mkdir -p "$IMG_DIR"
  truncate -s "$IMG_SIZE" "$IMG_PATH"
  zpool create "$POOL_NAME" "$IMG_PATH"
  zfs create -o mountpoint="$ROOTFS" -o compression=lz4 "$DATASET"

  echo "=== Bootstrapping Ubuntu 24.04 (minbase) ==="
  debootstrap --variant=minbase --include=git,ca-certificates \
    noble "$ROOTFS" http://archive.ubuntu.com/ubuntu

  echo "=== Configuring container ==="
  # Create testuser matching host uid
  chroot "$ROOTFS" useradd -m -u "$CONTAINER_UID" -s /bin/bash "$CONTAINER_USER"

  # Set up nix directories
  mkdir -p "$ROOTFS/home/${CONTAINER_USER}/.local/state/nix/profiles"
  chown -R "$CONTAINER_UID:$CONTAINER_UID" "$ROOTFS/home/${CONTAINER_USER}"

  # Allow any user to access the bind-mounted repo
  chroot "$ROOTFS" git config --system --add safe.directory /workspace

  # Create /nix mountpoints
  mkdir -p "$ROOTFS/nix/store"
  mkdir -p "$ROOTFS/nix/var/nix/daemon-socket"
  mkdir -p "$ROOTFS/nix/var/nix/profiles"

  # Minimal resolv.conf for network access
  echo "nameserver 1.1.1.1" > "$ROOTFS/etc/resolv.conf"

  echo "=== Taking @base snapshot ==="
  zfs snapshot "${DATASET}@base"

  echo "=== Warming nix store eval cache ==="
  # Install dev-tools to cache the flake evaluation, then remove so
  # @clean starts without dev-tools in the profile.
  nspawn_exec "nix profile install /workspace#dev-tools 2>&1 && nix profile remove dev-tools 2>&1" || true

  echo "=== Taking @clean snapshot ==="
  zfs snapshot "${DATASET}@clean"

  echo ""
  echo "Setup complete. Snapshots:"
  zfs list -t snapshot -r "$DATASET" -o name,used
  echo ""
  echo "Next: $0 shell   OR   $0 run all"
}

cmd_destroy() {
  echo "=== Destroying testbed ==="
  if pool_exists; then
    # Unmount and destroy
    zfs unmount -a -f 2>/dev/null || true
    zpool destroy -f "$POOL_NAME"
  fi
  rm -f "$IMG_PATH"
  rmdir "$IMG_DIR" 2>/dev/null || true
  echo "Done."
}

cmd_shell() {
  require_pool
  if snap_exists clean; then
    echo "Rolling back to @clean..."
    zfs rollback -r "${DATASET}@clean"
  fi
  echo "Launching interactive shell (exit to return)..."
  echo ""
  nspawn_exec --interactive
}

cmd_snapshot() {
  local name="${1:?Usage: $0 snapshot <name>}"
  require_pool
  zfs snapshot "${DATASET}@${name}"
  echo "Created ${DATASET}@${name}"
}

cmd_rollback() {
  local name="${1:-clean}"
  require_pool
  require_snap "$name"
  zfs rollback -r "${DATASET}@${name}"
  echo "Rolled back to ${DATASET}@${name}"
}

cmd_status() {
  if pool_exists; then
    echo "Pool:"
    zpool list "$POOL_NAME"
    echo ""
    echo "Dataset:"
    zfs list "$DATASET"
    echo ""
    echo "Snapshots:"
    zfs list -t snapshot -r "$DATASET" -o name,creation,used 2>/dev/null || echo "  (none)"
  else
    echo "No testbed pool exists."
  fi
}

cmd_run() {
  local target="${1:-all}"
  require_pool
  require_snap "clean"
  [[ -x "${REPO_ROOT}/bmci" ]] || die "bmci binary not found. Run: make build"

  local scenarios_dir="${SCRIPT_DIR}/scenarios"
  local files=()

  if [[ "$target" == "all" ]]; then
    mapfile -t files < <(find "$scenarios_dir" -name '*.sh' -type f | sort)
  else
    local pattern
    pattern=$(printf "%02d" "$target")
    mapfile -t files < <(find "$scenarios_dir" -name "${pattern}-*.sh" -type f)
  fi

  if [[ ${#files[@]} -eq 0 ]]; then
    die "No scenario files found for '$target'"
  fi

  local pass=0 fail=0 total=0

  for scenario_file in "${files[@]}"; do
    total=$((total + 1))
    local sname
    sname=$(basename "$scenario_file" .sh)

    # Source scenario to get functions
    unset -f scenario_name scenario_setup scenario_setup_root scenario_test scenario_verify 2>/dev/null || true
    source "$scenario_file"

    local display_name
    display_name=$(scenario_name 2>/dev/null || echo "$sname")

    echo "=== [$sname] $display_name ==="

    # 1. Rollback to clean
    zfs rollback -r "${DATASET}@clean"

    # 2. Run setup: user-level first, then root-level for privileged ops
    local setup_cmd
    setup_cmd=$(declare -f scenario_setup 2>/dev/null)
    if [[ -n "$setup_cmd" ]]; then
      echo "  [setup]"
      nspawn_exec "${setup_cmd}; scenario_setup" 2>&1 | sed 's/^/    /' || true
    fi

    local setup_root_cmd
    setup_root_cmd=$(declare -f scenario_setup_root 2>/dev/null) || true
    if [[ -n "$setup_root_cmd" ]]; then
      echo "  [setup-root]"
      nspawn_exec --root "${setup_root_cmd}; scenario_setup_root" 2>&1 | sed 's/^/    /' || true
    fi

    # 3. Run test inside container
    echo "  [test]"
    local test_cmd output exit_code
    test_cmd=$(declare -f scenario_test)
    output=$(nspawn_exec "${test_cmd}; scenario_test" 2>&1) && exit_code=$? || exit_code=$?
    echo "$output" | sed 's/^/    /'

    # 4. Verify
    if scenario_verify "$output" "$exit_code"; then
      echo "  [PASS]"
      pass=$((pass + 1))
    else
      echo "  [FAIL]"
      fail=$((fail + 1))
    fi

    echo ""
  done

  echo "=== Results: $pass/$total passed, $fail failed ==="
  [[ "$fail" -eq 0 ]]
}

# ── Main ──────────────────────────────────────────────────────────────

case "${1:-help}" in
  setup)    cmd_setup ;;
  destroy)  cmd_destroy ;;
  shell)    cmd_shell ;;
  run)      cmd_run "${2:-all}" ;;
  snapshot) cmd_snapshot "${2:-}" ;;
  rollback) cmd_rollback "${2:-clean}" ;;
  status)   cmd_status ;;
  *)
    echo "Usage: $0 <command>"
    echo ""
    echo "Commands:"
    echo "  setup       Create ZFS pool, container, and snapshots (~30s)"
    echo "  shell       Interactive shell in the container"
    echo "  run [N|all] Run scenario(s) with auto-rollback"
    echo "  snapshot N  Take a named snapshot"
    echo "  rollback N  Roll back to a snapshot (default: clean)"
    echo "  status      Show pool/dataset/snapshot info"
    echo "  destroy     Tear down pool and remove image"
    ;;
esac
