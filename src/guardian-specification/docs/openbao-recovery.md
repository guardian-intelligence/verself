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
  -> emit encrypted init material to operators/offsite escrow
  -> wait for operator-held root trust material

initialized + sealed
  -> obtain threshold unseal material from external trust source
  -> unseal
  -> if baseline reconciliation is requested and threshold material was
     presented for generate-root, generate a transient root token
  -> reconcile baseline or report root authority required
  -> report available only after required baseline state is reconciled

initialized + unsealed
  -> reconcile baseline or report root authority required
  -> report available only after required baseline state is reconciled

baseline reconciliation requested by OpenBao component CRD
  -> require a transient operator token or threshold unseal material
  -> optionally generate a transient root token through OpenBao generate-root
  -> reconcile mounts/policies/auth/secret paths
  -> revoke transient root token or presented operator token
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
operator recipients. The initial root token is encrypted for the configured
operator recipient and included in the encrypted init-material handoff. The
recovery report does not contain the root token or plaintext unseal shares.

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
- `GenerateRootFailed`: threshold unseal material did not produce a transient
  root token;
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

The operator path is stdin-based:

```sh
openbao-recover recover \
  --repo-root=/home/ubuntu/.local/state/guardian/repo/current \
  --resource-graph=/home/ubuntu/.local/state/guardian/repo/current/workspace/.guardian/fly/document.json \
  --resource-name=openbao \
  --operator-token-stdin < <operator-token-file>
```

When the operator has threshold unseal material instead of an existing root
token, recovery can generate a transient root token through OpenBao's
generate-root flow. The same command works for an initialized and sealed node:
the recovery binary first unseals OpenBao with the presented shares, then uses
those shares to authorize generate-root before reconciling baseline state.

```sh
openbao-recover recover \
  --repo-root=/home/ubuntu/.local/state/guardian/repo/current \
  --resource-graph=/home/ubuntu/.local/state/guardian/repo/current/workspace/.guardian/fly/document.json \
  --resource-name=openbao \
  --generate-root-token-stdin < <unseal-shares-file>
```

The recovery binary starts a generate-root attempt, submits shares from stdin,
uses OpenBao's decode-token endpoint to decode the returned token, reconciles
baseline state, and then calls `auth/token/revoke-self`. Incomplete attempts
are canceled.

The token is not accepted through argv or environment variables. A successful
baseline run attempts `auth/token/revoke-self` before reporting recovery
complete. If the presented token lacks system authority, recovery reports
`RootTrustMaterialAvailable=False/OperatorRootCredentialsRequired` and leaves
baseline reconciliation blocked.

Provider root credentials are imported or rotated through component-owned
operator paths. OpenBao recovery reports
`RootTrustMaterialAvailable=False/ProviderRootCredentialRequired` when a
provider parent credential is required and no restored secret exists.

## References

- OpenBao seal/unseal: https://openbao.org/docs/concepts/seal/
- OpenBao operator init: https://openbao.org/docs/2.3.x/commands/operator/init/
- OpenBao generate-root command: https://openbao.org/docs/commands/operator/generate-root/
- OpenBao generate-root API: https://openbao.org/api-docs/system/generate-root/
- OpenBao decode-token API: https://openbao.org/api-docs/2.3.x/system/decode-token/
- OpenBao Raft snapshot API: https://openbao.org/api-docs/2.3.x/system/storage/raft/
- Vault snapshot restore sequence: https://developer.hashicorp.com/vault/docs/sysadmin/snapshots/restore
