# Root Trust Material

Root trust material is external authority required to continue recovery. It is
held outside the recovered host and outside committed Guardian resource graphs.

Examples:

- OpenBao Shamir unseal shares;
- OpenBao recovery shares when auto-unseal is used;
- an HSM/KMS seal that can decrypt the OpenBao barrier;
- operator PGP recipient identities for encrypted init output;
- provider parent credentials that must be imported from the provider control
  plane;
- backup retrieval authority for encrypted offsite snapshots.

## Condition Contract

Components report root-trust readiness with the standard condition type
`RootTrustMaterialAvailable`.

```yaml
type: RootTrustMaterialAvailable
status: "False"
reason: UnsealQuorumIncomplete
resource: openbao
message: threshold unseal material is required to unseal restored OpenBao
```

Common reasons:

- `OperatorRootCredentialsRequired`;
- `UnsealQuorumIncomplete`;
- `ExternalSealUnavailable`;
- `SnapshotTrustMaterialMismatch`;
- `InitRecipientIdentityRequired`;
- `ProviderRootCredentialRequired`;
- `BackupRetrievalAuthorityRequired`.

`RootTrustMaterialAvailable=True` means the component has enough external
authority to continue its recovery action. It does not mean the component is
fully recovered.

## Handling Rules

Root trust material MUST NOT be written to durable host storage.

Root trust material MUST NOT be passed in argv or environment variables.

Root trust material MUST NOT appear in command output, logs, traces, ClickHouse
events, scheduler events, or recovery reports.

Interactive or future programmatic operator entry must bind the prompt to
verifiable operation facts:

- target host identity;
- repo upload digest;
- recovery binary digest;
- component name and action;
- OpenBao seal status;
- snapshot digest when restoring;
- operator recipient fingerprints when initializing;
- whether the submitted value is consumed through stdin or another ephemeral
  channel.

## Evidence

Recovery evidence records identifiers and digests:

- condition type, status, and reason;
- component resource name;
- snapshot digest and timestamp;
- OpenBao version;
- seal mode;
- PGP recipient fingerprints;
- provider credential fingerprint;
- root-token or operator-token revocation status, including
  `OpenBaoGeneratedRootToken` and `OpenBaoOperatorTokenRevoked`.

Evidence records MUST NOT include plaintext shares, root tokens, provider API
tokens, private keys, or backup decrypt keys.
