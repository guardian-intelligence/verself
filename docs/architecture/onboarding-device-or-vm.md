# Onboarding devices and workloads

Cert-authenticated SSH for arbitrary operator laptops and ephemeral
workload VMs (Devin, Cursor, CI runners) without exposing port 22 to
the public internet.

## Trust model

- CUE declares trusted devices (`operators.<name>.devices.<device>`)
  and the workload-pool capacity (`workloads.pool.slot_count`). All
  WireGuard peers in `wg-ops` are projected from this set; nothing
  writes peers to the kernel outside of the convergence loop.
- OpenBao runs the SSH CA. Two auth methods front it:
  - **OIDC** at `auth/oidc-ssh-ca`, bound to Zitadel. Mints periodic
    Vault tokens (`token_period=14d`, `token_max_ttl=30d`) scoped to
    `ssh-ca/sign/operator`. Used by humans on interactive devices.
  - **AppRole** at `auth/approle/workload-enrollment`. `secret_id_ttl=15m`,
    `secret_id_num_uses=1`, `token_ttl=24h`, `token_max_ttl=24h`,
    scoped to `ssh-ca/sign/workload`. Used by ephemeral VMs.
- sshd binds to the wg-ops interface only (`10.66.66.1`). Public `:22`
  is dropped at nftables. `AuthorizedKeysFile=none`; the only
  admissible credential is a cert signed by the OpenBao CA whose
  `valid_principals` intersects `/etc/ssh/principals/ubuntu`.
- Every signed cert carries a stamped KeyID: `verself-<principal>-<device-or-slot>`.
  sshd records the KeyID into journald on every accept; the
  `verself.host_auth_events` MV materialises it as `cert_id`.

## Discovery surface

Caddy serves three immutable, public-by-design artifacts under
`https://verself.sh/.well-known/`:

| Path                                | Content                                      | Purpose                                                          |
| ----------------------------------- | -------------------------------------------- | ---------------------------------------------------------------- |
| `/.well-known/verself-ssh-ca.pub`   | OpenBao SSH CA public key (`/etc/ssh/verself-ssh-ca.pub`) | Operator devices verify cert chain; never trust foreign CAs.     |
| `/.well-known/verself-openbao-ca.pem` | OpenBao TLS server cert (`/etc/openbao/tls/cert.pem`) | Operator devices' `bao` CLI uses this as the TLS trust anchor.   |
| `/.well-known/verself-wireguard.json` | `{server_pubkey, endpoint, port, network}` | Operator devices configure their `wg-ops` interface from this.   |

Cached locally on each device under `~/.config/verself/trust-anchors/`
on first fetch (TLS-verified). Subsequent fetches must match the
pinned sha256 or the operation aborts.

## Operator-device onboarding (laptops)

A trusted operator owns merge access to the CUE topology. Onboarding a
new device is a two-actor flow: the new device generates its keys, the
trusted operator opens the PR.

```
new device                                   trusted operator
──────────                                   ────────────────
aspect operator onboard --device=<name>
  ├─ generate ed25519 SSH keypair
  │  at ~/.config/verself/ssh/<name>{,.pub}
  ├─ generate WireGuard keypair
  │  at ~/.config/verself/wg/<name>{,.pub}
  ├─ fetch /.well-known/verself-* (TLS, pin sha256)
  └─ print CUE diff for the device entry  ─►  open PR with diff
                                              merge → deploy
                                              wg-ops peer list reconciles
                                              ssh principals reconcile
                       ◄──  device entry live
  ├─ wg-quick up wg-ops (using fetched discovery + local priv key)
  ├─ bao login -method=oidc -path=oidc-ssh-ca role=operator
  │  (browser → Zitadel; one-shot)
  ├─ bao write ssh-ca/sign/operator
  │     public_key=@<name>.pub
  │     valid_principals=operator
  │     key_id=verself-operator-<name>
  └─ ssh fm-dev-w0 'true'  (validates the full path)
```

The Vault token returned by OIDC is **periodic**: `aspect operator refresh`
(invoked by `aspect deploy` pre-flight) renews it indefinitely so long
as `token_max_ttl` (30d) has not elapsed. After 30d the operator
re-runs `aspect operator onboard --refresh-oidc`, which re-OIDCs and
reuses the existing keys/cert path.

Hard-fail conditions (loud, no silent fallbacks):

- Vault token expired: `aspect operator refresh` exits non-zero with the
  recovery command in stderr. Subsequent `aspect deploy` aborts before
  touching the substrate.
- `/.well-known/` artifact sha256 differs from the pinned hash: onboard
  aborts. Recovery is manual hash verification against the host's
  `/etc/openbao/credstore/ssh-ca.pub` via an already-onboarded device.
- WireGuard handshake does not complete within 10s: onboard aborts;
  nothing else is attempted.

## Workload onboarding (Devin, Cursor, CI runners)

The pool model. `workloads.pool.slot_count` (default 4) is declared in
CUE. Each slot has a permanent `(wg_pubkey, wg_addr)` pair generated on
first deploy and stored in OpenBao KV at
`kv/workload-pool/slots/<n>/{wg-private-key,wg-public-key}`. Slot
pubkeys are projected into `wg-ops` peers alongside operator devices,
so the kernel WireGuard config is byte-stable across reconfigures.

