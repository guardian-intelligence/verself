// Package zfsharness manages ZFS-backed VM allocation from golden images.
//
// It provides the core operations needed for CI job sandboxing:
// golden image management, per-job clone allocation, crash recovery,
// and metrics collection. Not a generic ZFS wrapper — every exported
// type exists to serve the golden-image-clone allocation pattern.
//
// # Architecture
//
// Two layers:
//
//   - Public: [Harness] + [Clone] speak the language of golden images,
//     jobs, and allocation. Harness.Allocate is the hot path.
//   - Private: executor wraps the zfs CLI. All ~25 ZFS operations are
//     preserved for internal use and future features (golden rotation,
//     replication).
//
// # Design decisions
//
//   - Shell out to `zfs` CLI, never libzfs. Every production ZFS project
//     (DBLab ~1,300 lines, Incus ~4,400 lines, OBuilder, Velo) does this.
//     The CLI is ZFS's stable API; go-libzfs (bicomsystems/go-libzfs) has
//     SIGSEGV panics from concurrent C memory handling, breaks on every ZFS
//     version upgrade (0.6→0.7→0.8→2.0→2.1→2.2), and has a global mutex
//     that serializes all property reads.
//   - Use -H (tab-separated) and -p (machine-parseable) flags for structured
//     output, following mistifyio/go-zfs v4 conventions.
//   - No mutexes — ZFS handles kernel-level locking. Multiple goroutines can
//     call operations concurrently via independent subprocesses.
//   - Context-based timeouts on every command (ZFS can hang on degraded pools).
//
// # Performance profile (measured on file-backed pool)
//
// A typical `zfs clone` takes ~5.7ms end-to-end from Go:
//
//	fork+exec+load zfs binary:  ~2.0ms  (dynamic linker, libc init)
//	ZFS_IOC_CLONE ioctl:        ~1.7ms  (actual kernel clone)
//	Post-clone OBJSET_STATS:    ~0.5ms  (re-fetch properties after mutation)
//	Pre-clone validation:       ~0.3ms  (OBJSET_STATS, POOL_STATS checks)
//	Go exec.Command overhead:   ~1.2ms  (runtime fork, pipe setup)
//
// The kernel clone is 1.7ms; the other ~4ms is subprocess ceremony. Three
// paths to reduce this if burst scheduling (1000+ clones/sec) demands it:
//
//  1. Direct ioctl from Go — bypass the zfs binary entirely. The kernel
//     interface is a single ioctl(fd, ZFS_IOC_CLONE, &zfs_cmd). Incus
//     (lxc/incus cmd/incusd/main_forkzfs.go) wraps ioctls in a re-exec
//     subprocess for mount namespace isolation, but the ioctl itself is
//     straightforward. Tradeoff: must track the unstable ioctl struct layout
//     across ZFS versions (the same reason bicomsystems/go-libzfs breaks on
//     every upgrade). Only viable if pinning to a single ZFS version.
//
//  2. Pre-forked zfs process — keep a long-lived zfs subprocess and feed
//     it commands over stdin. The CLI doesn't support this, but a thin C
//     shim around libzfs_core (not libzfs — libzfs_core is the lower-level
//     stable-ish interface) could accept newline-delimited commands. DBLab
//     (postgres-ai/database-lab-engine) considered this but chose simplicity:
//     the clone itself is O(1) and the subprocess cost is negligible vs the
//     seconds spent starting Postgres on the clone.
//
//  3. Batch cloning — pre-create a pool of clones and hand them out. DBLab's
//     async clone pattern (engine/internal/cloning/base.go:198) returns
//     StatusCreating immediately and provisions in a goroutine. For CI, a
//     warm pool of N ready clones eliminates clone latency from the critical
//     path entirely. Tradeoff: idle clones consume ZFS metadata memory
//     (~50-100KB each) and complicate golden image rotation.
package zfsharness
