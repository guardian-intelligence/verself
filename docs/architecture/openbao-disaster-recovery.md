# OpenBao Disaster Recovery

This document defines the offsite backup and bootstrap-from-zero recovery
architecture for site OpenBao: the bundle format, the `openbao-snapshot`
producer, the restore procedure, custody rules for key material, and the
verification model. The bootstrap state machine and steady-state secret
handling are defined in `secrets-and-integrations.md`. Release signing keys
held in OpenBao Transit are defined in `release-architecture.md`.

The recovery case covered here is complete loss of every control-plane host
in a site: all raft data, all wrapped unseal material, the on-host site root
key copy, and all host configuration. OpenBao recovers first among all
stateful systems because Nomad secret templates and every runtime credential
depend on it. PostgreSQL, ClickHouse, and TigerBeetle recovery are separate
domains.

## Recovery Model

Recovery from total site loss requires exactly two artifacts, held in
separate custody domains:

1. The latest snapshot bundle in the site recovery bucket
   (`verself-<site>-backups`, prefix `recovery/v1/openbao/`).
2. The site root key, held by the founder off-fleet.

The decryption chain is: site root key → unwraps share envelopes → Shamir
shares → master key → keyring inside the snapshot → barrier data. A raft
snapshot restored onto a fresh cluster with
`bao operator raft snapshot restore -force` reseals the cluster under the
keyring contained in the snapshot, so the snapshot is only usable with shares
from the same key epoch. Bundles embed their own epoch's envelopes, which
makes snapshot/share mispairing structurally impossible.

### Envelope construction

`openbao-up` wraps each Shamir share at init time:

- KDF: scrypt (N=32768, r=8, p=1) over SHA-256 of the site root key, with a
  fresh 32-byte salt per envelope.
- AEAD: XChaCha20-Poly1305 with the envelope version string
  (`verself.openbao.unseal-key.v1`) as associated data.

The AEAD provides bundle authenticity in addition to confidentiality. A
writer-credential holder without the root key can upload a complete
fabricated bundle, but cannot mint envelopes that unwrap under the site root
key; recovery fails at AEAD open and aborts. The residual attack with a
compromised writer credential is replay of a genuine old bundle under a new
prefix, which steers recovery to stale state. This is accepted: it is bounded
by the retention window, by snapshot cadence, and by the operator comparing
the manifest `created_at` against expectation during recovery.

### Compromise domains

| Holder of | Yields |
| --- | --- |
| Recovery bucket contents | Barrier ciphertext and envelope ciphertext. Metadata: bundle sizes, cadence, bao version. |
| Site root key alone | Nothing. |
| Bucket contents and site root key | All site secrets at every retained snapshot point. |
| A running control-plane host | All site secrets. Root key copy, envelopes, and the unsealed barrier are host-resident. The backup path adds ciphertext egress only. |
| Recovery writer credential | Bundle replay (above), garbage writes, reads of bundle ciphertext (the "Recovery bucket contents" row above — R2 has no write-only token tier; see `src/integrations/cloudflare/AGENTS.md`), and deletion or overwrite of objects older than the lock window. Within the lock window, deletion and overwrite are prevented by object lock only. |

### Loss domains

| Lost | Outcome |
| --- | --- |
| All control-plane hosts | Recover to the latest bundle. RPO equals snapshot cadence. |
| Recovery bucket | No data loss. Bundling resumes when the bucket is restored. |
| Founder's root key copy, fleet alive | Re-establish custody from `/etc/verself/bootstrap/openbao-root.key` on any control-plane host. |
| Site root key everywhere | Unrecoverable. The root key is the single irreducible secret. Its off-fleet redundancy (offline copy in a physically separate location) is a runbook obligation. |

## Bundle Format

Each bundle is one prefix in the site recovery bucket:

```text
recovery/v1/openbao/<site>/<YYYYMMDDTHHMMSSZ>-<sha256[:8]>/
  raft.snap                      # GET /v1/sys/storage/raft/snapshot
  unseal-key-1.wrapped.json      # current envelopes from the bootstrap state dir
  unseal-key-2.wrapped.json
  unseal-key-3.wrapped.json
  manifest.json                  # uploaded last; commit record
```

`manifest.json`:

```json
{
  "schema_version": 1,
  "site": "dev",
  "bao_version": "2.5.2",
  "key_shares": 3,
  "threshold": 2,
  "wrapped_key_version": "verself.openbao.unseal-key.v1",
  "created_at": "20260609T200340Z",
  "files": { "<name>": "<sha256>", "...": "..." }
}
```

