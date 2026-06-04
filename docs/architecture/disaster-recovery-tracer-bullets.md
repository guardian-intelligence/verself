# Disaster Recovery Tracer Bullets

## Component Recovery Contract

Infrastructure components own their recovery mechanism. Nomad runs the recovery
task as a batch job. Deployment-service is not involved.

Each recovery task should provide:

- `backup`: create a recovery point and write a manifest.
- `verify`: restore a named recovery point into scratch state and compare
  restored state against the manifest.
- `exercise`: create and verify a recovery point in one run.

Evidence belongs in `verself.recovery_events` and in a manifest colocated with
the recovery point. The ClickHouse insert path must use `batch.AppendStruct`.

## ClickHouse Native Backup Tracer

Recovery point:

```text
20260604T031206Z-992b8c0e5d15
```

Backup key:

```text
dr/v1/sites/prod/components/clickhouse/mechanisms/native_disk_full/points/20260604T031206Z-992b8c0e5d15/verself.zip
```

Manifest:

```text
dr/v1/sites/prod/components/clickhouse/mechanisms/native_disk_full/points/20260604T031206Z-992b8c0e5d15/verself.manifest.json
```

Procedure:

1. Add a dedicated ClickHouse local backup disk named
   `verself_recovery_backups`.
2. Allow native backups only to that disk with ClickHouse `backups.allowed_disk`.
3. Build `//src/infrastructure-components/clickhouse/cmd/clickhouse-recovery:clickhouse-recovery_nomad_artifact`.
4. Submit `src/infrastructure-components/clickhouse/recovery.nomad.hcl` as a
   Nomad batch job with `--action=exercise --site=prod --database=verself`.
5. Query `verself.recovery_events` and `system.backups` for completion
   evidence.

Measurements from prod:

| Source | Measurement | Value |
| --- | --- | --- |
| Nomad | allocation | `8263ba74-7cb3-3252-2888-1b71f3bd7b2d` |
| Nomad | task result | `clickhouse-recovery` exit code `0` |
| ClickHouse | database | `verself` |
| ClickHouse | table count | `38` |
| ClickHouse | source rows | `3,856,311` |
| ClickHouse | restored rows | `3,856,311` |
| `system.backups` | backup status | `BACKUP_CREATED` |
| `system.backups` | restore status | `RESTORED` |
| `system.backups` | files | `3,952` |
| `system.backups` | total bytes | `324,038,266` |
| `system.backups` | uncompressed bytes | `323,865,335` |
| `system.backups` | compressed bytes | `298,488,531` |
| `system.backups` | backup duration | `4,721 ms` |
| `system.backups` | restore duration | `12,974 ms` |
| `verself.recovery_events` | backup duration | `4,734 ms` |
| `verself.recovery_events` | verify duration | `12,986 ms` |
| filesystem | backup archive | `298,488,531 bytes` |
| filesystem | manifest | `7,605 bytes` |
| ClickHouse | scratch database after cleanup | absent |

Findings:

- Native `Disk(...)` and `File(...)` backups were initially blocked because
  `backups.allowed_disk` and `backups.allowed_path` were unset. The tracer uses
  `Disk('verself_recovery_backups', ...)`.
- ClickHouse rejected `compression_method='lz4'` for a `.zip` backup archive.
  The recovery command now lets ClickHouse use the default ZIP behavior.
- `system.backup_log` was absent on prod ClickHouse `26.3.2.3`; `system.backups`
  existed and carried the needed operation status and byte counts.
- The shared recovery cache parent must be root-owned and traversable by
  component users. Component subtrees should be private to their owning system
  users.
- Prod had an empty pre-tracer `verself.recovery_events` table with an
  incompatible shape. It was dropped before the successful run so the recovery
  command could create the canonical evidence schema.
- Nomad allocation logs were unavailable after terminal state because the client
  returned `state for allocation ... not found on client`. Completion evidence
  came from Nomad allocation status, ClickHouse evidence rows, `system.backups`,
  and filesystem measurements.

Remaining gaps:

