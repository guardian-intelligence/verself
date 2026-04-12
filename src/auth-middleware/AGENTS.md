# auth-middleware

Single source of truth for Zitadel JWT validation. Every Go service imports this. Do not fork.

Validates JWTs against Zitadel's JWKS endpoint (cached, local crypto after first fetch). Identity — subject, org ID, roles, email — is extracted from token claims and attached to request context. Services layer their operation catalog and authorization on top.

Zitadel is the sole IdP. Social login (Google / GitHub / Microsoft / Apple), MFA, and passkeys are Zitadel-side configuration — not this library's concern.

## Single-node JWKS fetch path

On a single bare-metal node, services fetch JWKS directly from Zitadel's loopback address (`http://127.0.0.1:8085/oauth/v2/keys`) using `oidc.ProviderConfig` with a **split issuer / JWKS URL**:

- `IssuerURL` = `https://auth.<domain>` — validates the JWT `iss` claim.
- `JWKSURL` = `http://127.0.0.1:8085/oauth/v2/keys` — controls where keys are actually fetched from.

A **Host-header-overriding HTTP transport** sends `Host: auth.<domain>` on JWKS requests so Zitadel's instance router accepts them. Without that header, Zitadel's multi-tenant router rejects the loopback request.

This layout avoids routing JWKS fetches through Caddy (TLS termination, WAF, DNS resolution) and eliminates the need for port-443 and DNS egress rules in per-service nftables.

The existing `oifname "lo" tcp dport 8085 accept` rule is sufficient **only** for single-node topology. On a 3-node topology, both the JWKS URL and the per-service nftables egress rules need to become topology-aware — Zitadel becomes remote and the loopback rule is no longer enough.
