# OBuilder — ZFS Build Cache with Sandboxing

> OCaml CI infrastructure. ZFS/btrfs snapshots per build step, runc/jail/macOS sandboxing.
>
> Repo: [ocurrent/obuilder](https://github.com/ocurrent/obuilder)
> Commit: `9810eb23`

## Seccomp fsync bypass

Intercepts all sync syscalls (`fsync`, `fdatasync`, `msync`, `sync`, `syncfs`, `sync_file_range`)
via seccomp and returns success without actually syncing. Rationale: if the build crashes, the
result is discarded anyway (no `@snap` tag). Massive speedup for `npm install` style workloads.

```ocaml
let seccomp_syscalls ~fast_sync =
  if fast_sync then [
    (* ... *)
    "fsync"; "fdatasync"; "msync"; "sync"; "syncfs"; "sync_file_range";
    (* ... *)
    "errnoRet", `Int 0;  (* Return error "success" *)
```

- [`lib/sandbox.runc.ml:80-96`](https://github.com/ocurrent/obuilder/blob/9810eb23/lib/sandbox.runc.ml#L80-L96) — syscall list and seccomp policy generation
- [`lib/sandbox.runc.ml:274`](https://github.com/ocurrent/obuilder/blob/9810eb23/lib/sandbox.runc.ml#L274) — injected into OCI spec
- [`lib/sandbox.runc.ml:342-353`](https://github.com/ocurrent/obuilder/blob/9810eb23/lib/sandbox.runc.ml#L342-L353) — CLI flag `--fast-sync`
- Based on: https://bblank.thinkmo.de/using-seccomp-to-filter-sync-operations.html
- Requires runc >= v1.0.0-rc92 for `errnoRet` support

**Applicability to forge-metal**: gVisor's `runsc` supports seccomp profiles. Could eliminate the
biggest I/O bottleneck in Next.js builds where `npm ci` calls `fsync` thousands of times.

## Content-addressable build steps via hash chains

Each build step's ID = `SHA256(sexp(base_id, command, env, user, workdir, ...))`. Since `base_id`
is itself a hash of the previous step, any change cascades through all subsequent steps. Structurally
identical to Git commit hashing.

- [`lib/build.ml:73-78`](https://github.com/ocurrent/obuilder/blob/9810eb23/lib/build.ml#L73-L78) — `sexp_of_run_input` → SHA256
- [`lib/build.ml:148`](https://github.com/ocurrent/obuilder/blob/9810eb23/lib/build.ml#L148) — same pattern for COPY steps (hashes file manifest)
- [`lib/build.ml:234`](https://github.com/ocurrent/obuilder/blob/9810eb23/lib/build.ml#L234) — base image ID = hash of image name string

Two developers running the same build automatically share cached ZFS snapshots.

## `@snap` tag as crash recovery marker

Build results are written directly to their final ZFS dataset (not a temp location — ZFS can't
rename while files are open). The `@snap` snapshot is created only on success. On startup, any
dataset without `@snap` is an incomplete build and gets cleaned up.

- [`lib/zfs_store.ml:210-211`](https://github.com/ocurrent/obuilder/blob/9810eb23/lib/zfs_store.ml#L210-L211) — "We start by either creating a new dataset or by cloning base@snap"
- [`lib/zfs_store.ml:288-307`](https://github.com/ocurrent/obuilder/blob/9810eb23/lib/zfs_store.ml#L288-L307) — cache update: main@snap presence = validity check

No external state needed. The ZFS namespace *is* the state.

## The promotion dance (hardest ZFS code)

ZFS can't delete a dataset that has clones. The big comment at the top of the file explains:

```
1. Create ds1.
2. Create snapshots ds1@snap1, ds1@snap2, ds1@snap3.
3. Create clones of ds1@snap2: clone1, clone2, clone3.
4. Promote clone2.
Now: clone2 has clones {clone1, ds1, clone3} and snapshots {snap1, snap2}.
     ds1 has no clones and snapshots {snap3}.
```

- [`lib/zfs_store.ml:4-24`](https://github.com/ocurrent/obuilder/blob/9810eb23/lib/zfs_store.ml#L4-L24) — full explanation of the promotion model
- [`lib/zfs_store.ml:159-160`](https://github.com/ocurrent/obuilder/blob/9810eb23/lib/zfs_store.ml#L159-L160) — `Zfs.promote` wrapper
- [`lib/zfs_store.ml:339-345`](https://github.com/ocurrent/obuilder/blob/9810eb23/lib/zfs_store.ml#L339-L345) — promote, then move old `@snap` out of the way

## Mysterious fuser workaround

When destroying a temporary cache dataset fails, they debug with `fuser -mv`, sleep, and retry.
Comment: "Don't know what's causing this."

- [`lib/zfs_store.ml:362-363`](https://github.com/ocurrent/obuilder/blob/9810eb23/lib/zfs_store.ml#L362-L363)

## LRU eviction with reference-counted GC

SQLite tracks each build result with `rc` (reference count of child builds depending on it).
Only leaves of the dependency tree (rc=0) and older than a threshold are eviction candidates.

```sql
SELECT id FROM builds WHERE rc = 0 AND used < ? ORDER BY used ASC LIMIT ?
```

- [`lib/dao.ml:43`](https://github.com/ocurrent/obuilder/blob/9810eb23/lib/dao.ml#L43) — the eviction query
- [`lib/dao.ml:40-45`](https://github.com/ocurrent/obuilder/blob/9810eb23/lib/dao.ml#L40-L45) — full prepared statement set

## SQLite WAL mode with relaxed sync

```ocaml
exec_literal db "PRAGMA journal_mode=WAL";
exec_literal db "PRAGMA synchronous=NORMAL";
```

Acceptable because `@snap` tag on ZFS is the real consistency marker — SQLite losing the last
transaction is recoverable.

- [`lib/db.ml:57-58`](https://github.com/ocurrent/obuilder/blob/9810eb23/lib/db.ml#L57-L58)

## macOS: real OS users as sandbox isolation

On macOS (no containers), OBuilder creates actual macOS users via `dscl` and runs builds as
those users with `sudo su -l`. ZFS `mountpoint` manipulation provides filesystem isolation.
Builds are serialized (global `Lwt_mutex`) because of this.

- [`lib/sandbox.macos.ml:67`](https://github.com/ocurrent/obuilder/blob/9810eb23/lib/sandbox.macos.ml#L67) — `create_new_user`
- [`lib/sandbox.macos.ml:124`](https://github.com/ocurrent/obuilder/blob/9810eb23/lib/sandbox.macos.ml#L124) — `dscl` reference

## Concurrent build deduplication

When a second request arrives for an ID already being built, it tails the existing build log
instead of starting a new build. Uses `Lwt_condition` for live log tailing.

- [`lib/db_store.ml` build dedup](https://github.com/ocurrent/obuilder/blob/9810eb23/lib/db_store.ml) — in-memory `Builds` map keyed by build ID

## Cache generation counter

Shared caches (e.g., opam package cache) use a generation counter. When releasing a cache clone,
check if `cache.gen` still matches. If yes, replace main cache. If no (someone else updated),
discard.

- [`lib/zfs_store.ml:28-30`](https://github.com/ocurrent/obuilder/blob/9810eb23/lib/zfs_store.ml#L28-L30) — `gen : int` field