- This tracer writes to local disk. The component contract is ready for an S3
  target, but offsite storage should be wired through object-storage-service
  once that boundary is available.
- The first exercised scope was the `verself` database. The `default` telemetry
  database is materially larger and should be exercised separately with an
  explicit capacity check.
- The transient loopback artifact server used for the drill should be replaced
  by the normal recovery artifact path.

References:

- ClickHouse Backup and Restore overview:
  <https://clickhouse.com/docs/operations/backup/overview>
- ClickHouse local disk and S3 disk backups:
  <https://clickhouse.com/docs/operations/backup/disk>
- ClickHouse S3 backup endpoint:
  <https://clickhouse.com/docs/operations/backup/s3_endpoint>
- ClickHouse `system.backups`:
  <https://clickhouse.com/docs/operations/system-tables/backups>

## PostgreSQL Logical Cold-Start Tracer

Recovery point:

```text
20260604T034812Z-f28c22ada40e
```

Backup key:

```text
dr/v1/sites/prod/components/postgresql/mechanisms/pg_dumpall_logical/points/20260604T034812Z-f28c22ada40e/postgresql.sql.gz
```

Manifest:

```text
dr/v1/sites/prod/components/postgresql/mechanisms/pg_dumpall_logical/points/20260604T034812Z-f28c22ada40e/manifest.json
```

Procedure:

1. Build `//src/infrastructure-components/postgresql/cmd/postgresql-recovery:postgresql-recovery_nomad_artifact`.
2. Build `//src/infrastructure-components/postgresql:postgresql_runtime`.
3. Submit `src/infrastructure-components/postgresql/recovery.nomad.hcl` as a
   Nomad batch job with `--action=exercise --site=prod`.
4. The job creates a full `pg_dumpall` gzip artifact, manifest, temporary
   restore cluster, and verify evidence file.
5. Verification compares backup digest, backup size, database inventory, and
   restored role count.

Measurements from prod:

| Source | Measurement | Value |
| --- | --- | --- |
| Nomad | allocation | `4d65092c-c30b-b9d2-f164-071b9eff01ba` |
| Nomad | task result | `postgresql-recovery` exit code `0` |
| PostgreSQL | source databases | `22` |
| PostgreSQL | restored databases | `22` |
| PostgreSQL | source roles | `39` |
| PostgreSQL | restored roles | `39` |
| manifest | SHA-256 | `f44088943a08fc2cc3363f6072c04fec6cc40491e7a62876dbcf7bf89ceafa6c` |
| filesystem | backup artifact | `617,331,708 bytes` |
| filesystem | manifest | `1,097 bytes` |
| filesystem | verify evidence | `1,579 bytes` |
| Nomad | task duration | `109 seconds` |
| Nomad | observed peak memory | `482 MiB` |

Findings:

- The logical tracer is useful for cold-start confidence, but it is not the
  durable production PostgreSQL backup mechanism.
- The recovery job must disable Nomad rescheduling. A failed recovery drill
  should leave one failed allocation with evidence rather than silently start a
  replacement attempt.
- Scratch restore verification should include role inventory, not just database
  names, because `pg_dumpall` is a cluster-level logical dump.
- Recovery JSON writes use same-directory temporary files and atomic rename so
  interrupted attempts do not leave fixed `.tmp` files that block retries.
- The current PG job writes host-local manifest/evidence. It still needs the
  canonical ClickHouse evidence insertion path used by the ClickHouse tracer.

Production target:

- Add pgBackRest as the PostgreSQL-owned durable mechanism.
- Enable WAL archiving with `archive_mode=on` and a pgBackRest
  `archive-push` command.
- Store encrypted pgBackRest repositories in object storage.
- Use `pgbackrest backup`, `pgbackrest check`, `pgbackrest info`, and restore
  into quarantine for every drill.
- Keep `pg_dumpall_logical` as a secondary cold-start/export mechanism and
  emergency portability path.

References:

- PostgreSQL continuous archiving and point-in-time recovery:
  <https://www.postgresql.org/docs/current/continuous-archiving.html>
- PostgreSQL `pg_dumpall`:
  <https://www.postgresql.org/docs/current/app-pg-dumpall.html>