Upload protocol: snapshot and envelopes first, manifest last. Object stores
have no multi-object transactions; the manifest is the commit record. A
crashed upload leaves a prefix without a manifest, which the selection
algorithm never considers. Expired debris is removed by bucket lifecycle.

Selection algorithm for recovery: list `manifest.json` keys under the site
prefix, sort lexicographically (the timestamp prefix makes this
chronological), take the newest, download the bundle, verify every file
against the manifest hashes before any restore action.

Envelopes ride in every bundle even though they change only at rekey. Each
prefix is a closed recovery unit; there is no cross-bundle epoch bookkeeping.

## Components

### openbao-snapshot

A Go binary at `src/infrastructure-components/openbao/cmd/openbao-snapshot`,
sibling of `openbao-up`, run as a Nomad periodic batch task (`raw_exec`,
root, on the OpenBao host) declared in the openbao component's `nomad.hcl`.
Per run:

1. Authenticate via Nomad workload identity (`jwt-nomad`). The runtime
   catalog role `openbao-snapshot-runtime` grants `read` on
   `sys/storage/raft/snapshot` and `read` on the KV path holding the
   recovery-writer R2 credential. The snapshot endpoint requires no sudo
   capability.
2. Stream `GET /v1/sys/storage/raft/snapshot`.
3. Read the envelopes from `/var/lib/verself/bootstrap/openbao/`. A missing
   envelope aborts the run; a snapshot-only bundle must never be written.
4. Upload the bundle manifest-last using SigV4 over `http.Client` with
   path-style URLs, the construction already used by
   `object-storage-service/internal/objectstorage/r2.go`. Endpoint, region,
   bucket, prefix, and credentials are configuration; Cloudflare R2 requires
   region `auto`, Garage requires its configured `s3_region`.
5. Write one evidence row to ClickHouse per attempt: site, outcome
   (`uploaded`, `failed`, `standby_skipped`), snapshot sha256, bytes,
   duration, bundle prefix.

Hourly cadence. Snapshots for this deployment are tens of kilobytes; single
PUTs suffice and multipart is not used.

### openbao-up restore action

`openbao-up --action=restore` automates the proven sequence against a fresh
converged host:

1. Download and verify the selected bundle.
2. Throwaway `operator init` (1 share, threshold 1) and unseal.
3. `bao operator raft snapshot restore -force` with the throwaway root
   token. The node reseals under the snapshot keyring.
4. Install the bundle envelopes into the bootstrap state dir and run the
   existing bootstrap unseal path (unwrap with the site root key, unseal).

Workload-identity reconciliation is excluded from the restore action.
OpenBao validates the `jwt-nomad` JWKS URL at configuration time, so
reconciliation runs only after the site Nomad is serving
`/.well-known/jwks.json`. Restored `jwt-nomad` configuration references the
previous Nomad cluster's keys and must be reconciled before runtime jobs can
log in.

### Recovery bucket

The bucket and its policies are OpenTofu-owned
(`src/tools/provisioning/terraform`):

- Object lock on `recovery/v1/` (35 days). Bundles are immutable within the
  window, including against the writer credential.
- Lifecycle rule expiring `recovery/v1/openbao/` objects after the lock
  window. No deletion code exists in the system; the writer credential does
  hold delete capability (see below), which the lock window neutralizes for
  retained bundles.
- Writer credential: an R2 Object Read & Write token scoped to the site
  recovery bucket — R2 offers no write-only or PUT-only tier
  (`src/integrations/cloudflare/AGENTS.md`). Integrity comes from the bucket
  lock plus manifest-last upload, not token permissions; ciphertext reads by
  the writer credential are accepted residual exposure. Minted and rotated by
  `cloudflare-control-plane ensure-recovery` / `rotate-recovery`, stored in
  site OpenBao KV for the snapshot job.
- Reader credential: minted at recovery time by the operator from the
  Cloudflare control plane. Every stored copy of a reader credential lives
  inside the system being restored, so recovery documentation assumes none
  exists.

## Recovery Procedure

For a site whose hosts are completely lost:

1. Provision hosts (`src/tools/provisioning`), `aspect site root-handoff`,
   `aspect site converge-host`. Convergence installs the site root key at
   `/etc/verself/bootstrap/openbao-root.key`, regenerates OpenBao bootstrap
   TLS, and creates `/var/log/openbao`. These are host-local materials,
   intentionally outside the bundle.
2. Mint a recovery reader credential from the Cloudflare control plane.
3. `openbao-up --action=restore` (bundle selection, hash verification,
   throwaway init, force restore, unseal from envelopes).
4. Start Nomad, then run workload-identity reconciliation
   (`openbao-up` bootstrap path) to repoint `jwt-nomad` at the new cluster's
   JWKS and reconcile roles.
