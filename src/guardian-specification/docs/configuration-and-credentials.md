# Configuration and Credentials

Guardian includes static configuration in the seed manifest and reports a
digest of the values it consumed.

```yaml
staticConfig:
  baseURL: https://gamma.guardianintelligence.org
  credentialsRef: gamma-credentials
```

`staticConfig.baseURL` is the configured external base URL for the deployment
variant described by this file. `staticConfig.credentialsRef` is an
operator-defined handle for the credential bundle required by `board` and
`fly`.

Secret values must not be embedded directly in committed config documents.
Credential bundles may resolve to operator-provided files, local secret stores,
OpenBao paths, provider authority configs, or hardware-backed stores.

Implementations resolve secret references at runtime and emit fingerprints or
stable identifiers. Command results never include secret values.