- pgBackRest user guide:
  <https://pgbackrest.org/user-guide.html>

## PostgreSQL pgBackRest PITR Drill

The PITR drill runs a destructive scratch PostgreSQL cluster on the prod host.
It does not delete the production service cluster. Each drill creates a fresh
pgBackRest stanza, enables `archive_mode` for the scratch source, takes a full
backup, writes a committed marker workload, stops the source with immediate
shutdown, deletes the scratch PGDATA, restores from pgBackRest, starts the
restored cluster, waits until `pg_is_in_recovery()` is false, compares marker
rows, records I/O counters, writes a JSON report, and inserts one
`verself.recovery_events` row.

Runtime:

- pgBackRest: `2.58.0-1.pgdg24.04+1`, pinned in
  `src/infrastructure-components/postgresql/postgresql.MODULE.bazel`.
- Job: `src/infrastructure-components/postgresql/pitr-drill.nomad.hcl`.
- Binary:
  `//src/infrastructure-components/postgresql/cmd/postgresql-pitr-drill:postgresql-pitr-drill_nomad_artifact`.
- Recovery runtime:
  `//src/infrastructure-components/postgresql:postgresql_recovery_runtime`.

Drill parameters:

| Parameter | Value |
| --- | --- |
| Reports | `10` no-boundary plus `10` forced-boundary |
| Writer duration | `8 seconds` after the full backup |
| Writer interval | `100 ms` |
| Scratch archive timeout | `1 second` |
| Repository | local pgBackRest POSIX repository under the scratch run root |
| No-boundary restore mode | latest restorable archived WAL |
| Forced-boundary restore mode | after every marker, call `pg_switch_wal()`, wait for `pg_stat_archiver.archived_count` to advance, restore to `last_ack_lsn` |

No-boundary measurements from prod:

| Point | Expected rows | Restored rows | Lost rows | Data loss | RTO | Backup | Restore | Archived WAL |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| `20260604T045010Z-5dedf0c03ce7` | `71` | `63` | `8` | `894 ms` | `1,065 ms` | `1,329 ms` | `394 ms` | `11` |
| `20260604T045021Z-d64bb23dab0a` | `72` | `68` | `4` | `448 ms` | `1,312 ms` | `833 ms` | `384 ms` | `12` |
| `20260604T045033Z-ed75edb03431` | `72` | `72` | `0` | `0 ms` | `1,075 ms` | `1,506 ms` | `404 ms` | `13` |
| `20260604T045044Z-a0632cadd458` | `71` | `68` | `3` | `342 ms` | `1,080 ms` | `769 ms` | `403 ms` | `12` |
| `20260604T045055Z-2740c6043ca4` | `71` | `68` | `3` | `337 ms` | `1,069 ms` | `826 ms` | `393 ms` | `12` |
| `20260604T045106Z-b0d43fdc352f` | `72` | `68` | `4` | `447 ms` | `1,054 ms` | `841 ms` | `377 ms` | `12` |
| `20260604T045117Z-1ec5dd8da0f4` | `72` | `69` | `3` | `337 ms` | `1,079 ms` | `768 ms` | `403 ms` | `12` |
| `20260604T045128Z-5e08f263156b` | `71` | `69` | `2` | `220 ms` | `1,073 ms` | `763 ms` | `397 ms` | `12` |
| `20260604T045139Z-d5796ec96314` | `71` | `68` | `3` | `330 ms` | `1,056 ms` | `836 ms` | `381 ms` | `12` |
| `20260604T045150Z-cde317c18453` | `71` | `67` | `4` | `462 ms` | `1,057 ms` | `910 ms` | `384 ms` | `12` |

No-boundary aggregate:

| Measurement | Value |
| --- | --- |
| Successful reports | `10` |
| Lost rows | min `0`, max `8`, avg `3.4` |
| Data loss duration | min `0 ms`, max `894 ms`, avg `381.7 ms` |
| RTO | min `1,054 ms`, max `1,312 ms`, avg `1,092 ms` |
| Backup duration | min `763 ms`, max `1,506 ms`, avg `938.1 ms` |
| Restore command duration | min `377 ms`, max `404 ms`, avg `392 ms` |
| Archived WAL | avg `12` |
| Average write IOPS during drill | `1,408.8` |
| Peak sampled write IOPS | `16,327.6` |
| Average write bytes/sec during drill | `75,117,980` |

