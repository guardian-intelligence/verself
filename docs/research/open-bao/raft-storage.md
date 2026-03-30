# Raft Integrated Storage

OpenBao's recommended storage backend for production. Eliminates external dependencies
(no Consul, no PostgreSQL) -- a single Go binary with embedded consensus.

Source: `physical/raft/` in https://github.com/openbao/openbao
Docs: https://openbao.org/docs/configuration/storage/raft/

## Architecture

The `RaftBackend` wraps HashiCorp's `raft` library (`github.com/hashicorp/raft`) with
`raft-boltdb/v2` for log storage. It implements:
- `physical.Backend` -- standard Get/Put/Delete/List
- `physical.Transactional` -- atomic multi-key operations (OpenBao addition)
- `physical.HABackend` -- leader election for multi-node

## On-disk format

BoltDB (`vault.db`) is the underlying storage engine. The FSM (`fsm.go`) writes all data to
a single BoltDB file with two buckets:

| Bucket | Contents |
|--------|----------|
| `data` | All key-value pairs (OpenBao's encrypted storage entries) |
| `config` | Raft metadata: `latest_indexes`, `latest_config`, `local_node_config` |

The database filename is hardcoded as `databaseFilename = "vault.db"`.

Put/Delete operations are submitted as Raft log entries, replicated to quorum (self on
single-node), then applied to the BoltDB FSM. The FSM implements `raft.BatchingFSM` for
efficient batch applies.

## Snapshots

The `BoltSnapshotStore` (`snapshot.go`) uses a "just-in-time" approach. Since the FSM already
maintains a complete BoltDB file, snapshots are served directly from the live database rather
than creating incremental dumps. The snapshot ID is a constant `"bolt-snapshot"`.

When installing a snapshot from another node, it writes a new BoltDB file in batches of
50,000 entries per transaction, then atomically renames it into place using `safeio.Rename`.

## Transactional storage (OpenBao-specific)

`transaction.go` (31.7KB) implements transactional storage with SHA-384 hash verification for
read consistency. This is an OpenBao addition not present in Vault. It uses
`beginTxOp`/`commitTxOp` FSM operations with verification hashes to detect conflicts.

## Configuration

```hcl
storage "raft" {
    path                   = "/var/lib/openbao/raft"
    node_id                = "node1"
    performance_multiplier = 1  # recommended for production
}
```

Key parameters:

| Parameter | Default | Description |
|-----------|---------|-------------|
| `max_entry_size` | 1MB | Maximum size of a single Raft log entry |
| `max_transaction_size` | 8MB | Maximum size of a transactional batch |
| `snapshot_threshold` | 8192 | Commits between automatic snapshots |
| `snapshot_interval` | 120s | Time between snapshot checks |
| `trailing_logs` | 10000 | Log entries kept after snapshot |

## Single-node behavior

A single-node cluster has zero failure tolerance. The single node self-elects as leader. The
documentation "highly discourages" single-node production but it works.

**Backup strategy for single-node:** Since the data directory lives on ZFS in forge-metal:
1. `zfs snapshot pool/openbao@backup-$(date +%s)` -- instant, consistent
2. `bao operator raft snapshot save /tmp/raft.snap` -- application-level backup
3. `zfs send` to offsite (B2/R2/S3) for disaster recovery

Both approaches work. The ZFS snapshot captures the entire Raft state atomically (BoltDB uses
`mmap` + `fdatasync`, and ZFS snapshots are atomic at the block layer). The `bao operator raft
snapshot` command is the application-aware alternative that produces a portable backup file.

## Applicability to forge-metal

- Data directory at `/var/lib/openbao/raft` on the ZFS pool
- ZFS snapshots provide free, instant, consistent backups
- `zfs send` to offsite storage (same pipeline as other ZFS backups)
- No external database dependency -- fits the "single binary per service" model
- Transactional storage prevents corruption from concurrent operations
