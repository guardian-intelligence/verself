# OpenBao Recovery

OpenBao is the first root-of-trust recovery target after a boarded host has a
minimal executor. The OpenBao infrastructure component owns installation,
status detection, snapshot restore, initialization, unseal, baseline
configuration, and health reporting.

Guardian reports OpenBao blockers through component conditions. The base
protocol does not encode OpenBao-specific recovery fields.

## Inputs

OpenBao recovery consumes:

- boarded repo artifacts, including the repo-bundled OpenBao binary and
  recovery binary;
- the OpenBao component CRD, including component-owned config templates,
  backup policy, recovery policy, and baseline policy;
- offsite encrypted Raft snapshots and signed manifests when available;
- operator-held Shamir unseal material or a working external seal;
- operator PGP recipient identities for fresh initialization;
- desired baseline mounts, policies, auth methods, and secret paths owned by
  the OpenBao component.

Provider credentials such as Cloudflare account authority are imported into
OpenBao after OpenBao is initialized and unsealed. They are not substrate files.

## Restart Model

OpenBao recovery distinguishes process restart, host reboot, restored storage,
and fresh initialization.

An initialized site must restart without a stored root token. The autonomous
restart path is a configured seal that can unseal OpenBao at process start, such
as an HSM, KMS, KMIP, TPM-bound mechanism, or other component-owned auto-unseal
mechanism. Shamir material is recovery authority for manual unseal and emergency
breakglass operations; it is not the steady-state restart mechanism for an
autonomous site.

Fresh initialization is the only path that receives the initial root token from
OpenBao. The recovery process uses that token in memory to reconcile the initial
baseline and revokes it before reporting recovery complete. The initial root
token is not written to init material, Guardian resource graphs, reports, logs,
Nomad task arguments, environment variables, or durable host files.

## State Machine

```text
absent
  -> install OpenBao binary/config from boarded repo
  -> start server
  -> read status

uninitialized + snapshot available
  -> initialize temporary restore target if required
  -> unseal temporary restore target for authenticated restore
  -> force-restore Raft snapshot
  -> report OpenBaoServerRestartRequired/AfterSnapshotRestore
  -> restart server
  -> unseal with material matching the restored snapshot
  -> report available

uninitialized + no snapshot
  -> require operator PGP recipient identities
  -> initialize fresh and keep the initial root token and unseal shares in memory
  -> emit encrypted init material to operators/offsite escrow
  -> unseal with the in-memory threshold shares
  -> reconcile baseline with the in-memory initial root token
  -> revoke the initial root token
  -> report available only after required baseline state is reconciled

initialized + sealed
  -> unseal through configured auto-unseal, or obtain threshold material for
     manual unseal
  -> reconcile baseline only through scoped workload identity or explicit
     operator token
  -> otherwise report OpenBaoBaselineReconciled=False
  -> report available only after required baseline state is reconciled

initialized + unsealed
  -> reconcile baseline only through scoped workload identity or explicit
     operator token
  -> otherwise report OpenBaoBaselineReconciled=False
  -> report available only after required baseline state is reconciled

baseline reconciliation requested by OpenBao component CRD
  -> prefer scoped workload-authenticated authority
  -> otherwise require an explicit transient operator token
  -> reconcile mounts/policies/auth/secret paths
  -> revoke the transient recovery token when one was used

breakglass generate-root
  -> only when operators explicitly enable OpenBao's deprecated generate-root
     endpoints for a short emergency window
  -> require threshold unseal material over stdin
  -> generate a transient root token
  -> reconcile or repair the emergency target
  -> revoke the generated token and disable the endpoint again
```

## Mechanical Recovery

The recovery binary performs these steps:

1. Install the repo-bundled OpenBao runtime into the component runtime root.
2. Create the `openbao` user, storage directories, TLS files, and config with
   deterministic ownership and permissions.
3. Start or restart the OpenBao server.
4. Run `bao status -format=json`.
5. Branch on `initialized` and `sealed`.
6. Restore a snapshot when the component backup manifest selects one.
7. Initialize fresh only when no usable snapshot is selected.
8. Encrypt fresh init unseal shares for operators or offsite escrow before
   using them to unseal.
9. Reconcile baseline mounts, policies, audit devices, auth methods, and
   workload identity only when the component CRD requests that operation and
   sufficient transient authority is present.
10. Revoke transient root or operator credentials before reporting recovery
   complete.
11. Report availability once OpenBao is initialized, unsealed, and any required
   baseline state is reconciled.
12. Emit a recovery report that contains status, digests, fingerprints, and
    conditions.

## Snapshot Restore

OpenBao integrated storage snapshots are component-owned backup artifacts. The
component selects and retrieves the snapshot from offsite storage using
component-owned backup configuration.

When restoring a snapshot onto a clean node, the recovery flow may need to
initialize and unseal a temporary target, then authenticate with its transient
root token before force-restoring the snapshot. After restore, the component
reports `OpenBaoServerRestartRequired=True/AfterSnapshotRestore`; the recovered
OpenBao data then requires the original snapshot's unseal material or the
original external seal.