Forced-boundary measurements from prod:

| Point | Expected rows | Restored rows | Lost rows | Data loss | RTO | Backup | Restore | Archived WAL | Boundaries |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| `20260604T044723Z-3ef63be9b2c4` | `40` | `40` | `0` | `0 ms` | `2,094 ms` | `1,086 ms` | `401 ms` | `43` | `39` |
| `20260604T044735Z-184baec73e27` | `40` | `40` | `0` | `0 ms` | `2,589 ms` | `741 ms` | `380 ms` | `45` | `39` |
| `20260604T044748Z-102e898b5861` | `40` | `40` | `0` | `0 ms` | `2,378 ms` | `1,221 ms` | `428 ms` | `45` | `39` |
| `20260604T044801Z-9b659a8e6a34` | `40` | `40` | `0` | `0 ms` | `2,604 ms` | `1,442 ms` | `391 ms` | `46` | `39` |
| `20260604T044814Z-ef17b8b0cee9` | `40` | `40` | `0` | `0 ms` | `2,347 ms` | `1,302 ms` | `396 ms` | `44` | `39` |
| `20260604T044827Z-962fe4a72940` | `40` | `40` | `0` | `0 ms` | `2,338 ms` | `1,379 ms` | `389 ms` | `43` | `39` |
| `20260604T044840Z-f442dc37c6fd` | `39` | `39` | `0` | `0 ms` | `2,343 ms` | `1,549 ms` | `390 ms` | `43` | `38` |
| `20260604T044853Z-19d2aa91d94a` | `40` | `40` | `0` | `0 ms` | `2,071 ms` | `1,446 ms` | `373 ms` | `44` | `39` |
| `20260604T044905Z-1f5ecba0d1d7` | `39` | `39` | `0` | `0 ms` | `2,341 ms` | `735 ms` | `388 ms` | `43` | `38` |
| `20260604T044918Z-c7612ea60db6` | `40` | `40` | `0` | `0 ms` | `2,115 ms` | `1,516 ms` | `417 ms` | `44` | `39` |

Forced-boundary aggregate:

| Measurement | Value |
| --- | --- |
| Successful reports | `10` |
| Lost rows | min `0`, max `0`, avg `0` |
| Data loss duration | min `0 ms`, max `0 ms`, avg `0 ms` |
| RTO | min `2,071 ms`, max `2,604 ms`, avg `2,322 ms` |
| Backup duration | min `735 ms`, max `1,549 ms`, avg `1,241.7 ms` |
| Restore command duration | min `373 ms`, max `428 ms`, avg `395.3 ms` |
| Archive boundary wait | avg `3,357 ms` |
| Archive boundaries | avg `38.8` |
| Archived WAL | avg `44` |
| Average write IOPS during drill | `1,971.8` |
| Peak sampled write IOPS | `17,250.3` |
| Average write bytes/sec during drill | `152,953,990` |

Nomad allocations:

| Mode | Allocation | Result |
| --- | --- | --- |
| LSN preflight | `f05c6a2b-f34e-c25e-d252-49e63c2675cd` | exit `0`, `40/40` rows restored |
| Forced boundary, 10 reports | `97880677-908e-3510-b2d5-98e8f89c50b0` | exit `0`, `10/10` reports succeeded |
| No boundary, 10 reports | `27d239cd-d724-5004-d6e4-7a7d92227280` | exit `0`, `10/10` reports succeeded |

Findings:

- Base backup plus archived WAL restored repeatedly through pgBackRest under
  Nomad. The JSON reports and ClickHouse rows agree on marker loss, RTO, and
  byte/I/O measurements.
