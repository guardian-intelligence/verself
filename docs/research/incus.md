# Incus — System Container & VM Manager with ZFS Storage Driver

> Community fork of LXD. Manages system containers and VMs with deep ZFS integration,
> clustering via distributed SQLite (Cowsql), seccomp syscall interception, and OVN networking.
>
> Repo: [lxc/incus](https://github.com/lxc/incus)
> Commit: `a6386825a645`

The ZFS driver alone is ~4,400 lines across four files. The most interesting techniques are in
storage lifecycle management, but the seccomp interceptor and clustering model are also relevant
to forge-metal's architecture.

---

## The "ghost graveyard" — soft-delete via `deleted/` namespace

ZFS cannot destroy a snapshot that has active clones. Instead of failing, Incus renames
undeletable datasets into a parallel `deleted/` namespace tree (`pool/deleted/containers/`,
`pool/deleted/images/`, etc.) where they sit as "ghosts" until their dependents are destroyed.

When a clone is eventually deleted, `deleteDatasetRecursive()` walks back up the origin chain
and garbage-collects any orphaned ancestors in the `deleted/` tree — recursively.

Three moving parts:

**Soft-delete via rename**: undeletable volumes get a random UUID name in the `deleted/` tree
to prevent naming collisions if the same volume name is recreated.

```go
if len(clones) > 0 {
    _, err := subprocess.RunCommand("/proc/self/exe", "forkzfs", "--", "rename",
        d.dataset(vol, false), d.dataset(vol, true))
}
```

**Recursive GC**: after destroying a dataset, check if the origin lives in `deleted/` or is a
`@deleted-*` / `@copy-*` snapshot. If so, and no more clones reference it, delete the origin
too — recursively through multiple generations.

```go
func (d *zfs) deleteDatasetRecursive(dataset string) error {
    origin, err := d.getDatasetProperty(dataset, "origin")
    // ... delete the dataset ...
    if strings.HasPrefix(origin, filepath.Join(d.config["zfs.pool_name"], "deleted")) {
        dataset = strings.SplitN(origin, "@", 2)[0]
    } else if strings.Contains(origin, "@deleted-") || strings.Contains(origin, "@copy-") {
        dataset = origin
    } else {
        dataset = "" // Origin is still active, leave it alone
    }
    if dataset != "" {
        clones, _ := d.getClones(dataset)
        if len(clones) == 0 {
            d.deleteDatasetRecursive(dataset) // Recurse!
        }
    }
}
```

**Snapshot soft-delete**: when `zfs destroy -r` fails, check if clones are the reason. If so,
rename the snapshot to `@deleted-<uuid>` on the parent dataset, deferring deletion.

- [`driver_zfs_utils.go:deleteDatasetRecursive`](https://github.com/lxc/incus/blob/a6386825a645/internal/server/storage/drivers/driver_zfs_utils.go) — recursive GC
- [`driver_zfs_volumes.go:deleteVolume`](https://github.com/lxc/incus/blob/a6386825a645/internal/server/storage/drivers/driver_zfs_volumes.go) — soft-delete via rename
- [`driver_zfs_volumes.go:DeleteVolumeSnapshot`](https://github.com/lxc/incus/blob/a6386825a645/internal/server/storage/drivers/driver_zfs_volumes.go) — snapshot soft-delete

**Applicability to forge-metal**: This is a fourth solution to the "update golden image while
clones exist" problem — alongside OBuilder's promotion dance, DBLab's dual-pool rotation, and
Velo's clone-then-swap. The ghost graveyard is the most automated: no manual tracking, no
promotion complexity. The tradeoff is disk space: orphaned datasets linger until the last
dependent clone dies.

## Image resurrection from the graveyard

When creating an image volume, Incus first checks if a previously deleted copy exists in the
`deleted/` namespace. If found with matching size, it renames it back — instant "creation"
with zero I/O.

```go
if volSizeBytes != poolVolSizeBytes {
    // Can't restore — sizes don't match. Rename to random UUID
    // so it can never be restored.
    randomVol := NewVolume(d, d.name, vol.volType, vol.contentType,
        d.randomVolumeName(vol), vol.config, vol.poolConfig)
    subprocess.RunCommand("/proc/self/exe", "forkzfs", "--", "rename",
        d.dataset(vol, true), d.dataset(randomVol, true))
    canRestore = false
}
```

When the pool's `volume.size` has changed since the image was cached, the stale image gets
renamed to a random UUID (making it unreachable and eventually garbage-collected).

- [`driver_zfs_volumes.go:CreateVolume`](https://github.com/lxc/incus/blob/a6386825a645/internal/server/storage/drivers/driver_zfs_volumes.go) — resurrection check near top of function

**Applicability to forge-metal**: Golden images that are "replaced" could be resurrected if the
new golden image build fails and a rollback is needed. The size-guard pattern is important —
never silently restore a stale cache whose parameters have changed.

## `forkzfs` — mount namespace isolation for ZFS commands

Some ZFS operations (particularly renames involving the `deleted/` namespace) are executed not
via `zfs` directly, but via `/proc/self/exe forkzfs -- <command>`. This re-executes the Incus
daemon binary itself with a special subcommand that:

1. Creates an isolated mount namespace
2. Marks the entire mount tree as `MS_PRIVATE`
3. **Unmounts everything** under the Incus data directory
4. Runs the ZFS command in this clean namespace

```go
func (c *cmdForkZFS) run(cmd *cobra.Command, args []string) error {
    err := unix.Mount("none", "/", "", unix.MS_REC|unix.MS_PRIVATE, "")

    file, _ := os.Open("/proc/self/mountinfo")
    scanner := bufio.NewScanner(file)
    for scanner.Scan() {
        rows := strings.Fields(scanner.Text())
        if strings.HasPrefix(rows[4], expPath) {
            _ = unix.Unmount(rows[4], unix.MNT_DETACH)
        }
    }

    command := exec.Command("zfs", args...)
    return command.Run()
}
```

This solves a subtle problem: ZFS `rename` on datasets with `mountpoint=legacy` can fail when
the mount tree is polluted by bind mounts, overlay mounts, or stale entries from running
containers. By executing in a clean namespace, ZFS sees a pristine view.

- [`cmd/incusd/main_forkzfs.go`](https://github.com/lxc/incus/blob/a6386825a645/cmd/incusd/main_forkzfs.go)

**Applicability to forge-metal**: If the agent process manages ZFS while gVisor containers have
bind mounts into cloned datasets, the same mount-tree pollution problem will occur. The
`forkzfs` pattern — re-exec self with `CLONE_NEWNS` — is the cleanest fix.

## BLKZNAME ioctl — device discovery without udev

To find the `/dev/zd*` device path for a ZFS volume, Incus scans all `/dev/zd*` devices and
uses a raw `ioctl(BLKZNAME)` system call on each one to ask the kernel which dataset it
belongs to.

```go
func (d *zfs) getVolumeDiskPathFromDataset(dataset string) (string, error) {
    entries, _ := os.ReadDir("/dev/")
    // Filter to zd* entries, skip partitions (containing "p")
    // Sort by reverse creation date (newest first)

    zfsDataset := func(devPath string) string {
        r, _ := os.OpenFile(devPath, unix.O_RDONLY|unix.O_CLOEXEC, 0)
        defer r.Close()
        buf := [256]byte{}
        unix.Syscall(unix.SYS_IOCTL, uintptr(r.Fd()), linux.IoctlBlkZname,
            uintptr(unsafe.Pointer(&buf)))
        return string(bytes.Trim(buf[:], "\x00"))
    }

    for _, entry := range zfsEntries {
        if zfsDataset("/dev/" + entry.Name()) == dataset {
            return "/dev/" + entry.Name(), nil
        }
    }
}
```

Most ZFS tools rely on `/dev/zvol/pool/dataset` symlinks created by udev. Incus skips this
because udev symlinks can be stale, missing, or delayed. Sorted by creation time (newest first)
to find recently created volumes faster.

- [`driver_zfs_volumes.go:getVolumeDiskPathFromDataset`](https://github.com/lxc/incus/blob/a6386825a645/internal/server/storage/drivers/driver_zfs_volumes.go) — ioctl discovery
- [`driver_zfs_volumes.go:tryGetVolumeDiskPathFromDataset`](https://github.com/lxc/incus/blob/a6386825a645/internal/server/storage/drivers/driver_zfs_volumes.go) — retry loop (500ms intervals, 30s timeout)

**Applicability to forge-metal**: If forge-metal ever uses zvols (block devices) instead of
datasets, never trust udev symlinks. Query the kernel directly.

## zvol activation/deactivation with exponential backoff

Block volumes are kept invisible (`volmode=none`) when not in use and made visible
(`volmode=dev`) only when mounted. Deactivation retries with increasing delays because ZFS and
udev are asynchronous — setting `volmode=none` does not immediately make the device disappear.

```go
func (d *zfs) deactivateVolume(vol Volume) (bool, error) {
    waitDuration := time.Minute * 5
    waitUntil := time.Now().Add(waitDuration)
    i := 0
    for {
        err = d.setDatasetProperties(dataset, "volmode=none")
        if !util.PathExists(devPath) {
            break
        }
        if time.Now().After(waitUntil) {
            return false, fmt.Errorf("Failed to deactivate zvol after %v", waitDuration)
        }
        if i <= 5 {
            time.Sleep(time.Second * time.Duration(i))
        } else {
            time.Sleep(time.Second * time.Duration(5))
        }
        i++
    }
}
```

Sometimes ZFS needs multiple pushes. The comment: "Sometimes it takes multiple attempts for ZFS
to actually apply this."

- [`driver_zfs_volumes.go:activateVolume`](https://github.com/lxc/incus/blob/a6386825a645/internal/server/storage/drivers/driver_zfs_volumes.go) — activation polling (30s timeout)
- [`driver_zfs_volumes.go:deactivateVolume`](https://github.com/lxc/incus/blob/a6386825a645/internal/server/storage/drivers/driver_zfs_volumes.go) — deactivation backoff (5min timeout)

**Applicability to forge-metal**: Any ZFS property change that involves udev needs a polling
loop, not a single check. OBuilder has a similar mystery with `fuser` retries — this is
a recurring theme across ZFS codebases.

## ZFS delegation via `zoned` property toggle

Incus delegates ZFS datasets to unprivileged containers using ZFS 2.2+'s `zoned` property
and `zfs zone` command. But there's a choreography requirement: once `zoned=on`, the host
can no longer modify mountpoint or other properties. So Incus toggles it:

- On mount: `zoned=on` (hand off to container's user namespace)
- On unmount: `zoned=off` (reclaim host control)
- After migration receive: `zoned=off` (before updating mountpoint)

```go
func (d *zfs) delegateDataset(vol Volume, pid int) error {
    _, err := subprocess.RunCommand("zfs", "zone",
        fmt.Sprintf("/proc/%d/ns/user", pid), d.dataset(vol, false))
}
```

- [`driver_zfs_utils.go:delegateDataset`](https://github.com/lxc/incus/blob/a6386825a645/internal/server/storage/drivers/driver_zfs_utils.go) — `zfs zone` call
- [`driver_zfs_volumes.go:MountVolume`](https://github.com/lxc/incus/blob/a6386825a645/internal/server/storage/drivers/driver_zfs_volumes.go) — `zoned=on`
- [`driver_zfs_volumes.go:UnmountVolume`](https://github.com/lxc/incus/blob/a6386825a645/internal/server/storage/drivers/driver_zfs_volumes.go) — `zoned=off`

**Applicability to forge-metal**: If CI jobs ever need direct ZFS access inside their sandbox
(e.g., for build caching), delegation via `zfs zone` is the proper kernel-supported mechanism.
But the toggle dance is mandatory — you can't manage the dataset from the host while it's
zoned.

## Filesystem freeze for block-backed snapshot consistency

Before snapshotting block-backed filesystem volumes (zvols with ext4/xfs on top), Incus calls
`sync` then `fsfreeze --freeze`. This is **only** needed for block-backed volumes — native ZFS
datasets don't need it because ZFS's own snapshot is already atomic at the dataset level.

```go
func (d *common) filesystemFreeze(path string) (func() error, error) {
    err := linux.SyncFS(path)
    subprocess.RunCommand("fsfreeze", "--freeze", path)
    unfreezeFS := func() error {
        subprocess.RunCommand("fsfreeze", "--unfreeze", path)
        return nil
    }
    return unfreezeFS, nil
}
```

The code explicitly notes that "only calling `os.SyncFS()` doesn't suffice" — you need the
FIFREEZE ioctl to prevent new writes during the snapshot window.

- [`driver_common.go:filesystemFreeze`](https://github.com/lxc/incus/blob/a6386825a645/internal/server/storage/drivers/driver_common.go)

**Applicability to forge-metal**: Compare with Velo's `CHECKPOINT` approach for Postgres. The
pattern is the same: flush dirty buffers, prevent new writes, snapshot, unfreeze. For
forge-metal's ZFS datasets (not zvols), this is unnecessary — but worth knowing if the
architecture ever changes to zvol-backed containers.

## GUID-based migration protocol

Before transferring data, source and target exchange JSON headers containing snapshot names and
ZFS GUIDs. The target compares GUIDs to determine which snapshots it already has, then tells
the source to only send the missing ones.

```go
type ZFSMetaDataHeader struct {
    SnapshotDatasets []ZFSDataset `json:"snapshot_datasets"`
}
type ZFSDataset struct {
    Name string `json:"name"`
    GUID string `json:"guid"`
}
```

The bidirectional exchange:
1. Source sends its header (snapshot names + GUIDs)
2. Target responds with its header
3. Source sends only snapshots whose GUIDs the target doesn't have
4. Falls back to full generic copy if GUIDs diverge at the first snapshot

If `zfs receive` fails with "destination has snapshots" (diverged history), all target
snapshots are deleted and a full send is attempted.

- [`driver_zfs_volumes.go:MigrateVolume`](https://github.com/lxc/incus/blob/a6386825a645/internal/server/storage/drivers/driver_zfs_volumes.go) — migration send path
- [`driver_zfs_utils.go:datasetHeader`](https://github.com/lxc/incus/blob/a6386825a645/internal/server/storage/drivers/driver_zfs_utils.go) — header construction

**Applicability to forge-metal**: When distributing golden images to remote nodes via
`zfs send`, a GUID-based protocol avoids re-sending snapshots the target already has. This is
exactly the incremental distribution mechanism forge-metal needs for multi-node golden image
sync.

## `zfs.clone_copy=rebase` — origin chain walking

When copying a volume, the `rebase` mode walks the entire origin chain to find the original
image snapshot (`@readonly` under an `/images/` path), then does an incremental send from that
base. Copies of copies of copies all send only the delta from the original image.

```go
if d.config["zfs.clone_copy"] == "rebase" {
    origin := d.dataset(srcVol, false)
    for {
        fields := strings.SplitN(origin, "@", 2)
        if len(fields) > 1 && strings.Contains(fields[0], "/images/") &&
            fields[1] == "readonly" {
            break
        }
        origin, _ = d.getDatasetProperty(origin, "origin")
        if origin == "" || origin == "-" {
            origin = ""
            break
        }
    }
    if origin != "" && origin != srcSnapshot {
        args = append(args, "-i", origin) // Incremental from image base
    }
}
```

- [`driver_zfs_volumes.go:CreateVolumeFromCopy`](https://github.com/lxc/incus/blob/a6386825a645/internal/server/storage/drivers/driver_zfs_volumes.go) — rebase logic

**Applicability to forge-metal**: When CI jobs create derivative snapshots (warm caches, built
artifacts), rebasing against the golden image means transfers only carry the job-specific delta.
Significant bandwidth savings for multi-node deployments.

## Snapshot UUID regeneration at mount time

When mounting a block-backed snapshot, the snapshot has the same filesystem UUID as the parent.
Rather than regenerating at snapshot time (expensive, modifies the snapshot), Incus does it at
mount time — and uses different strategies per filesystem:

- **XFS**: `nouuid` mount option (XFS UUID regeneration is slow)
- **ext4**: `noload` mount option to prevent journal replay on read-only mounts
- **Both**: Create a temporary writable clone just for UUID regeneration, since snapshots are immutable

```go
if regenerateFSUUID {
    subprocess.RunCommand("zfs", "clone", snapshotDataset, dataset)
    d.setDatasetProperties(dataset, "volmode=dev")
    time.Sleep(500 * time.Millisecond) // Wait for udev

    if tmpVolFsType == "xfs" {
        mountOptions += ",nouuid"
    } else {
        regenerateFilesystemUUID(mountVol.ConfigBlockFilesystem(), volPath)
    }
}
```

- [`driver_zfs_volumes.go:mountVolumeSnapshot`](https://github.com/lxc/incus/blob/a6386825a645/internal/server/storage/drivers/driver_zfs_volumes.go) — UUID regeneration logic

## Seccomp syscall interceptor (most novel security feature)

Incus implements a syscall interception proxy using Linux's `SECCOMP_RET_USER_NOTIF`. BPF
filters in the container trigger notifications for specific syscalls. The Incus daemon validates
and performs the operation on behalf of the container, then returns the result.

| Syscall | What it does |
|---------|-------------|
| `mknod`/`mknodat` | Validates against device whitelist, bind-mounts real device node. Unprivileged containers can "create" device nodes without `CAP_MKNOD` |
| `mount` | Validates against `security.syscalls.intercept.mount.allowed` whitelist. Handles FUSE interception and idmap shifting |
| `setxattr` | Permits `trusted.overlay.opaque=y` for overlay whiteout operations. Maps container UIDs/GIDs to host equivalents |
| `sched_setscheduler` | Only permits changes from user namespace root |
| **`sysinfo`** | **Returns container-specific metrics instead of host values** — collects uptime, process count, memory/swap from cgroups |
| `bpf` | Intercepts `BPF_PROG_LOAD/ATTACH/DETACH`. Only allows `BPF_PROG_TYPE_CGROUP_DEVICE` |

The `sysinfo` interception is the cleverest: when a process inside a container calls `sysinfo()`,
it gets synthetic values from the container's cgroup limits instead of host values. Tools like
`free` and `htop` report accurate container-scoped values without guest modifications.

- [`internal/server/seccomp/seccomp.go`](https://github.com/lxc/incus/blob/a6386825a645/internal/server/seccomp/seccomp.go)

**Applicability to forge-metal**: The `sysinfo` interception pattern could make CI jobs report
accurate resource usage even inside gVisor sandboxes. The `mount` interception could allow
controlled FUSE mounts (e.g., for npm cache overlay) without granting `CAP_SYS_ADMIN`.

## Cowsql — distributed SQLite via Raft

Incus stores all cluster state in Cowsql, a fork of Canonical's dqlite — distributed SQLite
replicated via Raft consensus. Write transactions are replicated to a quorum of voters before
acknowledgement. Reads hit the local in-memory state without replication.

Key design:
- SQLite runs on a custom VFS that stores files **in process memory**, not disk
- Modified WAL pages are collected into Raft log entries
- At most one write transaction executes at a time (exclusive lock)
- Checkpoints can run independently on any node
- 3 voters by default, with 2 automatic standbys for promotion

Schema is at version 77 with 50+ tables, managed via numbered migration functions
(`updateFromV0` through `updateFromV76`).

- [`internal/server/db/`](https://github.com/lxc/incus/blob/a6386825a645/internal/server/db/) — database layer
- [Cowsql](https://github.com/cowsql/cowsql) — the consensus library

**Applicability to forge-metal**: If forge-metal's controller needs distributed state across
multiple nodes, embedded Cowsql eliminates the external database dependency. Single-binary
deployment with built-in consensus. But for the current single-node architecture, SQLite
without Raft is sufficient.

## Dataset naming encodes type, content, lifecycle, and filesystem

A single `dataset()` function generates ZFS paths that encode volume type, content type,
filesystem type, and lifecycle state:

- **Block-backed filesystem images** include the FS type: `pool/images/abc123_ext4`
  (switching `block.filesystem` creates a separate cached image)
- **VM/image block volumes** append `.block`: `pool/virtual-machines/myvm.block`
- **ISO volumes** append `.iso`: `pool/custom/data.iso`
- **Deleted volumes** move to `pool/deleted/<type>/<uuid>` (UUID prevents collisions)
  except images which keep their fingerprint name (for resurrection)
- **Snapshot prefixes**: `@snapshot-<name>`, `@deleted-<uuid>`, `@copy-<uuid>`,
  `@readonly`, `@migration-<uuid>`, `@backup-<uuid>`

- [`driver_zfs_utils.go:dataset`](https://github.com/lxc/incus/blob/a6386825a645/internal/server/storage/drivers/driver_zfs_utils.go)

**Applicability to forge-metal**: forge-metal's dataset naming should encode enough state to
enable garbage collection without a database. Incus's approach — embed lifecycle state in the
path, use `@readonly` as a completion marker (similar to OBuilder's `@snap`) — is the most
battle-tested pattern.

## Custom ZFS properties for metadata

Incus stores metadata as `incus:content_type` on custom volumes and `incusos:use=incus` on
IncusOS datasets. ZFS user properties survive send/receive, snapshots, and clones.

```go
cmd := exec.Command("zfs", "list", "-H", "-o", "name,type,incus:content_type",
    "-r", "-t", "filesystem,volume", d.config["zfs.pool_name"])
```

- [`driver_zfs_volumes.go:ListVolumes`](https://github.com/lxc/incus/blob/a6386825a645/internal/server/storage/drivers/driver_zfs_volumes.go) — property-based listing

**Applicability to forge-metal**: Custom ZFS properties like `forge:job_id`, `forge:golden_hash`,
`forge:created_at` would survive `zfs send/receive` to remote nodes. Better than a sidecar
database for metadata that must travel with the dataset. Compare with DBLab's `dle:*` properties.

## Loop-backed pool sync bypass

When creating zvols on a loop-file-backed pool (dev/test environments), Incus sets
`sync=disabled` to avoid kernel lockups from double-buffering.

```go
loopPath := loopFilePath(d.name)
if d.config["source"] == loopPath {
    opts = append(opts, "sync=disabled")
}
```

- [`driver_zfs_volumes.go:CreateVolume`](https://github.com/lxc/incus/blob/a6386825a645/internal/server/storage/drivers/driver_zfs_volumes.go) — sync bypass

**Applicability to forge-metal**: The ZFS testbed (`tests/testbed/testbed.sh`) uses a
file-backed pool. Setting `sync=disabled` on it would speed up test scenarios.

## Streaming `zfs list` output parsing

`ListVolumes` uses `bufio.Scanner` on `cmd.StdoutPipe()` instead of `cmd.Output()`, parsing
ZFS output line-by-line as it arrives. This handles pools with thousands of datasets without
buffering the entire output in memory.

- [`driver_zfs_volumes.go:ListVolumes`](https://github.com/lxc/incus/blob/a6386825a645/internal/server/storage/drivers/driver_zfs_volumes.go) — streaming parser

## Conditional `-R` flag to avoid encryption interference

`needsRecursion()` checks if a dataset actually has child datasets before using `zfs send -R`
(recursive replication). The raw mode flag `-w` is only added when `-R` is used:

```go
func (d *zfs) needsRecursion(dataset string) bool {
    dataset = strings.Split(dataset, "@")[0]
    entries, err := d.getDatasets(dataset, "filesystem,volume")
    return len(entries) > 0
}
```

Comment: "We only want to use recursion (and possible raw) mode if required as it can interfere
with ZFS encryption."

- [`driver_zfs_utils.go:needsRecursion`](https://github.com/lxc/incus/blob/a6386825a645/internal/server/storage/drivers/driver_zfs_utils.go)
- [`driver_zfs_utils.go:sendDataset`](https://github.com/lxc/incus/blob/a6386825a645/internal/server/storage/drivers/driver_zfs_utils.go)

## Auto-restart throttling

`shouldAutoRestart()` maintains a per-instance 10-slot circular buffer of timestamps, allowing
at most one restart per minute. Prevents restart storms from crashing the host.

**Applicability to forge-metal**: If a CI job keeps crashing and the agent keeps re-cloning
and retrying, a similar throttle prevents the agent from consuming all ZFS capacity with
failed clone debris.

## Fork subprocess pattern

Incus re-executes itself (`/proc/self/exe`) with special subcommands for privileged operations
that need different namespace/capability contexts:

| Subcommand | Purpose |
|-----------|---------|
| `forkstart` | Launch container with LXC config |
| `forklimits` | Selectively raise ulimits (PCI passthrough MEMLOCK) |
| `forknet` | Enter container network namespace for DHCP/interface management |
| `forkzfs` | ZFS operations in clean mount namespace |
| `forkmknod` | Create device nodes with proper credentials |

This is privilege separation without a separate binary. The main daemon process can run with
reduced capabilities while fork subcommands get exactly the privileges they need.

**Applicability to forge-metal**: The `bmci` agent could use the same pattern — `bmci forkzfs`
for ZFS operations that need a clean mount namespace, `bmci forksandbox` for gVisor setup that
needs specific capabilities.

---

## Cross-cutting observations

**Incus confirms every pattern from the previous research**:

1. **Shells out to `zfs` CLI** — 4,400 lines of subprocess calls, no libzfs.
2. **Invents its own metadata layer** — custom ZFS properties (`incus:content_type`), dataset
   naming conventions that encode lifecycle state, plus a full relational database (Cowsql)
   for everything else.
3. **Has its own solution to "delete snapshot with dependents"** — the ghost graveyard
   (`deleted/` namespace + recursive GC). This is the fourth distinct solution after OBuilder's
   promotion dance, DBLab's dual-pool rotation, and Velo's clone-then-swap.

**New patterns not seen in previous research**:

| Pattern | Where | Why it matters |
|---------|-------|---------------|
| Mount namespace isolation for ZFS ops | `forkzfs` | Running containers pollute the mount tree, breaking `zfs rename` |
| `BLKZNAME` ioctl for device discovery | `getVolumeDiskPathFromDataset` | udev symlinks are unreliable; query the kernel directly |
| Exponential backoff for property changes | `deactivateVolume` | ZFS property changes are async w.r.t. udev |
| GUID-based incremental migration | `MigrateVolume` | Exchange snapshot GUIDs before transfer, send only the delta |
| Origin chain walking for rebase | `CreateVolumeFromCopy` | Copies of copies still send incremental from the original image |
| Seccomp `sysinfo` interception | `seccomp.go` | Synthetic resource metrics inside containers |
| `zoned` property toggle dance | `delegateDataset` | ZFS 2.2+ dataset delegation to user namespaces |
| Self-re-exec for privilege separation | `forkzfs`, `forknet`, etc. | Single binary, multiple privilege contexts |
