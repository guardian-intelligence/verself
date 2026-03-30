# Backup and Recovery

Procedures for backing up and restoring a single-node OpenBao deployment with Raft storage
on ZFS.

## What you need to recover

| Item | Required | Where it lives |
|------|----------|---------------|
| Seal key file | Yes | `/etc/openbao/unseal.key` (SOPS-encrypted in repo) |
| Raft data | Yes | Either a Raft snapshot or ZFS snapshot of `/var/lib/openbao/raft/` |
| OpenBao config | Yes | HCL config file (in repo, deployed by Ansible) |
| Recovery keys | Optional | Only needed for `operator generate-root` |

Without the seal key, the data is permanently inaccessible. Without the data, the seal key
is useless. Both are required.

## Raft on-disk layout

```
/var/lib/openbao/raft/
  vault.db      # BoltDB FSM (all encrypted KV data)
  raft.db       # BoltDB log store (Raft WAL entries)
  snapshots/    # Internal Raft snapshot directory
```

## Raft snapshots

### Save

```bash
bao operator raft snapshot save /var/backups/openbao/bao-$(date +%F-%H%M).snap
```

Requires `read` on `sys/storage/raft/snapshot`. Taken from the leader node (forwarded
automatically on multi-node).

**Format:** gzip-compressed tar archive containing:
1. Metadata -- Raft log index, term, cluster configuration
2. State data -- full BoltDB FSM contents (all encrypted KV data)
3. SHA-256 checksum for integrity verification

Source: https://github.com/hashicorp/raft-snapshot/blob/master/snapshot.go

### Restore (normal)

```bash
bao operator raft snapshot restore /path/to/backup.snap
```

Checks that the seal keys are consistent with the snapshot. Fails if keys don't match.

### Restore (force)

```bash
bao operator raft snapshot restore -force /path/to/backup.snap
```

Bypasses the Raft-level key consistency check. Required when restoring to a fresh,
newly-initialized cluster. **The snapshot data is still encrypted with the original seal** --
the new server must have the same static key file.

### Automation

**OpenBao CE does NOT have built-in automated snapshots.** This is a Vault Enterprise feature.
Open feature request: https://github.com/openbao/openbao/issues/795

**Community tool:** `openbao/openbao-snapshot-agent` (43 commits, 7 releases as of March 2026).
Uses AppRole auth, supports S3-compatible storage and local filesystem.

Source: https://github.com/openbao/openbao-snapshot-agent

**Simple cron alternative (recommended for forge-metal):**

```bash
#!/bin/sh
# /usr/local/bin/bao-snapshot
BAO_ADDR="http://127.0.0.1:8200" \
BAO_TOKEN="$(cat /etc/openbao/snapshot-token)" \
bao operator raft snapshot save "/var/backups/openbao/bao-$(date +%F-%H%M).snap"

# Retention: delete snapshots older than 7 days
find /var/backups/openbao/ -name "*.snap" -mtime +7 -delete
```

## ZFS snapshot consistency

**ZFS snapshots of the Raft data directory are crash-consistent.** Here's why:

BoltDB's write path uses a two-phase commit:
1. Dirty pages written via `write()`, then `fdatasync()` to flush
2. New meta page (with incremented txid and checksum) written, then `fdatasync()`

If a crash occurs between phases, partially-written data is ignored because the meta page
wasn't committed. If the meta page itself is partial, its checksum invalidates it. BoltDB
states: "does not require recovery in the event of a system crash."

ZFS snapshots are atomic at the block layer. A `zfs snapshot` captures all blocks committed
by completed `fdatasync()` calls at that instant. The result:

- **Completed BoltDB transactions**: guaranteed in the snapshot
- **In-flight transactions**: rolled back on open (meta page invalid or missing)
- **Both `vault.db` and `raft.db`**: captured at the same instant (same dataset)

There is **no separate WAL file** to worry about (unlike PostgreSQL or SQLite). BoltDB stores
everything in the same file. Raft log is also a BoltDB file.

At worst, the snapshot loses the last in-flight transaction. On single-node, Raft replays from
the log. This is the same guarantee as a power failure.

## Disaster recovery scenarios

### Scenario A: Node dies, have Raft snapshot + seal key

1. Provision new server, install OpenBao with same config and seal key
2. Start OpenBao (uninitialized)
3. Initialize: `bao operator init -recovery-shares=1 -recovery-threshold=1`
4. Authenticate with temporary root token
5. Restore: `bao operator raft snapshot restore -force /path/to/backup.snap`
6. OpenBao restarts, auto-unseals via static key
7. Verify: `bao status`

### Scenario B: Node dies, have ZFS snapshot + seal key

1. Provision new server with ZFS
2. `zfs receive` the snapshot into the new pool
3. Deploy same OpenBao config and seal key
4. Point `storage.raft.path` at the received dataset
5. Start OpenBao -- finds existing data, auto-unseals, comes up with all data intact
6. Single-node cluster self-elects as leader

### Scenario C: Lost seal key, have data

**Unrecoverable.** The data is encrypted and cannot be decrypted without the seal key.

### Scenario D: Lost data, have seal key

**Unrecoverable.** The seal key alone cannot recreate the encrypted data. All secrets must be
re-entered.

### Recovery mode (corrupted but accessible data)

Start with `-recovery` flag:
- Auto-resizes Raft cluster to 1 node (bypasses quorum)
- Disables all subsystems (expiration, clustering, RPCs)
- Requires generating a recovery token
- Provides raw access via `sys/raw` endpoints

Source: https://openbao.org/docs/concepts/recovery-mode/

## Recommended backup strategy for forge-metal

**Two-layer approach:**

1. **ZFS snapshots (high-frequency, local):**
   ```bash
   # Every 15 minutes via systemd timer
   zfs snapshot pool/openbao@auto-$(date +%s)
   # Retain last 96 snapshots (24 hours)
   ```
   Instant, zero-cost, consistent. First line of defense against operator error.

2. **Raft snapshots (daily, offsite):**
   ```bash
   # Daily via cron
   bao operator raft snapshot save /var/backups/openbao/daily.snap
   # zfs send to B2/R2/S3
   ```
   Portable, application-aware. Second line of defense against hardware failure.

The seal key itself is backed up via SOPS in the git repository. As long as the repo and
at least one backup exist, recovery is possible.

## Seal migration (Shamir to static key)

If you accidentally initialize with Shamir and want to switch to static seal:

1. Generate static key file
2. Add `seal "static"` stanza to config
3. Restart OpenBao (starts sealed)
4. Unseal with migrate flag: `bao operator unseal -migrate <shamir-key>`
5. Root key is re-encrypted from Shamir to static seal
6. Old Shamir keys become recovery keys
7. Subsequent restarts: auto-unseal via static key

**Recommendation: initialize directly with static seal.** No reason to start with Shamir
and migrate later. Configure `seal "static"` before `bao operator init`.

Source: https://openbao.org/docs/concepts/seal/
