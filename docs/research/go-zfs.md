# go-zfs — Go ZFS Library Comparison

> Comparing mistifyio/go-zfs (CLI wrapper) vs bicomsystems/go-libzfs (C bindings).
>
> go-zfs repo: [mistifyio/go-zfs](https://github.com/mistifyio/go-zfs) @ `2a28eb65`
> go-libzfs repo: [bicomsystems/go-libzfs](https://github.com/bicomsystems/go-libzfs) @ `f9d12fe5`

## Recommendation: use mistifyio/go-zfs v4

go-libzfs has fundamental issues that make it unsuitable for production use.
Every project we studied (DBLab, OBuilder, Velo) shells out to the CLI.

## mistifyio/go-zfs: how it works

Every operation spawns a subprocess via `exec.Command`. No pooling, no connection reuse.
Uses ZFS's `-H` (tab-separated) and `-p` (machine-parseable) flags for structured output.

- [`utils.go`](https://github.com/mistifyio/go-zfs/blob/2a28eb65/utils.go) — `command.Run()` creates `exec.Cmd` per call
- Timeout support via `exec.CommandContext` with SIGTERM → grace period → SIGKILL
- Custom `Error` struct wraps `Err`, `Debug` (command string), and `Stderr`
- After every mutation (`Clone`, `Snapshot`, etc.), re-fetches dataset with `GetDataset()` — safe but costs an extra subprocess

## go-zfs: what it exposes

Covered: create/destroy datasets, snapshots, clone, rollback, send/receive, mount/unmount,
rename, diff, get/set property (single at a time), zpool CRUD.

**NOT exposed** (gaps relevant to forge-metal):
- `zfs promote` — critical for golden image updates with active clones
- `zfs hold` / `zfs release` — snapshot holds
- `zfs inherit` — property inheritance
- User properties (`user:*` custom properties)
- Encrypted/resumable send/receive
- `zpool scrub`, `zpool status` (detailed health info)

Adding `promote` is trivial — one `exec.Command("zfs", "promote", dataset)` following
existing patterns.

## go-zfs: concurrency model

No mutexes, no locking. The `Runner` is an `atomic.Value` (safe for concurrent reads), but
that's the only consideration. Multiple goroutines calling ZFS operations each spawn independent
subprocesses. ZFS handles locking at the kernel level.

## go-zfs: known gotchas

- `GetProperty("creation")` returned human-readable dates until v4 added `-p` flag
- ZFS commands can hang indefinitely (degraded pool, importing) — v4 added timeout support
- Library assumes `zfs`/`zpool` in PATH — no custom binary paths
- No `errors.Unwrap()` on custom `Error` type — `errors.Is()` won't work through it
- Global logger and runner state via package-level `SetLogger()`/`SetRunner()`

## go-libzfs: why not

| Issue | Impact |
|-------|--------|
| **SIGSEGV panics** from concurrent handlers freeing C memory | Production crashes |
| **Breaks on every ZFS version upgrade** (0.6, 0.7, 0.8, 2.0, 2.1, 2.2) | Constant maintenance |
| **Global mutex** serializes all property reads | Eliminates concurrency benefits |
| **`SendSize()` redirects process fd 1** (stdout) | Process-global, not thread-safe |
| Requires libzfs-dev headers at compile time | Kills cross-compilation |
| `DatasetOpenAll()` can hang indefinitely | No timeout mechanism |
| `go.mod` says `go 1.13`, compatibility via git branches | No semver, no modules |

The CLI is ZFS's stable interface. libzfs is explicitly not a stable API.
