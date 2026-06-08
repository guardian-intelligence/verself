# OpenBao Recovery

OpenBao is a preflight root service. Guardian does not manage OpenBao as a
Nomad job. Preflight installs the repo-bundled OpenBao runtime, prepares the
host integration, starts the systemd service, and runs `openbao-recover` until
OpenBao is initialized and unsealed or a concrete blocker is reported.

OpenBao recovery owns only root-service trust-store bootstrap: OpenBao runtime,
KV v2 mounts implied by declared `SecretPath` resources, and encrypted
operator-import token handoff for `source: operatorImport` paths during fresh
initialization. Provider imports, generated runtime secret values, secret
readers, auth methods, and service-specific reconciliation belong to the owning
component's Nomad job and component binary.

## State Machine

```text
absent
  -> install OpenBao runtime and config from the boarded repo
  -> start OpenBao
  -> read status

uninitialized + snapshot available
  -> verify snapshot manifest and bytes
  -> initialize and unseal a temporary restore target when needed
  -> force-restore the Raft snapshot
  -> report OpenBaoServerRestartRequired/AfterSnapshotRestore
  -> restart OpenBao
  -> unseal with material matching the restored snapshot
  -> report recovered

uninitialized + no snapshot
  -> require one operator PGP recipient per Shamir share
  -> initialize fresh
  -> keep initial root token and unseal shares in memory only
  -> write encrypted init material without the root token
  -> unseal with the in-memory threshold shares
  -> enable KV v2 mounts implied by SecretPath resources
  -> create encrypted guardian-operator-import handoff when operator imports exist
  -> revoke the initial root token
  -> report recovered

initialized + sealed
  -> unseal through configured auto-unseal or stdin threshold material
  -> report recovered when unsealed
  -> otherwise report WaitingForUnseal

initialized + unsealed
  -> report available
```

## Root Token Rule

Fresh initialization is the only path that sees the initial root token. The
token is process-local, never written to init material, reports, argv, env,
logs, resource graphs, or durable host files, and is revoked before recovery
reports success.

Initialized stores must restart without a stored root token. Autonomous restart
requires an external seal or equivalent root-of-trust mechanism. Shamir material
is breakglass/manual recovery authority, not the normal restart path.

## Snapshot Rule

OpenBao snapshots are component-owned backup artifacts. A restore manifest
records nonsecret evidence such as snapshot digest, byte count, OpenBao version,
seal mode, source identity, and object location. It never contains unseal
shares, root tokens, provider credentials, or decrypt keys.

After force-restore, OpenBao must be restarted and unsealed with material that
matches the restored data.

## Conditions

OpenBao recovery reports concrete component conditions:

- `OpenBaoRuntimePrepared`
- `OpenBaoServerReady`
- `OpenBaoSnapshotVerified`
- `OpenBaoSnapshotRestored`
- `OpenBaoServerRestartRequired`
- `OpenBaoInitialized`
- `OpenBaoInitMaterialDelivered`
- `OpenBaoUnsealed`
- `OpenBaoSecretStoreBootstrapped`
- `OpenBaoTransientTokenRevoked`
- `OpenBaoRecoveryComplete`

Provider credentials and generated service-specific secret values are reported
by the components that own those dependencies after OpenBao is available.