- `archive_timeout=1s` bounded the scratch loss window below one second in this
  corrected run, but it did not guarantee zero loss. PostgreSQL archives
  completed WAL segments; committed writes still in the current unarchived
  segment die with the lost PGDATA.
- The first PITR report set was discarded. The runner treated read-only
  consistency as restore completion, but PostgreSQL was still replaying WAL.
  Recovery verification now waits for `pg_is_in_recovery()` to become false.
- Target-time restore is the wrong invariant for this synthetic zero-loss
  drill. A target slightly after the last commit can be beyond the available
  archive and PostgreSQL correctly fails with `recovery ended before configured
  recovery target was reached`. The forced-boundary mode records
  `last_ack_lsn` after the marker commit and restores to that LSN.
- Forced boundary plus LSN targeting produced zero lost rows in `10/10` drills,
  but it roughly doubled observed write bytes/sec and raised RTO from
  `1.092 s` to `2.322 s` in this scratch workload. For production this is a
  measurement tool, not the recommended steady-state write policy.
- The measured RTO is only for a tiny scratch database and local POSIX
  repository. Production RTO must be remeasured with the live data size and the
  object-storage repository.
- Near-zero RPO for complete node loss requires streaming replication or
  synchronous remote durability. pgBackRest WAL archiving alone protects only
  WAL that has reached the repository.

References:

- PostgreSQL continuous archiving and point-in-time recovery:
  <https://www.postgresql.org/docs/16/continuous-archiving.html>
- PostgreSQL system administration functions:
  <https://www.postgresql.org/docs/16/functions-admin.html>
- pgBackRest user guide:
  <https://pgbackrest.org/user-guide.html>
- pgBackRest configuration reference:
  <https://pgbackrest.org/configuration.html>

## Durable Recovery Job Plan

Each stateful infrastructure component should have one owner-local recovery job
and one owner-local recovery binary. Jobs use the common contract above and
write `verself.recovery_events` plus component-native evidence.

| Component | Durable mechanism | First job to build | Verification |
| --- | --- | --- | --- |
| PostgreSQL | pgBackRest base backups plus WAL archive | `postgresql-disaster-recovery` with `pgbackrest_full` and `pgbackrest_restore_verify` actions | `pgbackrest check`, quarantine restore, DB/role/schema counts, service-owned smoke queries |
| ClickHouse | native `BACKUP`/`RESTORE` to object storage | extend `clickhouse-disaster-recovery` from local disk to S3-compatible object storage | `system.backups`, scratch DB restore, table/row counts |
| TigerBeetle | replica recovery plus offline data-file snapshots for single-node | `tigerbeetle-disaster-recovery` | scratch TigerBeetle process, ledger/account invariant checks |
| OpenBao | raft snapshots | `openbao-disaster-recovery` | scratch OpenBao restore, unseal, auth mount/policy/KV metadata checks |
| SPIRE | datastore and key-manager backup | `spire-disaster-recovery` | scratch SPIRE server, bundle and entry listing checks |
| Nomad | rebuild from repo; optional raft snapshot | `nomad-disaster-recovery` only if ACLs/variables/ad hoc state become authoritative | fresh Nomad plus repo job resubmission, optional snapshot restore into quarantine |
| NATS JetStream | account or stream backup if streams become authoritative | `nats-disaster-recovery` | scratch NATS restore, stream/consumer inventory |
| Zot | object/registry storage backup if registry becomes release truth | `zot-disaster-recovery` | scratch registry, manifest/blob verification |
| Verdaccio | rebuildable mirror unless local packages become authoritative | no durable job until authoritative local package state exists | cache rebuild from lockfiles and upstream mirror policy |
| Forgejo | SQLite/repository backup if Forgejo becomes source truth | `forgejo-disaster-recovery` | scratch Forgejo, repository count, DB integrity check |

Order of implementation:

1. PostgreSQL pgBackRest PITR.
2. TigerBeetle recovery.
3. OpenBao raft snapshots.
4. SPIRE datastore/key backup.
5. ClickHouse S3-backed native backup.
6. NATS JetStream classification and recovery if needed.
7. Zot/Verdaccio/Forgejo classification and jobs only where state is
   authoritative.
