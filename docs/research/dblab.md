# DBLab Engine — ZFS Thin-Clone PostgreSQL

> Most mature ZFS-based database cloning tool. Clone a 1TB Postgres in ~10s.
>
> Repo: [postgres-ai/database-lab-engine](https://github.com/postgres-ai/database-lab-engine)
> Commit: `16c9fa32`

## Dual-pool rotation for live refresh

The hardest problem: how to update the golden snapshot while clones are serving traffic.
DBLab maintains an ordered linked list of pool FSManagers. Refresh targets the pool with
zero active clones; new clones go to the freshly refreshed pool.

```go
func (pm *Manager) GetPoolToUpdate() *list.Element {
    for element := pm.fsManagerList.Back(); element != nil; element = element.Prev() {
        clones, err := fsm.ListClonesNames()
        if len(clones) == 0 {
            return element
        }
    }
    return nil
}
```

- [`engine/internal/provision/pool/pool_manager.go:40`](https://github.com/postgres-ai/database-lab-engine/blob/16c9fa32/engine/internal/provision/pool/pool_manager.go#L40) — `fsManagerList *list.List`
- [`engine/internal/provision/pool/pool_manager.go:76-78`](https://github.com/postgres-ai/database-lab-engine/blob/16c9fa32/engine/internal/provision/pool/pool_manager.go#L76-L78) — `MakeActive` moves element to front
- [`engine/internal/provision/pool/pool_manager.go:120-135`](https://github.com/postgres-ai/database-lab-engine/blob/16c9fa32/engine/internal/provision/pool/pool_manager.go#L120-L135) — `GetPoolToUpdate` finds idle pool

No downtime, no promotion dance, no ZFS trickery. Just route new clones to the fresh pool
while old clones drain naturally.

## Pre-snapshot + clone dance for consistent Postgres snapshots

Never snapshots a running database directly. Instead:

1. `zfs snapshot pool@snapshot_<ts>_pre` (captures live state, may have dirty buffers)
2. `zfs clone pool@snapshot_pre → clone_pre_<ts>` (new dataset from that snapshot)
3. Start Postgres in a Docker container on the clone
4. Promote from replica → primary (`pg_ctl promote`)
5. `CHECKPOINT` (flush everything)
6. Stop Postgres
7. `zfs snapshot clone_pre@clean` (now the data is crash-consistent)

- [`engine/internal/retrieval/engine/postgres/snapshot/physical.go:58-59`](https://github.com/postgres-ai/database-lab-engine/blob/16c9fa32/engine/internal/retrieval/engine/postgres/snapshot/physical.go#L58-L59) — `pre` and `promoteContainerPrefix` constants
- [`engine/internal/retrieval/engine/postgres/snapshot/physical.go:352`](https://github.com/postgres-ai/database-lab-engine/blob/16c9fa32/engine/internal/retrieval/engine/postgres/snapshot/physical.go#L352) — "Prepare pre-snapshot"
- [`engine/internal/retrieval/engine/postgres/snapshot/physical.go:581-672`](https://github.com/postgres-ai/database-lab-engine/blob/16c9fa32/engine/internal/retrieval/engine/postgres/snapshot/physical.go#L581-L672) — `promoteInstance` full flow

## Custom ZFS user properties as metadata layer

ZFS doesn't expose clone→parent relationships. DBLab builds a virtual DAG using custom
properties: `dle:branch`, `dle:parent`, `dle:child`, `dle:root`, `dle:message`.

Commit messages are base64-encoded to avoid ZFS property value escaping issues:

```go
encodedMessage := base64.StdEncoding.EncodeToString([]byte(message))
```

- [`engine/internal/provision/thinclones/zfs/zfs.go:9`](https://github.com/postgres-ai/database-lab-engine/blob/16c9fa32/engine/internal/provision/thinclones/zfs/zfs.go#L9) — `encoding/base64` import
- [`engine/internal/provision/thinclones/zfs/zfs.go:520`](https://github.com/postgres-ai/database-lab-engine/blob/16c9fa32/engine/internal/provision/thinclones/zfs/zfs.go#L520) — `zfs list -H -o dle:parent,dle:branch`
- [`engine/internal/provision/thinclones/zfs/zfs.go:1290`](https://github.com/postgres-ai/database-lab-engine/blob/16c9fa32/engine/internal/provision/thinclones/zfs/zfs.go#L1290) — base64 decode

## Pure shell-out (no go-zfs, no libzfs)

All ZFS ops are `fmt.Sprintf` → `exec.Command("/bin/bash", "-c", cmd)`. ~1300 lines of Go.
Conscious tradeoff for portability and simplicity.

```go
cmd := fmt.Sprintf("zfs clone -p -o mountpoint=%s %s %s && chown -R %s %s",
    cloneMountLocation, snapshotID, cloneMountName, m.config.OSUsername, cloneMountLocation)
```

- [`engine/internal/provision/thinclones/zfs/zfs.go:210-217`](https://github.com/postgres-ai/database-lab-engine/blob/16c9fa32/engine/internal/provision/thinclones/zfs/zfs.go#L210-L217) — clone creation
- [`engine/internal/provision/thinclones/zfs/zfs.go:419`](https://github.com/postgres-ai/database-lab-engine/blob/16c9fa32/engine/internal/provision/thinclones/zfs/zfs.go#L419) — snapshot creation
- [`engine/internal/provision/thinclones/zfs/zfs.go:259`](https://github.com/postgres-ai/database-lab-engine/blob/16c9fa32/engine/internal/provision/thinclones/zfs/zfs.go#L259) — destroy

Has a TODO acknowledging go-libzfs but never adopted it:
- [`engine/internal/provision/thinclones/zfs/zfs.go:1047`](https://github.com/postgres-ai/database-lab-engine/blob/16c9fa32/engine/internal/provision/thinclones/zfs/zfs.go#L1047) — the TODO comment

## Async clone creation

`CreateClone()` returns immediately with `StatusCreating`, provisions in a goroutine.
The ZFS clone is O(1) but starting the Docker container + Postgres promotion takes seconds.

```go
go func() {
    session, err := c.provision.StartSession(clone, ephemeralUser, cloneRequest.ExtraConf)
    c.fillCloneSession(cloneID, session)
}()
return clone, nil
```

- [`engine/internal/cloning/base.go:198`](https://github.com/postgres-ai/database-lab-engine/blob/16c9fa32/engine/internal/cloning/base.go#L198) — `StatusCreating`
- [`engine/internal/cloning/base.go:222`](https://github.com/postgres-ai/database-lab-engine/blob/16c9fa32/engine/internal/cloning/base.go#L222) — `go func()`
- [`engine/internal/cloning/base.go:238`](https://github.com/postgres-ai/database-lab-engine/blob/16c9fa32/engine/internal/cloning/base.go#L238) — `fillCloneSession`

## Snapshot cleanup pipeline

Constructs a single shell pipeline that sorts, filters busy snapshots, and destroys in one pass:

```go
"zfs list -t snapshot -H -o name -s %s -s creation -r %s | grep -v clone %s | head -n -%d %s" +
    "| xargs -n1 --no-run-if-empty zfs destroy -R "
```

`head -n -%d` keeps the N newest (retention limit). Busy snapshots (those with dependent clones)
are excluded via `grep -Ev`.

- [`engine/internal/provision/thinclones/zfs/zfs.go:580-607`](https://github.com/postgres-ai/database-lab-engine/blob/16c9fa32/engine/internal/provision/thinclones/zfs/zfs.go#L580-L607) — `CleanupSnapshots`
- [`engine/internal/provision/thinclones/zfs/zfs.go:816-821`](https://github.com/postgres-ai/database-lab-engine/blob/16c9fa32/engine/internal/provision/thinclones/zfs/zfs.go#L816-L821) — `excludeBusySnapshots` constructs grep filter

## Automatic password reset on clone

When cloning production data, all Postgres user passwords are reset to random MD5 hashes.
Prevents credential leakage from prod into dev/test.

- [`engine/internal/provision/databases/postgres/postgres_mgmt.go:18-28`](https://github.com/postgres-ai/database-lab-engine/blob/16c9fa32/engine/internal/provision/databases/postgres/postgres_mgmt.go#L18-L28) — `ResetPasswordsQuery` template
- [`engine/internal/provision/databases/postgres/postgres_mgmt.go:44-68`](https://github.com/postgres-ai/database-lab-engine/blob/16c9fa32/engine/internal/provision/databases/postgres/postgres_mgmt.go#L44-L68) — `ResetAllPasswords`

## Idle clone auto-reaping

Periodic timer (every 5 minutes) checks if clones are idle beyond `maxIdleMinutes`.
Parses Postgres CSV logs for recent activity, falls back to `pg_stat_activity`.

- [`engine/internal/cloning/base.go:38`](https://github.com/postgres-ai/database-lab-engine/blob/16c9fa32/engine/internal/cloning/base.go#L38) — `idleCheckDuration = 5 * time.Minute`
- [`engine/internal/cloning/base.go:45`](https://github.com/postgres-ai/database-lab-engine/blob/16c9fa32/engine/internal/cloning/base.go#L45) — `MaxIdleMinutes` config
- [`engine/internal/cloning/base.go:796-806`](https://github.com/postgres-ai/database-lab-engine/blob/16c9fa32/engine/internal/cloning/base.go#L796-L806) — timer loop
- [`engine/internal/cloning/base.go:836`](https://github.com/postgres-ai/database-lab-engine/blob/16c9fa32/engine/internal/cloning/base.go#L836) — `isIdleClone`
