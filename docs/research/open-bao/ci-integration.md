# CI Integration

How to inject secrets into CI jobs running in Firecracker VMs, and the state of
Forgejo Actions integration with OpenBao.

## AppRole auth method

Source: `builtin/credential/approle/` in https://github.com/openbao/openbao
Docs: https://openbao.org/docs/auth/approle/

### Authentication flow

1. Admin enables AppRole: `bao auth enable approle`
2. Admin creates a role with policies and constraints:
   `bao write auth/approle/role/ci-runner secret_id_ttl=10m token_ttl=20m token_policies=ci-read`
3. Admin reads the RoleID (static, like a username):
   `bao read auth/approle/role/ci-runner/role-id`
4. Orchestrator generates a SecretID (dynamic, like a one-time password):
   `bao write -f auth/approle/role/ci-runner/secret-id`
5. Job authenticates:
   `bao write auth/approle/login role_id=<role_id> secret_id=<secret_id>`

### SecretID security

From `validation.go`: SecretIDs are **never stored in plaintext**. They are HMAC'd with SHA-256
(`createHMAC` function) and stored by HMAC value. The `secretIDStorageEntry` struct tracks:

- `SecretIDAccessor` -- random UUID for listing/deletion (cannot be used for login)
- `SecretIDNumUses` -- usage counter (can be set to 1 for one-time use)
- `SecretIDTTL` -- expiration duration
- `CIDRList` -- source IP restrictions on SecretID usage
- `TokenBoundCIDRs` -- source IP restrictions on resulting token usage
- `Metadata` -- arbitrary key-value pairs attached to the token

`verifyCIDRRoleSecretIDSubset` enforces that SecretID CIDR blocks must be a subset of the
role's CIDR blocks.

### Response wrapping

The recommended pattern for CI: use response wrapping so the orchestrator never sees the raw
SecretID. The orchestrator receives a single-use wrapping token instead. The job unwraps it
to get the actual SecretID, then authenticates.

```bash
# Orchestrator generates wrapped SecretID
bao write -wrap-ttl=60s -f auth/approle/role/ci-runner/secret-id
# Returns: wrapping_token=hvs.xxx

# Job unwraps and authenticates
bao unwrap hvs.xxx
# Returns: secret_id=...
bao write auth/approle/login role_id=<role_id> secret_id=<secret_id>
```

### User lockout

Enabled by default: 5 failed attempts triggers 15-minute lockout. Counter resets after
15 minutes.

## Forgejo Actions integration

**Current state:** Forgejo does NOT natively support external secrets providers like
Vault/OpenBao. There is an open feature request:
https://codeberg.org/forgejo/forgejo/issues/6038

This references GitLab's implementation as a model. A related issue #2389 may partially
cover this for Actions.

### Workaround approaches

**1. OpenBao Agent as process supervisor**

Run `bao agent` in process supervisor mode on the runner. It authenticates via AppRole,
renders secrets as environment variables, then launches the actual job command. The agent
waits for all templates to render before starting the process.

**2. In-workflow API calls**

Use `curl` or `bao` CLI within the workflow to fetch secrets at job start:

```yaml
steps:
  - name: Fetch secrets
    run: |
      export BAO_ADDR=https://secrets.example.com
      export BAO_TOKEN=$(bao write -field=token auth/approle/login \
        role_id=${{ secrets.BAO_ROLE_ID }} \
        secret_id=${{ secrets.BAO_SECRET_ID }})
      echo "DB_PASSWORD=$(bao kv get -field=password secret/myapp/db)" >> $GITHUB_ENV
```

**3. Pre-populated runner secrets**

Store secrets in Forgejo's encrypted database (current model), synced from OpenBao externally
via a cron job or webhook.

## GitLab CI pattern (gold standard, API-compatible)

GitLab has the most mature CI-to-Vault integration. Since OpenBao is API-compatible, the same
patterns work:

```yaml
job_using_vault:
  id_tokens:
    VAULT_ID_TOKEN:
      aud: https://openbao.example.com
  secrets:
    DATABASE_PASSWORD:
      vault: production/db/password@secret
      token: $VAULT_ID_TOKEN
```

How it works:
1. GitLab Runner generates a JWT (OIDC ID token) for each job
2. Token contains bound claims: `project_id`, `ref`, `namespace_id`, `ref_protected`
3. OpenBao validates the JWT against the GitLab instance's OIDC discovery endpoint
4. If claims match the configured role, OpenBao returns a scoped access token
5. Runner uses that token to fetch secrets, injected as environment variables

Source: https://docs.gitlab.com/ci/secrets/hashicorp_vault/

## Firecracker VM pattern for forge-metal

The most natural pattern for forge-metal's Firecracker-based CI:

```
Host orchestrator (bare metal, root, owns AppRole credentials):
  1. Generate AppRole secret_id with num_uses=1, ttl=5m
  2. Response-wrap the secret_id (wrap-ttl=60s)
  3. Write wrapping token to a file on the zvol clone
  4. Boot Firecracker VM with the zvol as root disk

Inside VM:
  1. Read wrapping token from file, delete file
  2. bao unwrap <token> -> secret_id
  3. bao write auth/approle/login role_id=<role_id> secret_id=<secret_id>
  4. Use resulting token to read secrets needed for the build
  5. Token expires when TTL hits (20m) or VM exits
  6. VM exits -> orchestrator destroys zvol clone -> all traces gone
```

The wrapping token is single-use: if anyone intercepts it before the job, the job's unwrap
fails and the orchestrator knows the token was compromised.

**Important:** AppRole's `token_num_uses` must be `0` (unlimited) when using with `bao agent`,
because the agent doesn't track use counts internally.

### Alternative: virtio-vsock

Instead of writing the wrapping token to the zvol, pass it via virtio-vsock from host to guest.
This avoids the token touching persistent storage entirely. The guest reads from the vsock
socket, unwraps, authenticates, and the token never hits disk.

## Policy example for CI

```hcl
# ci-read.hcl -- policy for CI jobs
path "secret/data/ci/*" {
    capabilities = ["read"]
}

path "secret/data/ci/{{identity.entity.metadata.project}}/*" {
    capabilities = ["read"]
}

# Deny access to operator secrets
path "secret/data/forgejo/admin" {
    capabilities = ["deny"]
}

path "secret/data/clickstack/admin" {
    capabilities = ["deny"]
}
```

This ensures CI jobs can read CI-scoped secrets but cannot access Forgejo or ClickStack admin
credentials, which are reserved for the human operator.
