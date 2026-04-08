# Platform

Infrastructure, deployment, and operational tooling for the forge-metal stack.

## Local Frontend Development

Frontend apps (TanStack Start) run locally via `vp dev` with HMR. They talk to remote services over SSH tunnels. The one complication is OIDC: Zitadel enforces exact redirect URI matching and HTTPS-only in production OIDC apps.

### Zitadel Dev Mode OIDC Apps

Each frontend that uses OIDC needs **two Zitadel OIDC applications**: one for production (`devMode: false`, HTTPS redirect URIs) and one for local development (`devMode: true`, HTTP localhost redirect URIs). Zitadel's `devMode` toggle controls:

- **`devMode: false`** (production): HTTPS-only redirect URIs, exact match
- **`devMode: true`** (development): HTTP allowed, glob patterns in redirect URIs (e.g., `http://localhost:*/callback`)

Production OIDC apps are created automatically by each app's Ansible role (`zitadel_app.yml`). Dev OIDC apps are created once manually or via `seed-demo.yml`.

### Dev OIDC app setup

For each frontend, create a dev OIDC app in the same Zitadel project as the production app. Use the Zitadel console at `https://auth.<domain>` or the Management API:

| Frontend | Zitadel Project | Prod Port | Dev Port | Dev Redirect URI |
|---|---|---|---|---|
| rent-a-sandbox | sandbox-rental | 4244 | 4244 | `http://127.0.0.1:4244/callback` |
| webmail | mailbox-service | 4245 | 4245 | `http://127.0.0.1:4245/callback` |

The dev app must have:
- `appType: OIDC_APP_TYPE_USER_AGENT` (SPA, no server secret)
- `authMethodType: OIDC_AUTH_METHOD_TYPE_NONE` (public client)
- `devMode: true`
- `accessTokenType: OIDC_TOKEN_TYPE_JWT` (so backend middleware can validate)
- Redirect URI: `http://127.0.0.1:<port>/callback`
- Post-logout URI: `http://127.0.0.1:<port>`

### Running a frontend locally

```bash
# 1. SSH tunnels to remote services (run once, background)
ssh -L 4246:127.0.0.1:4246 \   # mailbox-service API
    -L 3011:127.0.0.1:3011 \   # electric-mail (shapes)
    -L 3010:127.0.0.1:3010 \   # electric (sandbox shapes)
    -L 4243:127.0.0.1:4243 \   # sandbox-rental-service API
    fm-dev-w0 -N &

# 2. Start the app with dev credentials
cd src/viteplus-monorepo/apps/mail   # or apps/rent-a-sandbox
AUTH_ISSUER_URL=https://auth.<domain> \
AUTH_CLIENT_ID=<dev-oidc-client-id> \
ELECTRIC_URL=http://127.0.0.1:3011 \
vp dev
```

Open `http://127.0.0.1:<port>`. Vite HMR gives sub-second feedback on every file save. Auth goes through real Zitadel (HTTPS, external). API calls and Electric shapes go through the SSH tunnels to real services.

### Why not just toggle devMode on the production app?

Works fine today (no real users). But once there are users, a production OIDC app with `devMode: true` accepts HTTP redirects, which is an open redirect vulnerability. Separate apps keep the blast radius contained.

## Ansible

See the root `AGENTS.md` for playbook listing and deployment topology.

### Role Conventions

- Roles live flat under `roles/` — no nesting by service type
- Each service role owns its own credentials (`/etc/credstore/<service>/`), systemd unit, nftables rules, and health check
- Secrets are SOPS-encrypted in `group_vars/all/secrets.sops.yml`, written to credstore by each role, loaded at runtime via systemd `LoadCredential=`
- PostgreSQL migrations live with the service that owns the schema (e.g., `src/mailbox-service/migrations/`); the platform provisions databases and roles, the service's Ansible role applies its migrations

### ElectricSQL Multi-Instance

Running multiple Electric instances on the same PostgreSQL cluster requires differentiating three things to avoid collisions. See the "ElectricSQL gotchas" section in the root `AGENTS.md` for details on `ELECTRIC_REPLICATION_STREAM_ID`, `ELECTRIC_INSTANCE_ID`, and `RELEASE_NAME`.
