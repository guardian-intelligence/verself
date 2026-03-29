# Velo — Git-like Postgres Branching on ZFS

> TypeScript/Bun CLI for instant Postgres database branching via ZFS clones.
>
> Repo: [elitan/velo](https://github.com/elitan/velo)
> Commit: `37dba5c4`

## CHECKPOINT before snapshot (not pg_start_backup)

Before snapshotting, runs `CHECKPOINT` on the running Postgres (~100ms to flush dirty buffers),
then immediately `zfs snapshot`. Works because ZFS snapshots are atomic — after CHECKPOINT the
on-disk state is consistent. Much simpler than the traditional backup API.

The codebase has `startBackupMode`/`stopBackupMode` methods but doesn't use them for branching.

- [`src/services/snapshot-service.ts:30`](https://github.com/elitan/velo/blob/37dba5c4/src/services/snapshot-service.ts#L30) — "executes CHECKPOINT before snapshot"
- [`src/services/snapshot-service.ts:57-58`](https://github.com/elitan/velo/blob/37dba5c4/src/services/snapshot-service.ts#L57-L58) — `docker.execSQL(containerID, 'CHECKPOINT;', username)`
- [`src/services/snapshot-service.ts:67`](https://github.com/elitan/velo/blob/37dba5c4/src/services/snapshot-service.ts#L67) — "Create ZFS snapshot immediately after checkpoint"

## Clone-then-swap for branch reset

Safe swap pattern: clone to temp first, verify, then rename. If cloning fails, the original
is untouched. Only after success does the destructive swap happen.

```
1. Clone parent's snapshot → temp dataset
2. Mount temp to verify
3. Rename original → backup
4. Unmount temp (required by ZFS before rename)
5. Rename temp → original name
6. Mount the swapped dataset
7. Destroy backup (best effort, don't fail if this errors)
```

- [`src/commands/branch/reset.ts:116`](https://github.com/elitan/velo/blob/37dba5c4/src/commands/branch/reset.ts#L116) — "Safe clone-then-swap"
- [`src/commands/branch/reset.ts:119`](https://github.com/elitan/velo/blob/37dba5c4/src/commands/branch/reset.ts#L119) — backup dataset naming
- [`src/commands/branch/reset.ts:136-153`](https://github.com/elitan/velo/blob/37dba5c4/src/commands/branch/reset.ts#L136-L153) — rename sequence + best-effort cleanup

## Atomic state persistence

All metadata lives in a single JSON file, but with surprising rigor:

1. Write to `.tmp`
2. `fsync` the temp file
3. Copy current state to `.backup`
4. `rename` temp → main (atomic on POSIX)

Same pattern as SQLite and etcd.

- [`src/managers/state.ts:99-120`](https://github.com/elitan/velo/blob/37dba5c4/src/managers/state.ts#L99-L120) — full atomic write + backup sequence
- [`src/managers/state.ts:132-148`](https://github.com/elitan/velo/blob/37dba5c4/src/managers/state.ts#L132-L148) — recovery from backup

## Rollback-on-failure mini-transactions

Every multi-step operation uses a `Rollback` class that collects cleanup functions in LIFO order.
If the operation fails at step 3, steps 2 and 1 are unwound. On success, `rollback.clear()`
discards them.

- [`src/utils/rollback.ts:8`](https://github.com/elitan/velo/blob/37dba5c4/src/utils/rollback.ts#L8) — `class Rollback`
- [`src/utils/rollback.ts:25`](https://github.com/elitan/velo/blob/37dba5c4/src/utils/rollback.ts#L25) — "Execute in reverse order (LIFO)"

Directly applicable to forge-metal's sandbox lifecycle (clone → setup → run → cleanup).

## recordsize=8k to match Postgres page size

Hardcoded, no config. Postgres reads/writes in 8KB pages. If ZFS uses a larger recordsize
(default 128KB), a single-page write triggers a read-modify-write amplification.

- [`src/config/defaults.ts:9`](https://github.com/elitan/velo/blob/37dba5c4/src/config/defaults.ts#L9) — `recordsize: '8k'` with comment "PostgreSQL page size"

For forge-metal's ClickHouse, the equivalent tuning would match ClickHouse's mark/granule size.

## Port allocation delegated to Docker

Pass `port: 0` (empty `HostPort`), Docker picks one, read it back after startup. No port
registry, no conflict management.

- [`src/managers/docker.ts:67-70`](https://github.com/elitan/velo/blob/37dba5c4/src/managers/docker.ts#L67-L70) — `HostPort: config.port === 0 ? '' : ...`
- [`src/managers/docker.ts:140-149`](https://github.com/elitan/velo/blob/37dba5c4/src/managers/docker.ts#L140-L149) — `getContainerPort` reads assigned port

## No checkout, no merge

Every branch runs its own Postgres container on its own port simultaneously. There's no
"switching" — you connect to a different port. Branches are throwaway parallel universes.
Sidesteps the hardest part of version control by not doing it.

## Password generation avoids special characters

```typescript
// Use only alphanumeric characters to avoid shell escaping issues
```

- [`src/utils/helpers.ts:7-8`](https://github.com/elitan/velo/blob/37dba5c4/src/utils/helpers.ts#L7-L8) — `generatePassword`

Small but telling — shell escaping is a persistent source of bugs.

## ZFS mount warning workaround

ZFS prints "filesystem successfully created, but it may only be mounted by root" to stderr.
Caught and ignored. Separate sudoers file for mount/unmount only.

- [`src/commands/setup.ts:141`](https://github.com/elitan/velo/blob/37dba5c4/src/commands/setup.ts#L141) — ZFS delegation setup (compression, recordsize, mountpoint, atime)