Claiming is metadata, not kernel state:

```
operator                                      workload (Devin/Cursor VM)
────────                                      ──────────────────────────
aspect operator enroll-workload
  ├─ claim a free slot from
  │  kv/workload-pool/leases (CAS write)
  ├─ mint AppRole secret-id
  │  (15m TTL, single-use)
  └─ emit env block:                         ►  injected as VM secret
       VERSELF_BOOTSTRAP_ROLE_ID=...
       VERSELF_BOOTSTRAP_SECRET_ID=...
       VERSELF_WG_PRIVATE_KEY=...   (slot's)
       VERSELF_SLOT=<n>
                                              verself-workload-bootstrap
                                                ├─ fetch /.well-known/verself-*
                                                ├─ wg-quick up wg-ops
                                                ├─ bao write auth/approle/login
                                                │   (returns 24h Vault token)
                                                ├─ bao write ssh-ca/sign/workload
                                                │   key_id=verself-workload-slot-<n>
                                                └─ exit (no daemon)
```

Cert TTL is 24h, hard-capped by the AppRole's `token_max_ttl`. The
slot lease auto-expires 24h after issue; on expiry the slot returns to
the free pool. Pool exhaustion fails `enroll-workload` with an
explicit error pointing at `slot_count` in `instances/prod/operators.cue`.

Workloads do not run a refresh daemon. A 24h cert is enough for one
work session; extending requires the operator to mint a new bootstrap
secret. This is the explicit constraint: a stolen workload secret-id
buys at most 24h of access from the wg-ops CIDR, the slot's KeyID
stamps every action, and revocation is "delete the lease in
OpenBao KV" with no host-side reconvergence required.

## SSH cert lifecycle

| Principal    | Source        | Cert max TTL | Vault token TTL          | Refresh                                        |
| ------------ | ------------- | ------------ | ------------------------ | ---------------------------------------------- |
| `operator`   | OIDC (human)  | 1h           | 14d periodic, 30d max    | `aspect operator refresh` (auto on `aspect deploy`) |
| `workload`   | AppRole       | 24h          | 24h, no renewal          | reissue via fresh AppRole secret-id            |
| `breakglass` | OIDC (human)  | 24h          | 24h                      | manual; deliberately friction-heavy            |

The breakglass path remains as-is. No daemon, no agent process — the
periodic-token model means the only persistent on-disk state on an
operator device is the Vault token in `~/.vault-token`, which the
existing OpenBao TTL machinery already manages.

## Observability

`verself.host_auth_events` is the single queryable surface. Every sshd
accept lands a row whose `cert_id` is the stamped KeyID, so the trusted
device set is recoverable by string match without joining other tables.

`aspect observe detect-recent-intrusions` runs:

```sql
SELECT recorded_at, source_ip, cert_id, key_fingerprint, body
FROM verself.host_auth_events
WHERE event_date >= today() - 1
  AND outcome = 'accepted'
  AND (
    NOT match(cert_id, '^verself-(operator|workload|breakglass)-[a-z0-9-]+$')
    OR splitByChar('-', cert_id)[3] NOT IN (
      -- the current trusted set, projected from CUE at query time
      <known device + slot suffix list>
    )
  )
ORDER BY recorded_at DESC;
```

The "known set" is materialised on the controller from the rendered
CUE, written next to the deploy artifacts as
`.cache/render/<site>/known_cert_id_suffixes.txt`, and shipped into the
ClickHouse query body. A Grafana panel on the host-auth dashboard runs
the same query as an alert with a 5-minute evaluation window.

## Operator commands

- `aspect operator onboard --device=<name>` — interactive onboarding
  on the new device. Idempotent: re-running on an already-onboarded
  device refreshes the cert and exits.
- `aspect operator refresh` — non-interactive Vault token renew + cert
  re-sign. Invoked by `aspect deploy` pre-flight. Fails loudly with the
  required recovery command if the token is past `token_max_ttl`.
- `aspect operator enroll-workload [--slot=<n>]` — operator-side. Claims
  a free slot, mints a single-use 15-min AppRole secret-id, prints the
  env block. Slot selection is automatic unless `--slot` pins it.
- `aspect observe detect-recent-intrusions` — runs the
  unknown-cert-id query for the last 24h.

## Out of scope (deferred)

- Lifecycle audit table (`verself.operator_lifecycle_events`), status
  MV, governance audit events. The cert-id-stamped `host_auth_events`
  is the single observability surface until that proves insufficient.
- CUE-projected `RevokedKeys` and `aspect platform rotate-ssh-ca`
  playbook. Rotation policy: delete the CA in OpenBao, redeploy, every
  device re-onboards. Acceptable while pre-release.
- SPIRE node-attestation path for headless workloads. The AppRole
  bootstrap-secret model covers Devin/Cursor without requiring an
  attestor that ephemeral VMs can satisfy.
- Generated SSH-config drop-in for the ansible inventory's
  `ansible_host` → wg-ops mapping is part of the same change set as
  reverting the `cloudflare_dns_public_ip` split (1903118e).