The snapshot manifest records:

- snapshot digest;
- OpenBao version;
- seal mode;
- backup timestamp;
- source cluster identity;
- storage backend;
- signing key identity;
- backup object location;
- operator-visible restore warnings.

The manifest does not contain unseal shares, root tokens, provider credentials,
or backup decrypt keys.

## Fresh Initialization

Fresh initialization is used only when no usable snapshot is selected.

The recovery binary receives OpenBao initialization output only inside the
process. It immediately encrypts each unseal share or recovery share for the
configured operator PGP recipient, writes the encrypted handoff, unseals
OpenBao with the in-memory threshold material when required, reconciles baseline
state with the in-memory initial root token, and revokes that token.

The encrypted handoff contains no root token. The initial root token is only a
transient bootstrap capability for the same process that created it.

For destructive disaster-recovery drills, the operator wipes the OpenBao data
directory before running the control loop. The OpenBao CRD describes the desired
cluster and fresh-initializes only when the observed storage is uninitialized.

Encrypted init material is delivered to operators or offsite escrow and removed
from the host after delivery. The recovery report records recipient
fingerprints and delivery status.

## Conditions

OpenBao recovery uses these condition reasons:

- `UnsealQuorumIncomplete`: threshold Shamir material has not been supplied;
- `ExternalSealUnavailable`: configured auto-unseal backing authority is not
  reachable;
- `InitRecipientIdentityRequired`: fresh init needs operator PGP recipients;
- `BackupRetrievalAuthorityRequired`: the component cannot retrieve the selected
  offsite snapshot;
- `BreakglassGenerateRootFailed`: threshold recovery material did not produce a
  transient breakglass token;
- `BaselineAuthorityRequired`: baseline reconciliation needs operator
  authority;
- `BaselineAuthorityInsufficient`: presented operator authority cannot reconcile
  baseline state.

## Baseline Reconciliation

After OpenBao is unsealed, recovery may reconcile component baseline state:

- audit logging;
- KV mounts;
- transit mounts when used;
- policies;
- workload identity auth;
- token roles and TTLs;
- component secret paths;
- provider credential metadata.

The autonomous production path is scoped workload-authenticated reconciliation:
Nomad or SPIFFE workload identity authenticates to the already-unsealed OpenBao
cluster, receives a narrowly scoped token for baseline reconciliation, applies
the desired baseline, and no-ops when state is current. This path is not fully
implemented yet for gamma; fresh initialization currently reconciles baseline
with the process-local initial root token, while an already-initialized store
without scoped workload authority reports `OpenBaoBaselineReconciled=False`.

The operator path is stdin-based:

```sh
openbao-recover recover \
  --repo-root=/home/ubuntu/.local/state/guardian/repo/current \
  --resource-graph=/home/ubuntu/.local/state/guardian/repo/current/workspace/.guardian/fly/document.json \
  --resource-name=openbao \
  --operator-token-stdin < <operator-token-file>
```

Generate-root is breakglass-only. OpenBao documents unauthenticated
generate-root endpoints as deprecated and disabled by default starting in
v2.5.3, so this path must not be part of routine recovery. Operators may use it
only during an emergency window where the listener is explicitly configured to
allow the endpoint, then disabled again.

```sh
openbao-recover recover \
  --repo-root=/home/ubuntu/.local/state/guardian/repo/current \
  --resource-graph=/home/ubuntu/.local/state/guardian/repo/current/workspace/.guardian/fly/document.json \
  --resource-name=openbao \
  --breakglass-generate-root-token-stdin < <unseal-shares-file>
```

The recovery binary starts a generate-root attempt, submits shares from stdin,
uses OpenBao's decode-token endpoint to decode the returned token, reconciles
baseline state, and then calls `auth/token/revoke-self`. Incomplete attempts
are canceled.

The token is not accepted through argv or environment variables. A successful
baseline run attempts `auth/token/revoke-self` before reporting recovery
complete. If the presented token lacks system authority, recovery reports
`OpenBaoBaselineReconciled=False/BaselineAuthorityInsufficient` and leaves
baseline reconciliation blocked.

Provider credentials are imported or rotated through component-owned
operator paths. Provider integrations report their own import blocker when a
provider parent credential is required and no restored secret exists.

## References

- OpenBao seal/unseal: https://openbao.org/docs/concepts/seal/
- OpenBao operator init: https://openbao.org/docs/2.3.x/commands/operator/init/
- OpenBao generate-root command: https://openbao.org/docs/commands/operator/generate-root/
- OpenBao generate-root API: https://openbao.org/api-docs/system/generate-root/
- OpenBao unauthenticated generate-root deprecation notice: https://openbao.org/community/deprecation/unauthed-generate-root/
- OpenBao decode-token API: https://openbao.org/api-docs/2.3.x/system/decode-token/
- OpenBao Raft snapshot API: https://openbao.org/api-docs/2.3.x/system/storage/raft/
- Vault snapshot restore sequence: https://developer.hashicorp.com/vault/docs/sysadmin/snapshots/restore