5. `aspect site bootstrap-deploy`. Credentials imported into OpenBao before
   the snapshot (Stripe, GitHub App material, Cloudflare child tokens,
   Transit signing keys) are present without provider-side re-import.
   Credentials rotated after the snapshot re-rotate through their normal
   rotation actions.

A restored snapshot reproduces all state as of snapshot time. Tokens and
leases valid at snapshot time return alive, including any revoked
afterwards; tokens minted after the snapshot are absent, and services
re-authenticate through workload identity. Snapshot cadence therefore bounds
credential resurrection as well as data loss.

## Key Lifecycle Rules

- Rekey (`bao operator rekey`) changes the Shamir shares. Rekey is performed
  only through an owned action that atomically rewraps the on-disk
  envelopes; ad hoc rekey creates a window in which the bundler pairs a
  new-keyring snapshot with stale envelopes, producing authentic-looking
  unrecoverable bundles. Until the owned action exists, rekey is prohibited.
  The recurring restore rehearsal detects an incoherent bundle within one
  cycle.
- Site root key rotation rewraps the on-disk envelopes under the new key.
  Bundles already in the bucket remain wrapped under the previous key; the
  founder retains the previous root key until those bundles age out of
  retention.
- Bundles before a rekey remain recoverable with their embedded envelopes
  and the root key effective at their creation.

## Verification

- Evidence: every `openbao-snapshot` attempt writes a ClickHouse row.
  Alerting keys on the absence of recent success rows for a site rather
  than on failure rows, because a wedged job emits nothing.
- Restore rehearsal: a recurring gameday restores the latest bundle of a
  site onto clean state and compares a KV canary and the Transit canary
  public key against recorded values. Bundles restore only within their own
  site; restoring a prod bundle onto a lower-trust host moves prod secrets
  across an isolation boundary.
- Hermetic CI test: a Bazel `go_test` in the openbao component runs the full
  lifecycle against the Bazel-pinned `bao` and a Bazel-pinned Garage
  (v2.3.0, single-node): init, seed canaries, bundle, wipe the data
  directory, restore, assert canaries. Garage validates SigV4 and rejects
  bad signatures, so signing regressions fail the test. R2-only behavior
  remains on the live gameday: object lock semantics (Cloudflare bucket
  configuration, outside the S3 API), region `auto`, and the
  `ensure-recovery` credential lifecycle.

## Multi-Node Topology

With three control-plane nodes per site:

- Snapshots are taken from the active node. The snapshot job runs on one
  node; when local OpenBao is a standby, the run records `standby_skipped`
  and exits. Cadence-based absence alerting covers the case where no node
  uploads.
- Envelopes are identical ciphertext across nodes and are replicated to each
  node's bootstrap state dir so any node can unseal after a restart.
- Restore brings up a single node from the bundle; remaining nodes join as
  raft peers and replicate from it.

The unseal trust model is unchanged by node count. Raw shares are never
distributed across fleet hosts: the fleet is a single failure domain (total
wipe loses all copies simultaneously) and a single compromise domain
(threshold shares on hosts would let two host compromises bypass the root
key).

## Rehearsal Evidence

Rehearsed on site dev (vs-dev-w0), 2026-06-09, with the Bazel-built
production binaries (`bao` 2.5.2, `openbao-up`):

- Two complete wipe-and-restore cycles. Cycle one recovered from
  controller-held bundle bytes; cycle two recovered through a Garage v2.3.0
  S3 endpoint with SigV4 (manifest-last upload, newest-manifest selection,
  hash verification, 403 on bad signature).
- Post-restore in both cycles: KV canary identical, Transit Ed25519 canary
  public key byte-identical and non-exportable, signing operational, audit
  device intact, `generate-root-token` recovery path functional with all
  temporary tokens revoked.
- Force-restore onto a freshly initialized cluster demanded the snapshot's
  keyring epoch, confirming the envelope-pairing requirement.
- The snapshot endpoint served a least-privilege token holding only `read`
  on `sys/storage/raft/snapshot`, expressible in the existing runtime
  catalog schema.
- `sys/storage/raft/snapshot-auto` is absent in OpenBao 2.5.2; upstream
  tracks it as openbao/openbao#795. When it lands, the producer reduces to
  reconciled configuration plus envelope upload.

Open items: live R2 leg (blocked on a dev recovery credential), the Garage
Bazel pin (sha256 recorded:
`f98d317942bb341151a2775162016bb50cf86b865d0108de03eb5db16e2120cd` for the
v2.3.0 x86_64-musl static binary), the `openbao-up` restore action, the
snapshot job and catalog role, the ClickHouse evidence schema, and the owned
rekey action.
