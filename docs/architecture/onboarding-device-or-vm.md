# Operator Access

Pomerium is the operator access plane for browser and SSH entry points.
Zitadel remains the human identity provider. OpenBao remains the workload
secret store and runtime relying party, but it does not issue operator SSH
certificates.

## SSH Path

Public `:22` terminates at Pomerium native SSH. Pomerium authenticates the
operator through Zitadel, evaluates route policy, signs an ephemeral SSH user
certificate with its local user CA, and opens an upstream SSH session to host
loopback.

```
operator ssh client -> access.<domain>:22 -> Pomerium -> 127.0.0.1:22 sshd
```

The upstream sshd configuration is intentionally narrow:

- `ListenAddress 127.0.0.1`
- `TrustedUserCAKeys /etc/ssh/verself-pomerium-user-ca.pub`
- `AuthorizedKeysFile none`
- password and keyboard-interactive authentication disabled

The SSH route name is `prod`, so a standard client connects with:

```bash
ssh ubuntu@prod@access.<domain>
```

Pomerium binds the local SSH public key to the authenticated Zitadel subject on
first use. Subsequent SSH, SCP, SFTP, Ansible, and Go SSH connections use the
same public-key source and re-evaluate Pomerium policy on each connection.

## HTTP Operator Routes

Operator HTTP routes are derived from `topology_routes` entries whose kind is
`operator_origin`. HAProxy terminates public TLS and forwards those origins to
Pomerium's loopback HTTP listener. Pomerium injects signed identity headers for
upstreams that can consume them.

Grafana uses Grafana's JWT auth provider with `X-Pomerium-Jwt-Assertion` and
Pomerium's JWKS endpoint. Grafana's local admin remains enabled for host-level
recovery through direct credential-store access.

## Authorization

The initial policy is an explicit operator email allow-list in the Pomerium
role defaults. Domain-wide access is available through
`pomerium_operator_allowed_domains`, but the default is empty.

SSH authorization constrains two dimensions:

- the authenticated identity must match an allowed Pomerium subject
- the requested upstream SSH username must be `ubuntu`

Broader environment scoping belongs in Pomerium route policy. Separate route
names such as `staging`, `prod`, and future preview environments can point at
different upstreams and carry different allowed subjects.

## Detection

After the cutover, accepted sshd sessions should have a loopback source address
because only Pomerium reaches upstream sshd. `aspect detect-intrusions` queries
`verself.host_auth_events` for accepted events with `source_ip` outside
`127.0.0.1` and `::1`.

```sql
SELECT recorded_at, outcome, auth_method, cert_id, user, source_ip, body
FROM verself.host_auth_events
WHERE event_date >= today() - 31
  AND recorded_at >= now() - toIntervalHour({hours:UInt32})
  AND outcome = 'accepted'
  AND source_ip NOT IN ('127.0.0.1', '::1')
ORDER BY recorded_at DESC;
```

The expected result is zero rows.

## Recovery

Lockout recovery is host reprovisioning. During pre-release operation this is
preferable to preserving a second production SSH authority or static host key
path. The Pomerium SSH user CA key lives in `/etc/credstore/pomerium/`; deleting
that credstore and rerunning host convergence rotates operator SSH trust.

OpenBao recovery remains independent of operator SSH. OpenBao runtime-secret
bindings are reconciled by the `openbao_runtime_secrets` role after the OpenBao
daemon is healthy.
