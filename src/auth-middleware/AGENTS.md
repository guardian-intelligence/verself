# auth-middleware

Single source of truth for Zitadel JWT validation. Every Go service imports this. Do not fork.

Validates JWTs against Zitadel's JWKS endpoint (cached, local crypto after first fetch). Identity — subject, org ID, roles, email — is extracted from token claims and attached to request context. Services layer their operation catalog and authorization on top.

Zitadel is the sole IdP. Social login (Google / GitHub / Microsoft / Apple), MFA, and passkeys are Zitadel-side configuration — not this library's concern.

## OIDC discovery path

Services use standard OIDC discovery from the public issuer URL:

- `IssuerURL` = `https://auth.<domain>` — discovers provider metadata and validates the JWT `iss` claim.

Do not add a second JWKS URL or Host-header transport. That creates a split trust path where the issuer and discovery metadata can disagree.

On the single-node deployment, `auth.<domain>` resolves to the local HAProxy edge (`127.0.0.1:443`) so service-side discovery and JWKS fetches stay on the loopback. Two paths get there because the supervisors have different namespacing:

- systemd-supervised units bind-mount `/etc/verself/auth-discovery-hosts` over `/etc/hosts` per-unit (`bind_read_only_paths` in `convergence.cue`), keeping the override scoped to the service.
- Nomad raw_exec workloads have no mount-namespace facility, so the `127.0.0.1 auth.<domain>` line lives directly in the host `/etc/hosts` (added by the `base` Ansible role) and applies process-wide.

Per-service nftables must allow loopback port 443 for discovery and JWKS fetches in either case. A three-node topology can remove both overrides and route discovery to the remote auth origin without changing Go service configuration.
