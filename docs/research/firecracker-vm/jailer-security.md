# Firecracker Jailer & Security Model -- Deep Dive

> Primary source: Firecracker source code (Rust), commit tree as of 2026-03-29.
> Repo: [firecracker-microvm/firecracker](https://github.com/firecracker-microvm/firecracker)
> Key source files: `src/jailer/src/{env.rs,chroot.rs,cgroup.rs,main.rs,resource_limits.rs}`
> Seccomp filters: `resources/seccomp/x86_64-unknown-linux-musl.json`
> Documentation: `docs/{jailer.md,seccomp.md,design.md,prod-host-setup.md}`

## Threat model

Firecracker's threat model is stated directly in `docs/design.md`:

> From a security perspective, all vCPU threads are considered to be running
> malicious code as soon as they have been started; these malicious threads need
> to be contained.

Containment is achieved by **nesting trust zones** from least trusted (guest vCPU)
to most trusted (host). Barriers between zones enforce isolation:

1. **KVM hardware virtualization** -- first boundary, hardware-enforced
2. **Seccomp BPF filters** -- per-thread syscall allowlists (loaded before guest code runs)
3. **Jailer** -- cgroups, namespaces, chroot/pivot_root, UID/GID drop
4. **Minimal device model** -- only 4 emulated devices (virtio-block, virtio-net, serial, i8042)

What is **trusted**: the host kernel, the host/API communication channel, and snapshot files.
What is **untrusted**: everything running inside the guest, including all vCPU execution.

Source: [`docs/design.md` lines 80-94](https://github.com/firecracker-microvm/firecracker/blob/main/docs/design.md)

## Jailer architecture -- exact steps

The jailer (`src/jailer/`) is a separate binary that runs as root, sets up the sandbox,
drops privileges, and `exec()`s into the Firecracker VMM binary. After the `exec()`, the
Firecracker process runs as an unprivileged user and can only access resources that the
jailer or a privileged third party has placed inside the jail.

### Step-by-step execution order

Source: `src/jailer/src/main.rs` `main_exec()` -> `Env::new()` -> `Env::run()`

**Phase 1: Process sanitization** (`main.rs:279-287`)

1. Close all inherited file descriptors except stdin/stdout/stderr using
   `close_range(3, UINT_MAX, CLOSE_RANGE_UNSHARE)` syscall (requires kernel >= 5.9)
2. Strip all environment variables -- iterates `env::vars()` and calls `env::remove_var()`
   for every key. Prevents information leakage from parent process.

**Phase 2: Argument parsing and environment setup** (`env.rs:137-274`)

3. Parse and validate the `--id` (alphanumeric + hyphens, max 64 chars)
4. Validate `--exec-file` path (must be an existing regular file, canonicalized)
5. Construct chroot directory path: `<chroot-base>/<exec-file-name>/<id>/root`
   (default chroot-base: `/srv/jailer`)
6. Parse `--uid` and `--gid` for privilege drop
7. Parse optional `--netns` (network namespace path)
8. Parse `--cgroup` arguments (format: `<cgroup_file>=<value>`)
9. Parse `--resource-limit` arguments (format: `<resource>=<value>`)
10. Detect userfaultfd device minor number from `/proc/misc`

**Phase 3: Jail construction** (`env.rs:641-772`)

11. **Copy exec binary** -- copies (not hardlinks) the Firecracker binary into
    `<chroot_dir>/firecracker`. Copying is deliberate: prevents two Firecracker
    processes from sharing the `.text` section via memory-mapped pages.
    (`env.rs:468-518`, comment: "hard-linking... is not desirable in Firecracker's
    threat model. Copying prevents 2 Firecracker processes from sharing memory.")
12. **Join network namespace** (if `--netns`): `setns(fd, CLONE_NEWNET)` (`env.rs:520-529`)
13. **Set resource limits** via `setrlimit()`: `RLIMIT_FSIZE` (file size) and
    `RLIMIT_NOFILE` (max open FDs, default 2048) (`resource_limits.rs:93-99`)
14. **Create cgroup hierarchy** (details below)
15. **Open `/dev/null`** before chrooting (needed for daemonization)

**Phase 4: Chroot with pivot_root** (`chroot.rs:19-101`)

16. `unshare(CLONE_NEWNS)` -- enter new mount namespace
17. `mount(NULL, "/", NULL, MS_SLAVE | MS_REC, NULL)` -- change all mount propagation
    to slave (host->jail propagation, no reverse)
18. `mount(<chroot_dir>, <chroot_dir>, NULL, MS_BIND | MS_REC, NULL)` -- bind mount
    the jail directory over itself (required by pivot_root: new root and old root
    must be on different filesystems)
19. `chdir(<chroot_dir>)` -- enter the jail directory
20. `mkdir("old_root", 0o600)` -- create mount point for old root
21. `pivot_root(".", "old_root")` -- swap filesystem roots
22. `chdir("/")` -- ensure we're at new root
23. `umount2("old_root", MNT_DETACH)` -- detach old filesystem hierarchy
24. `rmdir("old_root")` -- remove the mount point

**Phase 5: Device creation** (`env.rs:676-708`)

25. Create folder hierarchy: `/`, `/dev`, `/dev/net`, `/run` -- all with mode 0o700,
    owned by `<uid>:<gid>`
26. `mknod("/dev/net/tun", S_IFCHR | S_IRUSR | S_IWUSR, makedev(10, 200))` -- TAP device
27. `mknod("/dev/kvm", S_IFCHR | S_IRUSR | S_IWUSR, makedev(10, 232))` -- KVM device
28. `mknod("/dev/urandom", S_IFCHR | S_IRUSR | S_IWUSR, makedev(1, 9))` -- entropy source
29. If userfaultfd device exists: `mknod("/dev/userfaultfd", ...)` with detected minor number
30. `chown()` all devices to `<uid>:<gid>`

**Phase 6: Daemonization** (optional, `env.rs:713-763`)

31. Double-fork method: `fork()` -> parent exits -> child calls `setsid()` ->
    `fork()` again -> child exits -> grandchild is daemon
32. Redirect stdin/stdout/stderr to `/dev/null` via `dup2()`
33. Purpose: detach from controlling terminal, prevent SIGHUP on parent exit

**Phase 7: PID namespace and exec** (`env.rs:328-383, 765-771`)

34. If `--new-pid-ns`: `clone(NULL, CLONE_NEWPID)` -- child becomes PID 1 in new namespace
35. If the jailer was a session leader (and not daemonizing): child calls `setsid()` to
    become leader of new session, preventing SIGHUP delivery from jailer's exit
36. Parent writes child PID to `<exec_file>.pid` in jail root, then `exit(0)`
37. Drop privileges: `Command::new(chroot_exec_file).uid(uid).gid(gid).exec()`
    -- this is a Rust `exec()` that replaces the process image

### Namespaces used

| Namespace | How | Purpose |
|-----------|-----|---------|
| Mount (`CLONE_NEWNS`) | `unshare()` in step 16 | Isolate filesystem view, enable pivot_root |
| PID (`CLONE_NEWPID`) | `clone()` in step 34 (optional `--new-pid-ns`) | Firecracker becomes PID 1, cannot see host processes |
| Network (`CLONE_NEWNET`) | `setns()` in step 12 (optional `--netns`) | Join pre-created network namespace |

**NOT used**: user namespace (no UID/GID mapping -- jailer uses real UIDs), IPC namespace, UTS namespace.

## Cgroup setup

Source: `src/jailer/src/cgroup.rs`

The jailer supports both cgroupv1 and cgroupv2 (selected via `--cgroup-version`, default v1).

### Cgroupv1 flow

1. Parse `/proc/mounts` to discover controller mount points (regex matching `cgroup` vs `cgroup2`)
2. For each `--cgroup` argument, extract the controller name from the filename
   (e.g., `cpu.shares` -> controller `cpu`)
3. Find the controller's mount point from the parsed mounts
4. Create `<mount_point>/<parent_cgroup>/<id>/` directory
5. Write the jailer's PID to `tasks` (or `cgroup.procs` for v2)
6. Write each cgroup value to its corresponding file
7. Inherit `cpuset.cpus` and `cpuset.mems` from parent if creating cpuset cgroups

### Cgroupv2 flow

1. Find the unified hierarchy mount point (`cgroup2` in `/proc/mounts`)
2. Create `<unified_mount>/<parent_cgroup>/<id>/` directory
3. Write PID to `cgroup.procs`
4. If no `--cgroup` args and v2: just move process to `--parent-cgroup` if it exists

### Recommended cgroup constraints for CI

From `docs/prod-host-setup.md`:

- **CPU**: `cpuset.cpus` for core pinning, `cpu.cfs_quota_us`/`cpu.cfs_period_us` for CPU bandwidth
- **Memory**: `memory.limit_in_bytes` (hard), `memory.memsw.limit_in_bytes` (memory+swap),
  `memory.soft_limit_in_bytes` (flexible)
- **Block I/O**: `blkio.throttle.io_serviced`, `blkio.throttle.io_service_bytes`

## Seccomp filter details

Source: `resources/seccomp/x86_64-unknown-linux-musl.json`, `src/vmm/src/seccomp.rs`

### Architecture

Seccomp filters are per-thread, not per-process. Three separate filter sets:

| Thread | Unique syscalls | Total rules | Default action |
|--------|----------------|-------------|----------------|
| VMM (main) | 48 | 64 | `trap` (SIGSYS) |
| API | 31 | 36 | `trap` (SIGSYS) |
| vCPU | 24 | 46 | `trap` (SIGSYS) |

`default_action: trap` means any syscall NOT in the allowlist triggers SIGSYS, which
kills the process. This is the strictest mode -- not `log` or `errno`, but `trap`.

Filters are compiled from JSON into binary BPF at build time using `seccompiler-bin`,
then embedded directly into the Firecracker binary. At runtime, they're loaded via:

```
prctl(PR_SET_NO_NEW_PRIVS, 1)     // prevent privilege escalation
syscall(SYS_seccomp, SECCOMP_SET_MODE_FILTER, 0, &bpf_prog)
```

Source: `src/vmm/src/seccomp.rs:110-134`

### Filter loading timing

- **VMM thread**: filters installed right before executing guest code on vCPU threads
- **API thread**: filters installed right before launching the HTTP server
- **vCPU threads**: filters installed right before executing guest code

This means the Firecracker API server starts with full syscall access, then locks down.

### What's allowed (x86_64 musl target)

**48 total unique syscalls** across all threads:

```
accept4, brk, clock_gettime, close, connect, epoll_ctl, epoll_pwait,
eventfd2, exit, exit_group, fcntl, fstat, fsync, ftruncate, futex,
getrandom, gettid, io_uring_enter, io_uring_register, io_uring_setup,
ioctl, lseek, madvise, mincore, mmap, mprotect, mremap, msync, munmap,
open, read, readv, recvfrom, recvmsg, restart_syscall, rt_sigaction,
rt_sigprocmask, rt_sigreturn, sched_yield, sendmsg, sendto, sigaltstack,
socket, stat, timerfd_settime, tkill, write, writev
```

### What's NOT allowed (notable exclusions)

- `fork`, `clone`, `execve` -- cannot spawn processes
- `mount`, `umount`, `pivot_root`, `chroot` -- cannot modify mount namespace
- `setuid`, `setgid`, `setns`, `unshare` -- cannot change identity or namespaces
- `ptrace` -- cannot trace other processes
- `kill` -- cannot signal other processes (only `tkill` for self/threads with SIGABRT)
- `chmod`, `chown`, `chdir` -- cannot change file metadata
- `mkdir`, `rmdir`, `unlink`, `rename` -- cannot modify directory structure
- `listen`, `bind` -- cannot create listening sockets (only `accept4` on existing ones)
- Any `*at` variants (`openat`, `mkdirat`, etc.)

### Argument-level filtering

Many allowed syscalls have argument constraints:

- `ioctl` -- only specific KVM ioctls and a few terminal/network ioctls:
  - VMM: `FIONBIO`, `TIOCGWINSZ`, `TCGETS`, `TCSETS`, plus 5 KVM ioctls
    (`GET_DIRTY_LOG`, `GET_IRQCHIP`, `GET_CLOCK`, `GET_PIT2`, `SET_USER_MEMORY_REGION`)
  - vCPU: `KVM_RUN` + 14 KVM state getters + `TUNSETOFFLOAD` + `KVM_KVMCLOCK_CTRL`
    + `KVM_CHECK_EXTENSION(KVM_CAP_MSI_DEVID)` + `KVM_SET_GSI_ROUTING` + `KVM_IRQFD`
- `mmap` -- only specific flag combinations (`MAP_SHARED`, `MAP_ANONYMOUS|MAP_PRIVATE`,
  `MAP_FIXED|MAP_ANONYMOUS|MAP_PRIVATE`, `MAP_SHARED|MAP_POPULATE`)
- `socket` -- only `AF_UNIX, SOCK_STREAM|SOCK_CLOEXEC, 0` (Unix domain sockets)
- `accept4` -- only with `SOCK_CLOEXEC`
- `futex` -- only `FUTEX_WAIT`, `FUTEX_WAKE`, `FUTEX_WAIT_PRIVATE`,
  `FUTEX_WAIT_BITSET_PRIVATE`, `FUTEX_WAKE_PRIVATE`
- `tkill` -- only `SIGABRT` (for panic handling)
- `rt_sigaction` -- only `SIGABRT`

### Custom filters

Users can override with `--seccomp-filter <path>` (compiled binary, not JSON).
Can also disable entirely with `--no-seccomp` (not recommended for production).

## Jailer + ZFS zvol: how to pass block devices into the jail

This is the critical integration point for forge-metal. Firecracker takes block devices
via the API's `path_on_host` field in the drive configuration. The path must be accessible
from inside the chroot jail.

### Three approaches for getting a zvol into the jail

**Approach 1: `mknod` the zvol device node inside the jail (recommended)**

The test framework (`tests/framework/jailer.py`) demonstrates this pattern. When the
resource is a block device, it uses `mknod()` with the source device's major/minor
numbers instead of copying or hardlinking:

```python
if file_path.is_block_device():
    perms = stat.S_IRUSR | stat.S_IWUSR
    os.mknod(jailed_path, stat.S_IFBLK | perms, os.makedev(major, minor))
    os.chown(jailed_path, uid, gid)
```

For ZFS zvols, the equivalent orchestrator step would be:

```bash
# After zfs clone, before starting jailer:
ZVOL_PATH="/dev/zvol/pool/ci/job-abc"
MAJOR=$(stat -c '%t' "$ZVOL_PATH")
MINOR=$(stat -c '%T' "$ZVOL_PATH")

JAIL_ROOT="/srv/jailer/firecracker/<id>/root"
mknod "${JAIL_ROOT}/rootfs" b 0x$MAJOR 0x$MINOR
chown <uid>:<gid> "${JAIL_ROOT}/rootfs"
```

Then configure the drive as `path_on_host: "/rootfs"` (relative to jail root).

**Approach 2: Bind mount the zvol device into the jail**

This was the subject of [Issue #1089](https://github.com/firecracker-microvm/firecracker/issues/1089).
The jailer's `pivot_root()` uses `MS_BIND | MS_REC` (since the fix in PR #1093), so
bind mounts now propagate correctly. The orchestrator would:

```bash
mount --bind /dev/zvol/pool/ci/job-abc ${JAIL_ROOT}/rootfs
```

**Approach 3: Symlink the zvol (will NOT work)**

The jailer opens files with `O_NOFOLLOW` when copying the exec binary. While drive
files are opened by Firecracker itself (not the jailer), relying on symlinks through
`/dev/zvol/` adds an unnecessary indirection. Use the direct `/dev/zd<N>` device
path instead, which is the actual block device node.

### Known issues and considerations

1. **ZFS zvol device paths**: `/dev/zvol/pool/ci/job-abc` is a symlink managed by
   udev to `/dev/zd<N>`. For `mknod`, use `stat` on the symlink target to get the
   real major/minor numbers. ZFS zvols use major 230 (Linux ZFS zvol driver).

2. **Jailer mount point scaling**: The jailer parses `/proc/mounts` to find cgroup
   hierarchies. With many ZFS datasets, `/proc/mounts` can be very large. From
   `docs/jailer.md`: "The time it takes to create a jail depends on the number of
   mount points in the system... 10x when 10 jails are created in parallel with 500
   mount points." ZFS datasets each add a mount point. Use `canmount=off` on
   non-filesystem datasets and minimize mounted datasets.

3. **Block device ownership**: The zvol device node inside the jail must be owned
   by the jailer's `--uid`/`--gid` with read+write permission. ZFS zvol device nodes
   on the host are typically owned by root. The orchestrator must create the mknod'd
   device with correct ownership.

4. **I/O scheduler for zvols**: ZFS zvols use the default block device I/O scheduler,
   which may not be optimal. Consider `none` (noop) scheduler since ZFS has its own
   I/O scheduler internally. See [openzfs/zfs#1017](https://github.com/openzfs/zfs/issues/1017).

5. **zvol blocksize vs Firecracker**: Firecracker's virtio-block device has no
   blocksize constraint -- it presents the block device as-is to the guest.
   **Use 16K volblocksize** (ZFS 2.2+ default). 4K is actively harmful (compression
   disabled, metadata overhead). See
   [capacity-and-operations.md](capacity-and-operations.md#zvol-volblocksize-use-16k).

6. **Read-only drives**: For the kernel image drive, use `is_read_only: true` in the
   drive config. This can be a shared file (hardlinked or copied once) rather than
   a per-job zvol clone.

## Production host recommendations relevant to CI

From `docs/prod-host-setup.md`:

1. **Disable SMT**: Prevents speculation side channels between tenants sharing a
   physical core. For single-tenant CI, less critical but still recommended.

2. **Disable swap**: Prevents guest memory contents from being written to disk.
   Also prevents ZFS ARC pressure from triggering swap.

3. **Serial console**: Disable with `8250.nr_uarts=0` boot arg in guest kernel.
   Without this, the guest can flood the host's stdout buffer.

4. **Kernel command line**: Add `quiet loglevel=1` to host kernel to minimize
   serial console overhead from TAP device creation (measured 3ms -> 8.5ms
   snapshot restore regression on aarch64 from console logging).

5. **kvm-pit thread**: After guest start, the kernel creates a `kvm-pit/<pid>`
   thread that belongs to the root cgroup. An external agent should move this
   thread into the microVM's cgroup to prevent CPU overhead.

6. **Overwatcher process**: Recommended to periodically check for unresponsive
   Firecracker processes (possible deadlock in signal handler) and SIGKILL them.

## Forge-metal integration pattern

For the forge-metal orchestrator, the recommended integration is:

```
1. zfs clone pool/golden-zvol@ready pool/ci/job-<id>           # ~1.7ms
2. stat /dev/zvol/pool/ci/job-<id> -> (major, minor)           # get device numbers
3. mkdir -p /srv/jailer/firecracker/<id>/root                   # create jail root
4. mknod /srv/jailer/firecracker/<id>/root/rootfs b M m         # create device node
5. chown <uid>:<gid> /srv/jailer/firecracker/<id>/root/rootfs   # set ownership
6. cp /path/to/vmlinux /srv/jailer/firecracker/<id>/root/kernel # copy kernel
7. jailer --id <id> --exec-file /usr/bin/firecracker            # launch jailed VM
       --uid <uid> --gid <gid>
       --cgroup cpuset.cpus=<core> --cgroup cpuset.mems=0
       --new-pid-ns --daemonize
       -- --config-file /vm-config.json
8. (job runs inside VM)
9. VM exits
10. zfs get written pool/ci/job-<id>                            # bytes dirtied
11. zfs destroy pool/ci/job-<id>                                # cleanup
12. rm -rf /srv/jailer/firecracker/<id>/                        # cleanup jail
```

The key insight is that the jailer itself never touches the zvol. The orchestrator
(running as root) creates the `mknod` device node before invoking the jailer.
The jailer creates the jail, drops privileges, and execs Firecracker. Firecracker
then opens the block device path (inside the jail) via its API configuration.

**Note:** The Go SDK's `NaiveChrootStrategy` hard-links drives into the chroot, which
fails for block devices. A custom `ChrootStrategy` using `mknod` is required. See
[go-sdk.md](go-sdk.md#zfs-zvol--jailer-the-hard-link-problem) for the implementation pattern.

For networking setup within the jailer's namespace, see
[networking.md](networking.md#jailer---netns-integration).

## Edge case: zvol destroyed while VM is running

If `zfs destroy` is called while Firecracker still holds the zvol's block device open
(e.g., timeout-based cleanup racing against a slow job), the zvol is not immediately
freed. ZFS defers destruction until the last reference is released. However, new I/O
from the guest may encounter errors depending on timing:

- **Before destroy completes:** Existing file descriptors continue working (ZFS zvols
  use the kernel block device layer, which holds a reference).
- **After destroy + close:** The device node becomes invalid. Any guest I/O after
  Firecracker reopens the device (unlikely but possible on error recovery paths)
  would fail with `ENXIO`.

**Safe pattern:** Always wait for VM exit (`m.Wait(ctx)`) before `zfs destroy`. If
using timeout-based cleanup, `SIGTERM` the Firecracker process first, wait for exit,
then destroy. The LIFO cleanup stack in the Go SDK handles this ordering naturally.

## Sources

- [Firecracker source: `src/jailer/src/env.rs`](https://github.com/firecracker-microvm/firecracker/blob/main/src/jailer/src/env.rs) -- jailer environment setup and `run()` method
- [Firecracker source: `src/jailer/src/chroot.rs`](https://github.com/firecracker-microvm/firecracker/blob/main/src/jailer/src/chroot.rs) -- pivot_root implementation
- [Firecracker source: `src/jailer/src/cgroup.rs`](https://github.com/firecracker-microvm/firecracker/blob/main/src/jailer/src/cgroup.rs) -- cgroup v1/v2 setup
- [Firecracker source: `src/jailer/src/main.rs`](https://github.com/firecracker-microvm/firecracker/blob/main/src/jailer/src/main.rs) -- FD cleanup, env sanitization
- [Firecracker source: `src/vmm/src/seccomp.rs`](https://github.com/firecracker-microvm/firecracker/blob/main/src/vmm/src/seccomp.rs) -- BPF filter application via `prctl`
- [Seccomp filters (x86_64)](https://github.com/firecracker-microvm/firecracker/blob/main/resources/seccomp/x86_64-unknown-linux-musl.json) -- per-thread syscall allowlists
- [Jailer documentation](https://github.com/firecracker-microvm/firecracker/blob/main/docs/jailer.md) -- usage, operation sequence, known limitations
- [Production host setup](https://github.com/firecracker-microvm/firecracker/blob/main/docs/prod-host-setup.md) -- security configuration recommendations
- [Design document](https://github.com/firecracker-microvm/firecracker/blob/main/docs/design.md) -- threat model, architecture
- [Seccomp documentation](https://github.com/firecracker-microvm/firecracker/blob/main/docs/seccomp.md) -- filter loading, custom filters
- [Test framework: jailer.py](https://github.com/firecracker-microvm/firecracker/blob/main/tests/framework/jailer.py) -- block device mknod pattern
- [Issue #1089: bind mount handling](https://github.com/firecracker-microvm/firecracker/issues/1089) -- MS_REC fix for block device mounts
- [Firecracker + ZFS blog post](https://mgdm.net/weblog/firecracker-ignite-zfs/) -- zvol with ext4 inside, NixOS integration
