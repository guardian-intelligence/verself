# gVisor Filesystem Optimization for React/Next.js CI

> How gVisor's filesystem modes interact with npm/Next.js workloads, and which
> configurations minimize the sandboxing overhead for build-heavy CI jobs.
>
> Sources: gVisor docs, gVisor blog (rootfs overlay, directfs, seccomp optimization)
>
> Conducted 2026-03-30.

## The gVisor filesystem bottleneck

gVisor intercepts all syscalls and re-implements them in userspace (the "Sentry"). For
filesystem operations, the traditional model routes every file operation through a
separate "gofer" process via LISAFS RPC:

```
App → syscall → Sentry → LISAFS RPC → Gofer → host syscall → kernel
```

This adds two layers of overhead:
1. **RPC cost** — inter-process communication for every file operation
2. **Host syscall cost** — the gofer itself makes host syscalls with kernel context switches

For React/Next.js builds, which are heavily filesystem-bound (npm ci: 50K-100K file writes,
tsc: 30K-45K stat() calls, next build: thousands of module reads), this overhead is
devastating.

## Three gVisor optimizations that matter

### 1. Rootfs Overlay (default since ~2023)

**What it does:** Places a tmpfs-backed overlay **inside** the sandbox over the read-only
host filesystem. All writes go to in-sandbox tmpfs instead of through the gofer.

**Architecture:**
```
Before: App write → Sentry → RPC → Gofer → host overlay upper → kernel
After:  App write → Sentry → tmpfs (in-process) — no RPC, no host syscall
```

**Performance impact:**
- Microbenchmark (fsstress): **262.79s → 3.18s** (82x faster)
- Real-world (abseil-cpp build): **Halved the sandboxing overhead**

**Memory management:** To prevent tmpfs from exhausting container memory, gVisor uses a
"filestore" — a single host file that backs the tmpfs data via memory-mapping. The sandbox
can access and mutate the filestore efficiently without gofer RPCs.

**Self-backed mode:** Places the filestore in the host overlay's upper layer. This allows
kubelet to detect storage usage while hiding the file from the container. Configured via:
```
--overlay2=root:self
```

**Applicability to forge-metal:** The rootfs overlay is critical for `npm ci` performance.
Without it, every file extraction during package installation would require a gofer RPC.
With it, all writes go to in-sandbox tmpfs (memory-backed or filestore-backed).

**However:** The rootfs overlay means writes don't go to the ZFS zvol directly. For
forge-metal, we want writes to go to the zvol (so `zfs get written` captures I/O).
We may need `--overlay2=root:dir:/path` to place the overlay data on the zvol mount.

### 2. Directfs (default since ~2023)

**What it does:** Instead of routing file operations through the gofer via RPC, the sandbox
makes file-descriptor-relative syscalls directly. The gofer donates FDs for mount points
via SCM_RIGHTS, then the sandbox uses `openat(2)`, `fstatat(2)`, etc.

**Security model:**
- `O_NOFOLLOW` enforced via seccomp (prevents symlink traversal attacks)
- No procfs access
- Sandbox gets same privileges as gofer (e.g., `CAP_DAC_OVERRIDE`)
- Linux mount namespace isolation prevents escape

**Performance impact:**
- `stat(2)`: **>2x faster** (critical for TypeScript module resolution)
- Real-world workloads: **12% reduction in absolute runtime**
- Ruby load time: **17% reduction**

**Applicability to forge-metal:** `stat(2)` being 2x faster directly helps:
- TypeScript module resolution (30K-45K stat() calls per typecheck)
- ESLint file traversal
- Node.js `require()` resolution
- Next.js page/route discovery

### 3. Dentry Cache tuning

gVisor maintains an LRU cache of unreferenced directory entries. Default: 1000 entries
per mount.

**For React/Next.js workloads:** `node_modules` has deeply nested directory trees with
50K+ entries. The default 1000-entry cache will thrash constantly. Increasing it should
reduce repeated stat() lookups:

```
--dcache=10000  # or per-mount: mount with dcache option
```

**Exclusive file access:** When the container is the only writer (true for CI jobs),
enable aggressive caching:
```
--file-access-mounts=exclusive
```
This avoids continuous dentry revalidation against the host filesystem.

