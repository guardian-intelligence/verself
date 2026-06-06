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
  -> unseal with original snapshot trust material
  -> report available

uninitialized + no snapshot
  -> require operator PGP recipient identities
  -> initialize fresh with PGP-encrypted unseal shares
  -> configure baseline mounts/policies/auth with the transient root token
  -> revoke the transient root token
  -> emit encrypted init material to operators/offsite escrow

initialized + sealed
  -> obtain threshold unseal material from external trust source
  -> unseal
  -> report available

initialized + unsealed
  -> report available

baseline reconciliation requested by OpenBao component CRD
  -> require a transient operator token or freshly-created root token
  -> reconcile mounts/policies/auth/secret paths
  -> revoke transient root token when created during fresh init
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
8. Unseal or block on `RootTrustMaterialAvailable=False`.
9. Report availability once OpenBao is initialized and unsealed.
10. Reconcile baseline mounts, policies, audit devices, auth methods, and
   workload identity only when the component CRD requests that operation and
   the required root authority is present.
11. Revoke transient root credentials.
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

The recovery binary initializes OpenBao with PGP-encrypted unseal shares for
operator recipients. The initial root token is held only in process memory long
enough to configure baseline mounts, policies, audit devices, and auth methods,
then revoked. If the implementation cannot keep the root token ephemeral, it
reports `RootTrustMaterialAvailable=False` with
`OperatorRootCredentialsRequired`.

Encrypted init material is delivered to operators or offsite escrow and removed
from the host after delivery. The recovery report records recipient
fingerprints and delivery status.

## Root Trust Conditions

OpenBao recovery uses these condition reasons:

- `UnsealQuorumIncomplete`: threshold Shamir material has not been supplied;
- `ExternalSealUnavailable`: configured auto-unseal backing authority is not
  reachable;
- `SnapshotTrustMaterialMismatch`: supplied material does not match the restored
  snapshot;
- `InitRecipientIdentityRequired`: fresh init needs operator PGP recipients;
- `BackupRetrievalAuthorityRequired`: the component cannot retrieve the selected
  offsite snapshot;
- `OperatorRootCredentialsRequired`: a human root operation is required.

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

Provider root credentials are imported or rotated through component-owned
operator paths. OpenBao recovery reports
`RootTrustMaterialAvailable=False/ProviderRootCredentialRequired` when a
provider parent credential is required and no restored secret exists.

## References

- OpenBao seal/unseal: https://openbao.org/docs/concepts/seal/
- OpenBao operator init: https://openbao.org/docs/2.3.x/commands/operator/init/
- OpenBao Raft snapshot API: https://openbao.org/api-docs/2.3.x/system/storage/raft/
- Vault snapshot restore sequence: https://developer.hashicorp.com/vault/docs/sysadmin/snapshots/restore
