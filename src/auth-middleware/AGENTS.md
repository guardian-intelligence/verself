# auth-middleware

Single source of truth for Zitadel JWT validation. Every Go service imports this. Do not fork.

Validates JWTs against Zitadel's JWKS endpoint (cached, local crypto after first fetch). Identity — subject, org ID, roles, email — is extracted from token claims and attached to request context. Services layer their operation catalog and authorization on top.

Zitadel is the sole IdP. Social login (Google / GitHub / Microsoft / Apple), MFA, and passkeys are Zitadel-side configuration — not this library's concern.

## OIDC discovery path

Services use standard OIDC discovery from the public issuer URL:

- `IssuerURL` = `https://auth.<domain>` — discovers provider metadata and validates the JWT `iss` claim.

Do not add a second JWKS URL or Host-header transport. That creates a split trust path where the issuer and discovery metadata can disagree.

On the single-node deployment, service units bind-mount `/etc/verself/auth-discovery-hosts` over `/etc/hosts` so `auth.<domain>` resolves to local Caddy (`127.0.0.1:443`) for those services only. Per-service nftables must allow loopback port 443 for discovery and JWKS fetches. A three-node topology can remove the host override and route discovery to the remote auth origin without changing Go service configuration.