## fsync behavior in gVisor

gVisor implements `fsync` by calling `fsync` on the underlying host file descriptor
(in directfs mode) or by sending an RPC to the gofer (in non-directfs mode).

**For tmpfs overlay writes:** `fsync` on tmpfs is essentially a no-op — tmpfs data is
already in memory. If the rootfs overlay catches writes before they hit the host, fsync
calls during `npm ci` are already nearly free.

**For writes that reach the host (zvol):** `fsync` forces a ZFS transaction group (TXG)
commit. This is the expensive path.

**The optimization stack:**
1. gVisor rootfs overlay catches most writes → fsync is tmpfs no-op
2. For writes that escape to host: seccomp filter intercepts fsync → return 0
3. For the few critical writes (lockfile, cache manifests): let fsync through

**Ideal configuration:** Use rootfs overlay for the build working directory (npm ci, next
build output). Mount the git repository from the zvol with directfs for read performance.
This gives:
- Fast writes (tmpfs overlay)
- Fast reads (directfs, no gofer RPC)
- Accurate `zfs get written` for reads that cause COW (git checkout changes)

## Recommended gVisor configuration for CI

```bash
runsc \
  --directfs=true \                    # Direct FD-based syscalls (default)
  --overlay2=root:self \               # Tmpfs overlay backed by host file
  --dcache=10000 \                     # Larger dentry cache for node_modules
  --file-access-mounts=exclusive \     # Aggressive caching (CI is sole writer)
  --platform=systrap                   # Fastest platform for syscall interception
```

**Platform choice:** `systrap` (formerly ptrace) is the default. For KVM-enabled hosts,
`--platform=kvm` reduces syscall interception overhead further. Inside a Firecracker VM,
KVM is not available (nested virtualization), so `systrap` is the only option.

## The EROFS optimization (future)

gVisor supports EROFS (Enhanced Read-Only Filesystem) for memory-mapped read-only layers.
For golden image content that never changes (Node.js binary, npm binary, base system files),
EROFS could eliminate host syscalls entirely for reads.

**Not yet applicable:** forge-metal's golden image is a full ext4 zvol, not an EROFS image.
But for future optimization, packaging the Nix closure as EROFS and mounting it separately
could speed up tool startup.

## Benchmarks to run

To quantify the gVisor filesystem overhead for React/Next.js CI:

1. **Baseline (no gVisor):** Run npm ci + next build directly on the host (zvol mount)
2. **gVisor default:** runsc with default settings
3. **gVisor optimized:** runsc with dcache=10000, exclusive file access
4. **gVisor + overlay:** runsc with rootfs overlay (tmpfs or self-backed)

Measure per-phase wall-clock time, syscall counts (via gVisor's internal tracing), and
`zfs get written` on the zvol.

The delta between (1) and (4) is the "sandboxing tax" — the cost of security isolation.
The goal is to minimize this tax while maintaining the isolation guarantee.

## Applicability to forge-metal

1. **Rootfs overlay is critical.** Without it, npm ci performance inside gVisor will be
   unacceptable (80x+ overhead on write-heavy workloads). Ensure overlay is enabled.

2. **Directfs is critical for typecheck.** The 2x stat() improvement directly helps tsc's
   30K-45K stat() calls during module resolution. Ensure directfs is enabled (default).

3. **Increase dcache for node_modules.** Default 1000 entries is too small for a 50K+ file
   tree. Set `--dcache=10000` or higher.

4. **Exclusive file access.** CI jobs are the sole writer to their clone. Enable
   `--file-access-mounts=exclusive` to avoid dentry revalidation overhead.

5. **Platform: systrap inside Firecracker.** No KVM available (no nested virt). systrap
   is the only option. This is fine — systrap overhead is <5% for computation-heavy
   workloads.

6. **Test overlay placement carefully.** If overlay data lives in tmpfs, `zfs get written`
   won't capture build I/O. If it lives on the zvol (`--overlay2=root:dir:/mnt`), writes
   go through the host filesystem and are captured. Choose based on whether I/O tracking
   or raw speed is more important.
