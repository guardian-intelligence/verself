# RFC: Application Layer Tech Stack

Two revenue-generating applications on the forge-metal platform, sharing auth and billing infrastructure. Both deployed on the same bare-metal box alongside existing CI and observability services.

## Applications

**Sandbox** — agent execution environments billed per second of wall-clock compute. Thin product layer over existing Firecracker infrastructure. Users get API keys, launch sandboxed VMs, pay for vCPU-seconds and GiB-seconds.

**Storefront** — bare metal server resale. Inventory sourced from upstream providers (Latitude.sh initially), marked up, sold as managed boxes with provisioning automation. Standard ecommerce: catalog, cart, checkout, order lifecycle.

Both are Next.js applications. Both authenticate against the same identity provider. Both settle payments through Stripe.

## Component Decisions

### Identity: Zitadel

Event-sourced identity provider. Single Go binary, PostgreSQL-backed. Every mutation is an immutable event appended to an event store — the audit trail is not a bolted-on log table but the database itself. Multi-tenant organizations are the central architectural primitive, not a feature added in v25 like Keycloak's Organizations.

Each customer org maps to a Zitadel organization. Both applications use OIDC with Zitadel as the IdP. Org membership and roles are managed in Zitadel; application-specific authorization (e.g., sandbox quotas, storefront purchase history) stays in each app's database.

License: AGPL-3.0 since v3. Acceptable for this deployment — Zitadel runs as infrastructure, not distributed as part of a product.

#### Why Zitadel

Three properties drove the decision over Keycloak, Authentik, and Ory:

1. **Event-sourced audit trail.** Every state change (user created, role assigned, token issued, password checked) is an immutable event with a monotonic sequence number. No `DELETE` or `UPDATE` touches the event store. This is architecturally unique among self-hosted identity providers — Keycloak stores mutable rows with optional event listeners, Authentik uses standard Django ORM. For a financial platform, an immutable identity audit trail is a compliance requirement, not a nice-to-have.

2. **Resource footprint.** ~512 MB RAM for Zitadel + shared PostgreSQL. Keycloak's JVM baseline is ~1.25 GB (300 MB non-heap + 70% heap from container limit). Authentik requires ~4 GB (Python server + worker processes). Ory requires three separate binaries (Kratos + Hydra + Keto) plus building your own login UI.

3. **Multi-org as core primitive.** Organizations, project grants, and cross-org role assignments are first-class API objects with dedicated gRPC endpoints. Keycloak added Organizations in v26 (GA) but the feature is younger and less battle-tested. Authentik has soft multi-tenancy via brands/domains but no first-class org model or org-admin delegation.

#### Data model

Zitadel's hierarchy: Instance → Organizations → Projects → Applications. Users belong to exactly one organization. Cross-org access is modeled via Project Grants and Role Assignments, not by duplicating user accounts.

```
Instance (auth.<domain>)
├── Platform Org
│     ├── Project: "Sandbox"
│     │     ├── Roles: [sandbox:admin, sandbox:user]
│     │     └── Application: "Sandbox Web" (OIDC confidential client)
│     ├── Project: "Storefront"
│     │     ├── Roles: [store:admin, store:customer]
│     │     └── Application: "Storefront Web" (OIDC confidential client)
│     ├── Machine User: "orchestrator" (JWT Profile auth)
│     ├── Machine User: "reconciliation-cron" (PAT auth)
│     └── Machine User: "stripe-webhook-worker" (PAT auth)
│
├── Customer Org: "AcmeCorp"
│     ├── Human users (managed by AcmeCorp's ORG_OWNER)
│     ├── Project Grant: Sandbox [sandbox:admin, sandbox:user]
│     ├── Project Grant: Storefront [store:admin, store:customer]
│     └── Custom branding (logo, colors, fonts)
│
└── Customer Org: "StartupXYZ"
      ├── Human users
      └── Project Grant: Sandbox [sandbox:user]  ← sandbox-only customer
```

Two separate projects (not one project with two apps) because the role models diverge — a sandbox admin and a storefront admin are different authorization domains. Both projects live in the Platform Org. Customer orgs receive Project Grants that delegate a subset of roles.

A Project Grant lets the receiving org's `ORG_OWNER` create Role Assignments for their own users, limited to the granted role subset. The receiving org cannot modify the project itself — only the Platform Org can add roles or change application configuration.

#### Role model

Platform administration uses Zitadel's built-in role hierarchy:

| Role | Scope | Capabilities |
|------|-------|-------------|
| `IAM_OWNER` | Instance | Create orgs, manage all instance settings, view all events |
| `ORG_OWNER` | Organization | Full self-service within their org (see below) |
| `PROJECT_OWNER` | Project | Manage project roles, apps, and grants |
| `PROJECT_GRANT_OWNER` | Granted project | Manage role assignments within a specific grant |

An `ORG_OWNER` can self-service without platform admin intervention:

- Create and manage users (invite, deactivate, reset credentials)
- Configure identity providers for their org (SAML, OIDC federation)
- Customize branding (logo, colors, fonts for login and emails)
- Set MFA policy, password complexity, lockout rules, session lifetimes
- Assign roles to their users within granted projects
- Verify custom domains

Application-level roles are defined per project:

| Project | Role Key | Meaning |
|---------|----------|---------|
| Sandbox | `sandbox:admin` | Manage API keys, view usage, configure org settings in Sandbox app |
| Sandbox | `sandbox:user` | Launch VMs, view own usage |
| Storefront | `store:admin` | Manage orders, view inventory, configure org settings in Storefront app |
| Storefront | `store:customer` | Browse catalog, place orders |

#### Token architecture

The critical claim is `urn:zitadel:iam:user:resourceowner:id` — this is the Zitadel organization ID that becomes the tenant key across every system: `org_id` in PostgreSQL, TigerBeetle account ID prefix, `org_id` column in ClickHouse metering.

When a user from AcmeCorp authenticates via the Sandbox app:

```json
{
  "sub": "287401958304129025",
  "iss": "https://auth.example.com",
  "aud": ["287401958304129025@sandbox"],
  "urn:zitadel:iam:user:resourceowner:id": "180025476050993153",
  "urn:zitadel:iam:user:resourceowner:name": "AcmeCorp",
  "urn:zitadel:iam:user:resourceowner:primary_domain": "acme.auth.example.com",
  "urn:zitadel:iam:org:project:roles": {
    "sandbox:user": {
      "180025476050993153": "acme.auth.example.com"
    }
  }
}
```

The role claim is a nested structure: `{role_key: {granting_org_id: granting_org_domain}}`. For most applications, a flat array is easier to consume. An Actions V1 script flattens it:

```javascript
function flattenRoles(ctx, api) {
  var roles = [];
  if (ctx.v1.claims['urn:zitadel:iam:org:project:roles']) {
    var projectRoles = ctx.v1.claims['urn:zitadel:iam:org:project:roles'];
    for (var key in projectRoles) {
      roles.push(key);
    }
  }
  api.v1.claims.setClaim('roles', roles);
}
```

Attached to the "Pre Access Token Creation" trigger, this produces `"roles": ["sandbox:user"]` alongside the original nested claim.

Scopes requested at auth time to populate these claims:

| Scope | Effect |
|-------|--------|
| `openid profile email` | Standard OIDC claims |
| `urn:zitadel:iam:user:resourceowner` | Includes org ID, name, and primary domain |
| `urn:zitadel:iam:org:project:role:sandbox:admin` | Request specific role claim |
| `urn:zitadel:iam:org:projects:roles` | Request roles for all projects |
| `urn:zitadel:iam:org:id:{org_id}` | Restrict login to a specific org + apply its branding |
| `offline_access` | Include refresh token |

#### OIDC integration pattern

Two OIDC confidential clients — one per Next.js application. Both use Authorization Code + PKCE. The org scope in the auth request restricts login to the user's org and triggers per-org branding on the login page.

```
User visits sandbox.example.com
  → Next.js middleware: no session
  → Redirect to auth.example.com/oauth/v2/authorize
      ?client_id=sandbox_client_id
      &scope=openid profile email
             urn:zitadel:iam:user:resourceowner
             urn:zitadel:iam:org:projects:roles
             offline_access
      &response_type=code
      &code_challenge=...
      &redirect_uri=https://sandbox.example.com/api/auth/callback
  → User authenticates (Zitadel login page, per-org branding)
  → Callback with authorization code
  → Token exchange: code → access_token + id_token + refresh_token
  → Next.js extracts from token:
      org_id = claims["urn:zitadel:iam:user:resourceowner:id"]
      roles  = claims["roles"]  (flattened by Action)
  → org_id becomes the tenant context for all downstream calls:
      PostgreSQL: WHERE org_id = $1
      TigerBeetle: account lookup by org-prefixed ID
      ClickHouse: org_id column in metering rows
```

The org ID is the single tenant key that threads through the entire billing pipeline. It is never derived from application-layer state — it comes exclusively from the identity token.

#### Machine-to-machine auth

Three machine users in the Platform Org for backend services:

| Machine User | Auth Method | Purpose |
|-------------|-------------|---------|
| `orchestrator` | JWT Profile (key pair) | VM billing: create/post/void TigerBeetle transfers, read org pricing from PostgreSQL |
| `reconciliation-cron` | PAT | Hourly consistency check: query ClickHouse metering, compare against TigerBeetle, push to Stripe |
| `stripe-webhook-worker` | PAT | Process Stripe webhooks: credit org accounts in TigerBeetle, update order state in PostgreSQL |

JWT Profile is the most secure method for the orchestrator — a private key signs a JWT assertion exchanged at the token endpoint. No client secret transmitted over the wire. PATs are simpler (pre-generated Bearer tokens, no OAuth dance) and acceptable for internal cron jobs and webhook workers that run on localhost.

Machine users receive role assignments like human users. The orchestrator gets `sandbox:admin` on the Sandbox project. The webhook worker gets a custom `billing:writer` role that authorizes TigerBeetle operations without granting application-level admin access.

#### Deployment model

PostgreSQL requirements: v14-18. No special extensions. Zitadel creates three schemas in its database (`eventstore`, `projections`, `system`) via the `zitadel init` command using a PostgreSQL admin connection.

Three-phase lifecycle:

```bash
# Phase 1: Create database, user, schemas (requires PG admin credentials)
zitadel init --config /etc/zitadel/config.yaml

# Phase 2: Run migrations, create first instance + default org + admin user
zitadel setup --masterkey "$(cat /etc/zitadel/masterkey)" --config /etc/zitadel/config.yaml

# Phase 3: Start the server (steady-state systemd service)
zitadel start --masterkey "$(cat /etc/zitadel/masterkey)" --config /etc/zitadel/config.yaml
```

`start-from-init` combines all three phases idempotently but does not exit after setup — it runs the server. For Ansible automation, run `init` and `setup` separately (both exit on completion), then let systemd manage `start`.

Key configuration (`/etc/zitadel/config.yaml`):

```yaml
Port: 8085  # 8080 is taken by HyperDX
ExternalDomain: auth.example.com
ExternalPort: 443
ExternalSecure: true

TLS:
  Enabled: false  # Caddy handles TLS termination

Database:
  postgres:
    Host: localhost
    Port: 5432
    Database: zitadel
    User:
      Username: zitadel
      Password: "${ZITADEL_DB_PASSWORD}"
      SSL:
        Mode: disable  # localhost, no TLS needed
    Admin:
      Username: postgres
      Password: "${PG_ADMIN_PASSWORD}"  # required — Zitadel connects over TCP, not unix socket
      SSL:
        Mode: disable
    MaxOpenConns: 10
    MaxIdleConns: 5
    MaxConnLifetime: 30m

Instrumentation:
  Trace:
    Exporter:
      Type: grpc
      Endpoint: localhost:4317
      Insecure: true
  Metrics:
    Enabled: true
```

`ExternalDomain`, `ExternalPort`, and `ExternalSecure` must match the actual public URL. Mismatches cause redirect loops or broken OIDC flows — this is the #1 self-hosting configuration error. Zitadel also enforces `ExternalDomain` on API requests: any request whose `Host` header does not match gets rejected with "Instance not found." The `/debug/healthz` endpoint is exempt. Changing these values after initialization requires rerunning `zitadel setup`.

**Masterkey**: A 32-character string used for AES-256 encryption of secrets at rest (IdP client secrets, TOTP seeds, SMTP passwords, signing keys). Generated once, stored in `/etc/zitadel/masterkey`. **Cannot be rotated** — losing it means losing access to all encrypted data. User passwords and client secrets are hashed (bcrypt/argon2), not encrypted, so those survive masterkey loss.

Systemd unit:

```ini
[Unit]
Description=Zitadel Identity Provider
After=network.target postgresql.service
Requires=postgresql.service

[Service]
Type=simple
User=zitadel
Group=zitadel
ExecStart=/opt/forge-metal/profile/bin/zitadel start \
  --masterkeyFile /etc/zitadel/masterkey \
  --config /etc/zitadel/config.yaml
Restart=always
RestartSec=5
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
```

Caddy route — `h2c` (HTTP/2 cleartext) is required because Zitadel serves gRPC and HTTP on the same port:

```
auth.example.com {
  reverse_proxy h2c://localhost:8085
}
```

Without `h2c`, gRPC calls fail silently. Standard `http://` proxying breaks the Management, Admin, and System APIs.

Default port: 8085 (moved from default 8080 to avoid conflict with HyperDX).

#### Resource requirements

| Resource | Minimum | This deployment |
|----------|---------|-----------------|
| Memory | 512 MB (docs recommend 4 GB per CPU core) | ~512 MB (single-node, low request volume) |
| CPU | 1 core per 100 req/s | Shared, minimal at launch scale |
| Disk | Minimal (state lives in PostgreSQL) | Shared PostgreSQL instance on NVMe |
| Network | HTTP/2-capable reverse proxy | Caddy with h2c upstream |

Password hashing (argon2/bcrypt) can spike to 4 CPU cores during authentication bursts. At launch scale this is negligible.

#### Observability

Zitadel exports telemetry natively via OpenTelemetry — no sidecar or StatsD bridge needed (unlike TigerBeetle):

- **Traces**: gRPC export to the existing OTel Collector on `:4317`. Every OIDC flow, API call, and event store write produces spans. The `Instrumentation.Trace.Exporter` config block must use exactly `Type: grpc` — Zitadel silently ignores unrecognized config keys, so a typo (e.g., `Traces` instead of `Trace`) produces zero trace export with no error logged.
- **Metrics**: Prometheus endpoint at `/debug/metrics`. Request latency, active sessions, event store lag.
- **Logs**: Structured JSON to stdout (`Log.Formatter.Format: json`), captured by journald. The OTel Collector's `journald/zitadel` receiver parses JSON, extracts `TraceID`/`SpanID` (PascalCase) into OTel trace context for log-trace correlation in HyperDX. Note: `request` fields are nested objects in JSON mode (`request.id`, `request.instance_host`), not flat keys.
- **Audit trail**: The event store itself. Every identity mutation is queryable via the Events API with filters by event type, aggregate ID, and time range. Retention is indefinite — events are never deleted.

The OTel Collector already forwards to ClickHouse. Zitadel traces and metrics flow into the existing `otel_traces` and `otel_metrics_*` tables without additional configuration beyond the `Instrumentation` block in Zitadel's config. Logs flow via the `journald/zitadel` receiver → ClickHouse `otel_logs` table.

#### Security model

Zitadel has no built-in authentication between callers and its API — security depends on network binding and the reverse proxy layer.

For this deployment: Zitadel listens on `127.0.0.1:8085`. Caddy terminates TLS and proxies public traffic to Zitadel. The Management Console (admin UI) is accessible at `auth.<domain>/ui/console` — restrict access via Caddy IP allowlisting or HTTP basic auth if the console should not be public-facing.

SMTP is required for email verification, password reset, and user invitations. Without a configured SMTP provider, these flows fail silently. Resend is the planned provider (deferred — not yet implemented). Until SMTP is configured, use pre-verified users and manual password setup.

#### Backup strategy

1. **PostgreSQL ZFS snapshots** of the data directory. Crash-consistent because the event store is append-only — a snapshot at any point captures a valid prefix of the event log. Projections (materialized views) are derived from events and can be rebuilt by replaying the event store.
2. **Masterkey backup.** The masterkey file must be backed up separately from the database — it is not stored in PostgreSQL. Without it, encrypted secrets (IdP configs, TOTP seeds, signing keys) are unrecoverable. The masterkey is stored in `secrets.sops.yml` (SOPS-encrypted, committed to git) and deployed to `/etc/credstore/zitadel/masterkey` on the server.

The event store is the single source of truth. If projections become corrupted, Zitadel rebuilds them on startup from the event log. A bare restore requires only the PostgreSQL data directory and the masterkey file.

#### Known caveats

- **Masterkey is non-rotatable.** Once set at `init` time, it cannot be changed without losing access to encrypted data. Analogous to TigerBeetle's immutable replica count — plan for it, don't plan to change it.
- **SCIM not production-ready.** Enterprise directory sync (Azure AD, Okta) requires manual user provisioning or API automation until SCIM ships. Keycloak and Authentik are ahead here.
- **Actions V1 → V2 migration.** Actions V1 (inline JavaScript via `goja` engine) is disabled in Zitadel v5. Actions V2 replaces inline JS with external HTTP webhooks — requires running a separate service for custom token logic. Start on V1 for the current v4 deployment; plan migration when upgrading to v5.
- **CVE tracking required.** Five significant CVEs in 2025 (one critical, CVSS 9.3). Patches span v2.x, v3.x, and v4.x branches. Single binary replacement + systemd restart makes patching fast, but it must be tracked.
- **ExternalDomain/ExternalPort/ExternalSecure must be correct from day one.** Changing these post-initialization requires rerunning `zitadel setup` and may invalidate existing OIDC sessions and passkey registrations.
- **Passkeys are domain-bound.** WebAuthn credentials are tied to `ExternalDomain`. Changing the domain after users register passkeys invalidates those credentials. Pick the final domain before onboarding users.
- **AGPL-3.0 license.** Network use triggers copyleft. Acceptable for internal infrastructure use; would require evaluation if Zitadel were modified and exposed as a service to third parties.
- **Login V2 is a separate service.** Zitadel 4.x defaults to `LoginV2.Required: true`, which redirects OIDC auth to `/ui/v2/login/` — a path served by a standalone Next.js app (`zitadel-login`), not the Go binary. The binary only embeds Login V1 (`/ui/login/`). This deployment disables Login V2 via the instance feature flag and uses the embedded Login V1. Login V1 is deprecated and removed in v5. Deploying Login V2 requires: (1) the `zitadel-login` Next.js service, (2) a service account PAT for the login app, (3) Caddy path-based routing to split `/ui/v2/login/*` to the login service.
- **No built-in caching layer.** Redis caching exists but is beta (standalone only, no Sentinel/Cluster). At single-node scale, PostgreSQL query performance is sufficient.

### Database: PostgreSQL (single instance, multiple databases)

```
PostgreSQL
├── zitadel       # Zitadel IAM state
├── sandbox       # sandbox app (API keys, job metadata, org quotas, pricing config)
└── storefront    # storefront app (inventory, orders, provisioning state)
```

One PostgreSQL instance. Databases are isolated — no cross-database queries, no shared schemas. The tenant linkage between apps is the Zitadel organization ID stored as a column in each app's tables.

Application state lives here. Financial state does not — that's TigerBeetle. The billing package's product catalog, subscription state, grant attribution, retry/DLQ state, and Stripe correlation live in PostgreSQL; aggregate balance enforcement lives in TigerBeetle.

Billing package tables live in the existing `sandbox` database because that is the infrastructure truth today. The database name does not imply product scope. The billing package is shared across products even though the backing PostgreSQL database retains its historical name.

Every billing mutation that reads or writes `credit_grants`, subscription status, or product eligibility for a single org acquires `pg_advisory_xact_lock(org_id::bigint)` inside the transaction before any reads. This is mandatory. The org IDs issued by Zitadel are decimal strings that fit in signed 63 bits, so the cast is lossless for forge-metal.

#### Sandbox database schema

```sql
-- Billing catalog. Product-agnostic.
CREATE TABLE products (
    product_id    TEXT PRIMARY KEY,
    display_name  TEXT NOT NULL,
    meter_unit    TEXT NOT NULL,  -- vcpu_second, token, gb_month, unit
    billing_model TEXT NOT NULL,  -- metered, licensed, one_time
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE plans (
    plan_id                 TEXT PRIMARY KEY,
    product_id              TEXT NOT NULL REFERENCES products(product_id),
    display_name            TEXT NOT NULL,
    stripe_monthly_price_id TEXT,
    stripe_annual_price_id  TEXT,
    monthly_price_cents     INTEGER,
    annual_price_cents      INTEGER,
    included_credits        BIGINT NOT NULL DEFAULT 0,
    unit_rates              JSONB NOT NULL DEFAULT '{}',
    overage_unit_rates      JSONB NOT NULL DEFAULT '{}',
    quotas                  JSONB NOT NULL DEFAULT '{}',
    cancellation_policy     JSONB NOT NULL DEFAULT '{"annual_refund_mode":"credit_note","void_remaining_credits":false}',
    is_default              BOOLEAN NOT NULL DEFAULT false,
    sort_order              INTEGER NOT NULL DEFAULT 0,
    active                  BOOLEAN NOT NULL DEFAULT true,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX idx_default_plan_per_product
    ON plans (product_id) WHERE is_default;

-- Org registration and Stripe correlation. Created lazily on first authenticated
-- request from the token's urn:zitadel:iam:user:resourceowner:id claim.
CREATE TABLE orgs (
    org_id              TEXT PRIMARY KEY,   -- Zitadel organization ID (string, not UUID)
    display_name        TEXT NOT NULL,
    stripe_customer_id  TEXT UNIQUE,        -- set on first Stripe Checkout session
    billing_email       TEXT,
    trust_tier          TEXT NOT NULL DEFAULT 'new',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TYPE subscription_status AS ENUM (
    'active', 'past_due', 'suspended', 'cancelled', 'trialing'
);
CREATE TYPE billing_cadence AS ENUM ('monthly', 'annual');

CREATE TABLE subscriptions (
    subscription_id         BIGINT PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
    org_id                  TEXT NOT NULL REFERENCES orgs(org_id),
    plan_id                 TEXT NOT NULL REFERENCES plans(plan_id),
    product_id              TEXT NOT NULL REFERENCES products(product_id),
    stripe_subscription_id  TEXT UNIQUE,
    stripe_item_id          TEXT,
    cadence                 billing_cadence NOT NULL DEFAULT 'monthly',
    billing_anchor_day      SMALLINT NOT NULL DEFAULT 1,
    current_period_start    TIMESTAMPTZ,
    current_period_end      TIMESTAMPTZ,
    status                  subscription_status NOT NULL DEFAULT 'active',
    status_changed_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    past_due_since          TIMESTAMPTZ,
    cancel_at_period_end    BOOLEAN NOT NULL DEFAULT false,
    cancelled_at            TIMESTAMPTZ,
    cancellation_reason     TEXT,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX idx_one_active_sub_per_product
    ON subscriptions (org_id, product_id)
    WHERE status IN ('active', 'past_due', 'trialing');

CREATE TYPE grant_account AS ENUM ('free_tier', 'credit');

CREATE TABLE credit_grants (
    grant_id            BIGINT PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
    org_id              TEXT NOT NULL REFERENCES orgs(org_id),
    product_id          TEXT NOT NULL REFERENCES products(product_id),
    account_type        grant_account NOT NULL,
    amount              BIGINT NOT NULL CHECK (amount > 0),
    consumed            BIGINT NOT NULL DEFAULT 0 CHECK (consumed >= 0),
    expired             BIGINT NOT NULL DEFAULT 0 CHECK (expired >= 0),
    remaining           BIGINT GENERATED ALWAYS AS (amount - consumed - expired) STORED,
    source              TEXT NOT NULL,      -- subscription, purchase, promo, refund, free_tier
    stripe_reference_id TEXT,
    subscription_id     BIGINT REFERENCES subscriptions(subscription_id),
    period_start        TIMESTAMPTZ,
    period_end          TIMESTAMPTZ,
    expires_at          TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_credit_grants_active
    ON credit_grants (org_id, product_id, expires_at)
    WHERE remaining > 0;
CREATE UNIQUE INDEX idx_credit_grants_subscription_period
    ON credit_grants (subscription_id, period_start, account_type)
    WHERE subscription_id IS NOT NULL;

CREATE TABLE org_pricing_overrides (
    org_id       TEXT NOT NULL REFERENCES orgs(org_id),
    plan_id      TEXT NOT NULL REFERENCES plans(plan_id),
    unit_rates   JSONB NOT NULL,
    quotas       JSONB,
    notes        TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, plan_id)
);

-- API keys for programmatic VM launch. Keys are bcrypt-hashed; the plaintext
-- is shown once at creation and never stored.
CREATE TABLE api_keys (
    key_id       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id       TEXT NOT NULL REFERENCES orgs(org_id),
    key_hash     TEXT NOT NULL,
    name         TEXT NOT NULL,
    scopes       TEXT[] NOT NULL DEFAULT '{}',
    created_by   TEXT NOT NULL,      -- Zitadel user ID of the creator
    expires_at   TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Job metadata. One row per Firecracker VM execution. Financial settlement
-- is in TigerBeetle; this table tracks lifecycle state.
-- BIGINT (not UUID) because the job_id is packed into TigerBeetle transfer IDs
-- via injective bit packing. See "Deterministic ID derivation" section.
CREATE TABLE jobs (
    job_id       BIGINT PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
    org_id       TEXT NOT NULL REFERENCES orgs(org_id),
    product_id   TEXT NOT NULL REFERENCES products(product_id),
    status       TEXT NOT NULL DEFAULT 'pending',
    vcpus        SMALLINT NOT NULL,
    mem_mib      INTEGER NOT NULL,
    started_at   TIMESTAMPTZ,
    ended_at     TIMESTAMPTZ,
    exit_reason  TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TYPE task_status AS ENUM ('pending', 'claimed', 'completed', 'retrying', 'dead');

-- Billing package task queue. BIGINT PK participates in deterministic
-- TigerBeetle transfer ID derivation for Stripe-driven tasks.
CREATE TABLE tasks (
    task_id         BIGINT PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
    task_type       TEXT NOT NULL,
    payload         JSONB NOT NULL DEFAULT '{}',
    status          task_status NOT NULL DEFAULT 'pending',
    idempotency_key TEXT UNIQUE,         -- pi_..., in_..., dp_..., or app-generated compound key
    attempts        INTEGER NOT NULL DEFAULT 0,
    max_attempts    INTEGER NOT NULL DEFAULT 5,
    last_error      TEXT,
    next_retry_at   TIMESTAMPTZ,
    scheduled_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    claimed_at      TIMESTAMPTZ,
    completed_at    TIMESTAMPTZ,
    dead_at         TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_tasks_claimable
    ON tasks (scheduled_at)
    WHERE status IN ('pending', 'retrying')
      AND (next_retry_at IS NULL OR next_retry_at <= now());
CREATE INDEX idx_tasks_dead
    ON tasks (dead_at)
    WHERE status = 'dead';

CREATE TABLE billing_events (
    event_id        BIGINT PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
    org_id          TEXT NOT NULL,
    event_type      TEXT NOT NULL,
    subscription_id BIGINT,
    grant_id        BIGINT,
    task_id         BIGINT,
    payload         JSONB NOT NULL DEFAULT '{}',
    stripe_event_id TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX idx_billing_events_stripe
    ON billing_events (stripe_event_id)
    WHERE stripe_event_id IS NOT NULL;

CREATE TABLE billing_cursors (
    cursor_name   TEXT PRIMARY KEY,
    cursor_ts     TIMESTAMPTZ,
    cursor_bigint BIGINT,
    cursor_json   JSONB NOT NULL DEFAULT '{}',
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

#### Storefront database schema

```sql
-- Storefront keeps product-local state only. Billing ownership remains in the
-- sandbox database; this database does not duplicate Stripe customer mapping,
-- subscriptions, credit grants, or the billing task queue.
CREATE TABLE org_profiles (
    org_id         TEXT PRIMARY KEY,
    display_name   TEXT NOT NULL,
    billing_email  TEXT,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE inventory (
    server_id    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    provider     TEXT NOT NULL DEFAULT 'latitude',
    spec_slug    TEXT NOT NULL,
    cpu_cores    SMALLINT NOT NULL,
    ram_gb       SMALLINT NOT NULL,
    disk_gb      INTEGER NOT NULL,
    location     TEXT NOT NULL,
    price_cents  INTEGER NOT NULL,   -- monthly, our markup price
    cost_cents   INTEGER NOT NULL,   -- monthly, upstream cost
    status       TEXT NOT NULL DEFAULT 'available',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE orders (
    order_id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id                   TEXT NOT NULL REFERENCES org_profiles(org_id),
    server_id                UUID NOT NULL REFERENCES inventory(server_id),
    status                   TEXT NOT NULL DEFAULT 'pending_payment',
    stripe_payment_intent_id TEXT,
    stripe_subscription_id   TEXT,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

`org_id` is `TEXT` in both databases because Zitadel organization IDs are string identifiers (numeric strings like `"180025476050993153"`), not UUIDs. The canonical billing row is `sandbox.orgs`, created lazily on first authenticated request. Product-local projections such as `storefront.org_profiles` are derived from the same token claim and keyed by the same `org_id`.

### Financial Ledger: TigerBeetle

Purpose-built double-entry accounting database. Single statically-linked Zig binary. Handles balance enforcement, idempotent transfers, and audit trail without application-level locking.

#### Why a separate ledger

PostgreSQL can do double-entry accounting with serializable transactions and CHECK constraints. The argument for TigerBeetle is that the invariants are enforced at the storage engine level, not the application level. A bug in Go code cannot create money, overdraw an account, or produce an unbalanced ledger — the database physically rejects it. For financial data, moving the correctness guarantee out of application code and into the storage layer eliminates an entire class of bugs.

The trade-off is operational: one more service. But TigerBeetle has no external dependencies (no PostgreSQL, no ZooKeeper, no configuration files), so the marginal operational cost is low.

#### Deployment model

TigerBeetle's deployment is two commands:

```bash
# One-time: create the data file
tigerbeetle format --cluster=0 --replica=0 --replica-count=1 /var/lib/tigerbeetle/data.tigerbeetle

# Run
tigerbeetle start --addresses=127.0.0.1:3320 /var/lib/tigerbeetle/data.tigerbeetle
```

The data file is a single pre-allocated file that grows elastically. Internally it's divided into 512 KiB blocks, all immutable and 128-bit checksummed. There are no configuration files — cluster topology is baked in at format time.

The systemd unit requires `AmbientCapabilities=CAP_IPC_LOCK` and `LimitMEMLOCK=infinity` because TigerBeetle locks its entire working set into physical memory (prevents the kernel from swapping pages to disk). Same pattern as ClickHouse's large-page setup.

Default port: 3320 (moved from TigerBeetle's default 3000 to avoid conflict with Forgejo).

#### Single-node caveats

TigerBeetle is designed for 6-replica clusters across 3 geographic sites. A 1-replica deployment works but has specific implications:

- **No replication.** Durability depends on ZFS snapshots and `zfs send` backups of the data file. This is acceptable for the current single-node platform.
- **Replica count is immutable.** Changing from 1 to 3 replicas requires reformatting the data file and replaying state. Plan for this if multi-node is on the roadmap.
- **Upgrades are simple.** Replace the binary, restart via systemd (~5 seconds of downtime). Data files are forward-compatible — new binaries auto-migrate the format.
- **No online schema changes.** Account types and flags are set at creation time. Changing an account's flags requires creating a new account and migrating balances via transfers.

#### Resource requirements

TigerBeetle's binary is ~50 MB, but the runtime footprint is much larger. It locks its grid cache into physical memory and uses io_uring for disk I/O.

| Resource | Minimum | This deployment |
|----------|---------|-----------------|
| Memory | 6 GiB (docs) | 1-2 GiB (tuned via `--cache-grid-size` for single-replica, low-volume) |
| CPU | 1 core dedicated | Shared, low contention at current scale |
| Disk | NVMe local (required for production) | NVMe already present on Latitude.sh box |
| Filesystem | ext4 or XFS | ext4 (ZFS zvol backing is fine — TigerBeetle sees a block device) |

Budget 1-2 GiB, not 50 MB.

#### Account model

Every billing concept maps to TigerBeetle accounts and transfers. No special-cased application logic for discounts, credits, or free tier — they're all transfers between accounts.

```
Per org (account IDs derived via OrgAccountID):
├── OrgAccountID(orgID, FreeTier)   # free-plan allowance for the current billing period
├── OrgAccountID(orgID, Credit)     # prepaid / subscription-funded aggregate balance

Operator (account IDs derived via OperatorAccountID, org_id=0):
├── OperatorAccountID(Revenue)          # realized revenue from paid usage only
├── OperatorAccountID(FreeTierExpense)  # cost of free-tier compute given away
├── OperatorAccountID(FreeTierPool)     # funds monthly free tier grants
├── OperatorAccountID(StripeHolding)    # Stripe payments land here before crediting org
├── OperatorAccountID(PromoPool)        # funds promotional credits
└── OperatorAccountID(ExpiredCredits)   # breakage from expired paid credits
```

Three ways for an org to have balance: free-plan grants (funded from `FreeTierPool`), prepaid credits (Stripe ACH/card top-ups), and metered-subscription deposits (recurring Stripe invoices). All three fund the same draw-down chain. From TigerBeetle's perspective, every VM launch is the same operation: "does this org have enough aggregate balance to run?" Product-specific eligibility and the `included` versus `overage` decision live in PostgreSQL under the per-org advisory lock.

#### Account flags

TigerBeetle accounts have flags that constrain what transfers are permitted. These are set at account creation and cannot be changed.

All accounts are created with `History: true` to enable `GetAccountTransfers` and `GetAccountBalances` queries. This is required for balance drift detection, audit queries, and the reconciliation cron.

| Account | Code | Flags | Effect |
|---------|------|-------|--------|
| `OrgAccountID(orgID, FreeTier)` | 1 | `debits_must_not_exceed_credits`, `history` | Monthly allowance — usage reservations fail when allowance exhausted. |
| `OrgAccountID(orgID, Credit)` | 2 | `debits_must_not_exceed_credits`, `history` | Prepaid balance — cannot go negative. Funds come from Stripe purchases or subscription deposits. |
| `OperatorAccountID(Revenue)` | 3 | `debits_must_not_exceed_credits`, `history` | Earned revenue from paid usage only. Credit-normal — paid VM charges credit it, refunds debit it. Flag prevents refunding more than earned. (`credits_must_not_exceed_debits` would reject the first charge — tested against live TigerBeetle.) |
| `OperatorAccountID(FreeTierExpense)` | 7 | `history` | Cost of free-tier compute given away. Credit-normal — free-tier VM charges credit it. No balance constraint — the expense is bounded by FreeTierPool, not by this account's balance. |
| `OperatorAccountID(FreeTierPool)` | 4 | `debits_must_not_exceed_credits`, `history` | Pool has a finite budget. Free tier grants debit this account — fails if pool exhausted. |
| `OperatorAccountID(StripeHolding)` | 5 | `history` | Contra account. No balance constraint — allows negative balance growth. Each Stripe deposit is a single transfer that debits StripeHolding and credits the org (no separate "receipt" entry). The running negative balance represents total Stripe funds disbursed to org accounts — the contra-entry for real money sitting in Stripe's bank. |
| `OperatorAccountID(PromoPool)` | 6 | `debits_must_not_exceed_credits`, `history` | Promotional credits draw from a funded pool. |
| `OperatorAccountID(ExpiredCredits)` | 8 | `history` | Breakage accumulator for expired paid credits. Credit-normal — expiration sweeps credit it when unused paid grants lapse. |

#### Transfer flags

| Flag | When used |
|------|-----------|
| `linked` | Chain multiple transfers to succeed or fail atomically. Used when a VM reservation should drain free-tier first, then fall back to credits. |
| `balancing_debit` | Auto-clamp the debit amount to available balance. Used for the free-tier leg of a reservation: "debit up to X, but don't exceed what's left." |
| `balancing_credit` | Auto-clamp the credit amount to the account's limit. Used for free-tier monthly reset: "credit up to the monthly cap, don't stack unused balance." |
| `pending` | Reserve funds at VM launch. Moves amount into `debits_pending` / `credits_pending` — reserved but not spent. |
| `post_pending_transfer` | Settle a reservation at VM exit. Can specify a lower amount than the original pending — the difference is released. |
| `void_pending_transfer` | Cancel a reservation entirely. Used when a VM fails to boot or is cancelled before execution. |

#### Two-phase billing (VM lifecycle)

The core billing mechanism is two-phase transfers tied to the VM lifecycle, not periodic aggregation. This gives sub-minute lockout for exhausted accounts without per-second TigerBeetle traffic.

```
1. User requests VM launch (vcpus=2, mem_mib=512)
2. Orchestrator estimates cost for RESERVATION_WINDOW (e.g., 300 seconds):
     reservation = (vcpus × vcpu_rate + mem_gib × mem_rate) × 300
3. Orchestrator reserves funds (two round trips, not one):
     a. Post PENDING transfer:
        id:     VMTransferID(jobID, windowSeq=0, LegFreeTier, KindReservation)
        debit:  OrgAccountID(orgID, FreeTier)
        credit: OperatorAccountID(FreeTierExpense)
        flags:  pending | balancing_debit (clamps to available free-tier balance)
     b. Read back transfer 3a's result to learn the clamped amount
     c. Compute remainder = reservation - clamped_amount
     d. If remainder > 0: post PENDING transfer:
        id:     VMTransferID(jobID, windowSeq=0, LegCredit, KindReservation)
        debit:  OrgAccountID(orgID, Credit)
        credit: OperatorAccountID(Revenue)
        flags:  pending
     e. If 3d fails (insufficient credit balance): void 3a, reject launch
     → If both succeed: funds reserved across two pending transfers, VM boots
4. VM runs
5. Every RESERVATION_WINDOW (300s) while VM is alive:
     a. POST current pending transfers for actual seconds used:
        VMTransferID(jobID, windowSeq, leg, KindSettlement)
        with pending_id referencing the original reservation
     b. Create NEW pending transfers for the next window (windowSeq increments)
     c. If new reservation fails → signal VM to shut down (funds_exhausted)
6. VM exits:
     a. POST final pending transfers for actual seconds consumed
     b. Excess reservation released automatically (partial post)
```

This is a two-round-trip operation per reservation: `balancing_debit` clamps the free-tier transfer to available balance, but TigerBeetle does not auto-route the remainder to another account. The orchestrator must read back the clamped amount from the first transfer's result before computing the second transfer's amount. The two pending transfers are independent (not linked) so they can be individually posted/voided during settlement.

The reservation window is tunable: 60 seconds for aggressive enforcement, 300 seconds for less TigerBeetle traffic. A user who exhausts their balance is locked out within one reservation window.

#### Idempotency

Every transfer has a 128-bit ID. Posting the same ID twice is a no-op — TigerBeetle returns success without double-processing. Transfer IDs are derived deterministically via injective bit packing (see "Deterministic ID derivation" section): `VMTransferID(job_id, window_seq, leg, kind)` for execution-time billing transfers, `SubscriptionPeriodID(subscription_id, period_start, kind)` for periodic subscription deposits, `StripeDepositID(task_id, kind)` for payment-driven tasks, and `CreditExpiryID(grant_id)` for expiry sweeps. The derivation is a pure function of application-layer identifiers — no mapping table, no sequence generator, no external state. Retries after orchestrator crashes resubmit the same IDs by construction.

#### Go client

```go
import tb "github.com/tigerbeetle/tigerbeetle-go"

client, err := tb.NewClient(types.ToUint128(0), []string{"127.0.0.1:3320"})
defer client.Close()
```

Thread-safe, single instance shared across goroutines. The client batches operations automatically. Requires Go >= 1.21, Linux >= 5.6 in production. Import: `types` is `github.com/tigerbeetle/tigerbeetle-go/pkg/types`. The Go SDK version must match the server binary version (0.16.x).

All balance and amount fields in the current `0.16.x` Go client are `types.Uint128`, including `Transfer.Amount`. forge-metal rates and grants are sized to fit in `uint64`, but every conversion from TigerBeetle back to application integers uses an overflow guard before truncation.

#### Security model

TigerBeetle has no authentication or authorization. No passwords, no TLS, no per-client permissions. Any process that can open a TCP connection to port 3320 has full read-write access. This is a deliberate design choice — TigerBeetle is meant to sit behind your application layer.

For this deployment: TigerBeetle listens on `127.0.0.1:3320`. Only local processes (the VM orchestrator, the reconciliation cron, the Stripe webhook worker) can connect. Network binding is the access control. If multi-service access with different permission levels is needed later, the documented pattern is a gateway service that authenticates callers and proxies permitted operations.

#### Observability

TigerBeetle does not emit OpenTelemetry natively. It uses StatsD (DogStatsD format) via an experimental flag:

```
tigerbeetle start --experimental --statsd=127.0.0.1:8125 ...
```

This emits `tb.*` metrics: request latency by operation type, grid cache hits/misses, compaction timing. The OTel Collector's StatsD receiver on `:8125` bridges these into the existing ClickHouse metrics pipeline.

Host-level CPU/RAM/disk metrics come from the OTel Collector's `hostmetricsreceiver` (already deployed). TigerBeetle's memory usage is a flat line (static allocation, locked at startup). Disk I/O shows periodic sequential write bursts (10ms batching window). No north/south traffic — all clients are local.

#### Backup strategy

Single-replica means no replication-based durability. Two complementary backup mechanisms:

1. **ZFS snapshots** of the data file directory. Crash-consistent because TigerBeetle's data file is designed for crash recovery (checksummed, hash-chained, immutable blocks). Schedule via the existing ZFS snapshot automation.
2. **Logical dump** — a small Go program that queries all account balances via the client library and writes them to a JSON file. This provides a human-readable reconstruction path if the data file format changes across a major version upgrade. Run daily via systemd timer.

### Metering: ClickHouse (already deployed)

Raw usage events already flow from smelter into ClickHouse as wide events. Billing extends this with one generic append-only table that records the billable source, the pricing phase selected at reservation time, and the funding-source breakdown that was actually settled.

ClickHouse is the metering source of truth for audit and analytics. It retains full-resolution events permanently. TigerBeetle handles online balance enforcement; PostgreSQL handles product eligibility and grant attribution; ClickHouse provides the immutable execution record for reconciliation, customer usage pages, and dispute review.

New table: `forge_metal.metering`

```sql
CREATE TABLE forge_metal.metering (
    org_id               LowCardinality(String)               CODEC(ZSTD(3)),
    product_id           LowCardinality(String)               CODEC(ZSTD(3)),
    source_type          LowCardinality(String)               CODEC(ZSTD(3)), -- job, request_batch, licensed_period
    source_ref           String                               CODEC(ZSTD(3)), -- product-owned opaque identifier
    window_seq           UInt32                               CODEC(Delta(4), ZSTD(3)),
    started_at           DateTime64(6)                        CODEC(DoubleDelta, ZSTD(3)),
    ended_at             DateTime64(6)                        CODEC(DoubleDelta, ZSTD(3)),
    billed_seconds       UInt32                               CODEC(Delta(4), ZSTD(3)),
    pricing_phase        LowCardinality(String)               CODEC(ZSTD(3)), -- free_tier, included, overage, licensed
    dimensions           Map(LowCardinality(String), Float64) CODEC(ZSTD(3)),
    charge_units         UInt64                               CODEC(T64, ZSTD(3)),
    free_tier_units      UInt64                               CODEC(T64, ZSTD(3)),
    subscription_units   UInt64                               CODEC(T64, ZSTD(3)),
    purchase_units       UInt64                               CODEC(T64, ZSTD(3)),
    promo_units          UInt64                               CODEC(T64, ZSTD(3)),
    refund_units         UInt64                               CODEC(T64, ZSTD(3)),
    exit_reason          LowCardinality(String)               CODEC(ZSTD(3)),
    recorded_at          DateTime64(6) DEFAULT now64(6)       CODEC(DoubleDelta, ZSTD(3))
) ENGINE = MergeTree()
ORDER BY (org_id, product_id, started_at, source_ref, window_seq)
```

For sandbox, `source_type='job'`, `source_ref` is the decimal `job_id`, and `dimensions` carries the resource vectors used for rating (`vcpu_seconds`, `gib_seconds`, `concurrent_vms`, or whatever the product defines). For licensed products there is still a metering row, but `pricing_phase='licensed'` and the row exists for visibility rather than online balance gating.

Required invariant: `charge_units = free_tier_units + subscription_units + purchase_units + promo_units + refund_units`. The orchestrator writes that breakdown once, at settlement time. No `billed` / `billed_at` mutation columns — MergeTree remains append-only.

### Billing Architecture

Billing has two paths: a real-time path that enforces balance limits during product execution, and a reconciliation path that compares ClickHouse, TigerBeetle, and PostgreSQL for drift.

#### Real-time path (VM orchestrator)

The VM orchestrator owns the billing hot path. Every VM launch, renewal, and exit produces TigerBeetle transfers directly — no intermediate queue or aggregation step.

```
VM launch request
    ↓
Orchestrator: estimate cost for reservation window
    ↓
TigerBeetle: pending transfers (free-tier → expense, credit → revenue)
    ↓ success                    ↓ failure
VM boots                    Reject: insufficient balance
    ↓
Every RESERVATION_WINDOW:
    Post current + reserve next
    ↓ reservation fails
    Signal VM shutdown (funds_exhausted)
    ↓
VM exits
    ↓
Post final pending transfer (actual usage)
    ↓
Write metering row to ClickHouse (append-only, once)
```

Pricing lookup comes from PostgreSQL `plans` plus optional `org_pricing_overrides`. The orchestrator computes the reservation amount from the selected rate card, but the data itself is not compiled into the binary. The `included` versus `overage` phase is selected under the org advisory lock at reservation boundaries and written into the `Reservation` plus the final ClickHouse metering row.

#### Reconciliation path (hourly cron)

A Go binary runs hourly via systemd timer. Its job is not to compute charges; the orchestrator already did that. Its job is to verify that the three internal sources of truth still agree.

1. **Query ClickHouse** for unreconciled metering rows using a durable high-water mark stored in `sandbox.billing_cursors`.
   ```sql
   SELECT org_id, product_id, source_ref, window_seq, charge_units,
          free_tier_units, subscription_units, purchase_units, promo_units, refund_units,
          started_at, ended_at
   FROM forge_metal.metering
   WHERE started_at > :last_reconciled_at
     AND started_at <= now() - INTERVAL 5 MINUTE
   ORDER BY started_at
   ```
2. **Query TigerBeetle** for the posted transfers covering the same sources.
3. **Query PostgreSQL** for the `credit_grants`, `billing_events`, and task completions that should explain those transfers.
4. **Compare.** Alert on any of:
   - metering row with no matching settled transfer
   - TigerBeetle transfer with no matching grant/event attribution
   - subscription deposit event with no corresponding grant row
5. **Advance the cursor** only after the comparison succeeds for the full batch.

Stripe is not a metering sink. There is no `/usage_records` synchronization path in this architecture.

#### Crash safety

The real-time path is crash-safe because TigerBeetle transfers are idempotent by ID. If the orchestrator crashes mid-reservation-renewal:

| Failure point | Recovery |
|--------------|----------|
| After pending transfer, before VM boot | Pending transfer exists. Orchestrator restart voids it (VM never ran). |
| During VM execution, before renewal | Firecracker VM continues running unaware of orchestrator state. On restart, the orchestrator discovers orphaned VMs by scanning Firecracker process state, computes actual wall-clock usage from VM start time to now, and posts the pending transfer(s) for the actual amount. If the orchestrator stays down longer than the pending transfer timeout, TigerBeetle voids the pending transfers and the org's funds are released — the operator absorbs the cost of the unmetered compute. Pending transfer timeout must be set longer than the maximum expected orchestrator restart time. |
| After VM exit, before ClickHouse write | TigerBeetle has the financial record. Reconciliation cron detects missing metering row and alerts. ClickHouse row can be backfilled from TigerBeetle transfer data. |

The reconciliation cron is not on the critical path — if it fails, billing enforcement continues via the orchestrator. Drift detection is delayed until the next successful run.

### Identity–Ledger Integration

The Zitadel and TigerBeetle sections above describe each system independently. This section documents the seams where identity state must coordinate with financial state — the integration points where bugs live.

#### org_id as universal tenant key

A single identifier threads through every system in a billing request: the Zitadel organization ID. It is never derived from application-layer state — it originates exclusively from the identity token and propagates unchanged.

```
Zitadel token                    Next.js middleware               PostgreSQL
urn:...:resourceowner:id  ────→  extracts org_id (string)  ────→  WHERE org_id = $1
  "180025476050993153"                    │
                                          ├──→  TigerBeetle
                                          │     account lookup by packed u128
                                          │
                                          └──→  ClickHouse
                                                org_id column in metering row
```

The org_id is `TEXT` in PostgreSQL, `LowCardinality(String)` in ClickHouse, and a packed component of a `u128` in TigerBeetle. The type boundary between string and u128 is where the derivation scheme below operates.

#### Deterministic ID derivation

TigerBeetle uses 128-bit unsigned integers for all account and transfer IDs. There are no string-keyed lookups — every operation requires a numeric ID known in advance. The standard approach is a mapping table in an external database, queried on every billing operation. This deployment eliminates that lookup by deriving TigerBeetle IDs deterministically from application-layer identifiers using injective bit packing.

##### The injectivity requirement

A function `f: A → B` is injective if distinct inputs always produce distinct outputs. For financial IDs, injectivity is not optional — a collision means two accounts or two transfers sharing an ID, and TigerBeetle silently deduplicates (by design, for idempotency). A collision in account IDs means an org's free-tier account overlaps with another org's credit account. A collision in transfer IDs means a legitimate transfer is silently dropped as a duplicate.

A cryptographic hash (blake3, SHA-256) provides probabilistic collision resistance. An injective packing function provides a mathematical guarantee — zero collision probability, proven by construction. The choice between them depends on whether the input space fits in the output space (128 bits):

| Entity | Input fields | Input bits | Fits in 128? |
|--------|-------------|-----------|:------------:|
| Account | org_id(64) + account_type(16) | 80 | Yes |
| VM transfer | job_id(64) + window_seq(32) + leg(8) + phase(8) | 112 | Yes |
| Subscription-period deposit transfer | subscription_id(64) + year_month(16) + kind(8) | 88 | Yes |
| Stripe/task-driven transfer | task_id(64) + kind(8) | 72 | Yes |

Every input space is ≤ 128 bits. No hashing is required anywhere in the system.

This depends on a schema choice: `job_id` and `task_id` in PostgreSQL are `BIGINT` (64 bits), not `UUID` (128 bits). A UUID `job_id` would push the VM transfer input to 192 bits, forcing a hash and surrendering both reversibility and guaranteed uniqueness. On a single PostgreSQL instance, `GENERATED ALWAYS AS IDENTITY` produces monotonically increasing integers without coordination overhead — UUIDs solve a distributed systems problem this deployment doesn't have.

The properties of injective packing vs. hashing:

| Property | Injective packing | Cryptographic hash |
|----------|:-:|:-:|
| Zero collision probability | Yes (proven) | No (probabilistic) |
| Deterministic | Yes | Yes |
| Reversible (extract original fields from ID) | Yes | No |
| LSM-friendly (monotonically increasing) | Yes (if high bits are time-ordered) | No (pseudo-random output) |
| Requires external mapping table | No | No |
| External dependencies | None | Hash library |

##### Byte ordering and LSM performance

TigerBeetle's LSM tree achieves higher throughput with monotonically increasing IDs. The `ID()` helper in every client library generates ULID-style identifiers: `u128 = (timestamp << 80) | random`, placing the timestamp in the most-significant bits so IDs increase over time.

The Go client stores `Uint128` as 16 little-endian bytes. `BytesToUint128` maps `bytes[0:8]` to the low u64 and `bytes[8:16]` to the high u64. Numeric comparison treats the high u64 as the primary sort key. The design rule: **place the temporally-increasing field in bytes [8:16]** (the high u64) so the packed ID's sort order aligns with creation order.

##### Account ID layout

```
128-bit Account ID (16 bytes, little-endian)
┌──────────────────────────────────────┬──────────────────────────────────────┐
│         bytes [0:7] (low u64)        │        bytes [8:15] (high u64)       │
├────────────┬─────────────────────────┼──────────────────────────────────────┤
│ type (u16) │     reserved (48 bits)  │             org_id (u64)             │
└────────────┴─────────────────────────┴──────────────────────────────────────┘

Numeric value: org_id × 2⁶⁴ + account_type
Sort order:    by org (time-ordered snowflake), then by type within org
```

Zitadel organization IDs are 63-bit snowflake-style integers — they encode a millisecond timestamp in the upper bits, so orgs created later have higher IDs. Placing org_id in the high u64 means account IDs increase monotonically as orgs are onboarded, and all accounts for a given org are contiguous in the LSM (they differ only in the low bits).

Operator accounts (no org) use `org_id = 0`. Zitadel snowflake IDs are always large positive numbers, so zero is an unambiguous sentinel. Operator account IDs are small numbers that sort before all org accounts — natural, since they're created once at system initialization.

Account type enum (also stored in TigerBeetle's `code` field):

| Value | Name | Owner | TigerBeetle flags |
|-------|------|-------|-------------------|
| 1 | free-tier | per org | `debits_must_not_exceed_credits` |
| 2 | credit | per org | `debits_must_not_exceed_credits` |
| 3 | revenue | operator (org_id=0) | `debits_must_not_exceed_credits` |
| 4 | free-tier-pool | operator | `debits_must_not_exceed_credits` |
| 5 | stripe-holding | operator | (none — contra account) |
| 6 | promo-pool | operator | `debits_must_not_exceed_credits` |
| 7 | free-tier-expense | operator | (none — expense accumulator) |
| 8 | expired-credits | operator | (none — breakage accumulator) |

The type is encoded in two places: in the account ID (for deterministic derivation without a lookup) and in the `code` field (for `QueryFilter` operations). The `user_data_64` field stores the raw `org_id` for reverse lookups — `QueryFilter{UserData64: orgID}` returns all accounts for an org without decomposing the packed ID.

##### Transfer ID layout

Transfers have different natural keys depending on their kind, but the bit layout is uniform — only the interpretation of the high u64 changes.

```
128-bit Transfer ID (16 bytes, little-endian)
┌──────────────────────────────────────┬──────────────────────────────────────┐
│         bytes [0:7] (low u64)        │        bytes [8:15] (high u64)       │
├───────────┬────────┬────────┬────────┼──────────────────────────────────────┤
│  seq (32) │leg (8) │kind (8)│rsv (16)│           source_id (u64)            │
└───────────┴────────┴────────┴────────┴──────────────────────────────────────┘

Numeric value: source_id × 2⁶⁴ + (reserved << 48) + (kind << 40) + (leg << 32) + seq
```

**source_id** (high u64): The primary entity this transfer belongs to. For VM transfers: `job_id` (from `jobs.job_id`). For subscription-period deposits: `subscription_id` (from `subscriptions.subscription_id`). For payment-driven tasks: `task_id` (from `tasks.task_id`). For expiry sweeps: `grant_id` (from `credit_grants.grant_id`). Always a `BIGINT` from PostgreSQL, always monotonically increasing, always in the high bits for LSM ordering.

**seq** (u32): Sequence number within the source. VM transfers: reservation window number (0 = initial, 1 = first renewal, ...). Subscription-period deposits: `year × 12 + month` derived from `period_start.UTC()`. Stripe deposits: 0 (one transfer per task).

**leg** (u8): Which leg of a multi-transfer operation. The two-phase billing flow creates two transfers per reservation window: leg 0 = free-tier debit, leg 1 = credit debit. Single-leg transfers (resets, deposits) use leg 0.

**kind** (u8): Transfer kind enum. Distinguishes reservation from settlement from void — critical because the same `(job_id, window_seq, leg)` triple appears in both the pending transfer and its post/void, and they must have different IDs.

Transfer kind enum (also stored in TigerBeetle's `code` field):

| Value | Name | source_id | TigerBeetle flags |
|-------|------|-----------|-------------------|
| 1 | vm_reservation | job_id | `pending` + `balancing_debit` (free-tier leg → FreeTierExpense) or `pending` (credit leg → Revenue) |
| 2 | vm_settlement | job_id | `post_pending_transfer` |
| 3 | vm_void | job_id | `void_pending_transfer` |
| 4 | free_tier_reset | subscription_id | `balancing_credit` |
| 5 | stripe_deposit | task_id | (single-phase) |
| 6 | subscription_deposit | task_id | (single-phase) |
| 7 | promo_credit | task_id | (single-phase) |
| 8 | dispute_debit | task_id | `balancing_debit` (clamps to available credit balance; debits org/credit, credits StripeHolding) |
| 9 | credit_expiry | grant_id | `balancing_debit` for org-side debit, single-phase sweep into `ExpiredCredits` or `FreeTierExpense` |

##### Transfer idempotency by construction

Each transfer kind has a distinct idempotency domain — the set of fields that uniquely identify "has this already been done?"

| Kind | Idempotency key | Guarantee |
|------|----------------|-----------|
| vm_reservation | (job_id, window_seq, leg) | One pending reservation per (job, window, leg). Orchestrator crash + retry resubmits the same ID. |
| vm_settlement | (job_id, window_seq, leg) | One settlement per pending. Different `kind` byte prevents collision with the pending. |
| vm_void | (job_id, window_seq, leg) | One void per pending. Mutually exclusive with settlement (enforced by TigerBeetle — pending resolves at most once). |
| free_tier_reset | (subscription_id, year_month) | One periodic free-plan deposit per subscription period. Re-running the deposit job produces the same transfer IDs — TigerBeetle returns `exists`. |
| stripe_deposit | (task_id) | One deposit per task. Duplicate Stripe webhooks produce duplicate task rows blocked by `UNIQUE(idempotency_key)` in PostgreSQL — the first layer. Same task_id → same transfer ID → TigerBeetle deduplication — the second layer. |
| dispute_debit | (task_id) | One debit per dispute task. Uses `KindDisputeDebit` (8) in the kind byte, distinct from `KindStripeDeposit` (5), preventing ID collision between a deposit and a dispute sharing the same task_id. |
| credit_expiry | (grant_id) | One expiry sweep per grant. Uses `KindCreditExpiry` (9), distinct from subscription and dispute flows. |

Two layers of idempotency for Stripe deposits: PostgreSQL `UNIQUE` prevents duplicate tasks, TigerBeetle's ID-based deduplication prevents duplicate transfers. Either layer alone is sufficient; both together handle every crash/retry/webhook-replay scenario.

##### Go type system

The packing functions are the only way to construct IDs. Distinct Go types prevent category errors at compile time:

```go
type OrgID  uint64
type JobID  int64   // BIGINT GENERATED ALWAYS AS IDENTITY
type SubscriptionID int64
type TaskID int64
type GrantID int64

type AccountID  struct{ raw types.Uint128 }  // cannot be used as TransferID
type TransferID struct{ raw types.Uint128 }  // cannot be used as AccountID

func OrgAccountID(org OrgID, t OrgAcctType) AccountID {
    var id [16]byte
    binary.LittleEndian.PutUint16(id[0:2], uint16(t))
    binary.LittleEndian.PutUint64(id[8:16], uint64(org))
    return AccountID{tb.BytesToUint128(id)}
}

func OperatorAccountID(t OperatorAcctType) AccountID {
    var id [16]byte
    binary.LittleEndian.PutUint16(id[0:2], uint16(t))
    // bytes [8:16] = 0 (operator sentinel)
    return AccountID{tb.BytesToUint128(id)}
}

func VMTransferID(job JobID, seq uint32, leg Leg, kind XferKind) TransferID {
    var id [16]byte
    binary.LittleEndian.PutUint32(id[0:4], seq)
    id[4] = uint8(leg)
    id[5] = uint8(kind)
    binary.LittleEndian.PutUint64(id[8:16], uint64(job))
    return TransferID{tb.BytesToUint128(id)}
}

func SubscriptionPeriodID(sub SubscriptionID, periodStart time.Time, kind XferKind) TransferID {
    t := periodStart.UTC()
    var id [16]byte
    binary.LittleEndian.PutUint32(id[0:4], uint32(t.Year())*12+uint32(t.Month()))
    id[5] = uint8(kind)
    binary.LittleEndian.PutUint64(id[8:16], uint64(sub))
    return TransferID{tb.BytesToUint128(id)}
}

func StripeDepositID(task TaskID, kind XferKind) TransferID {
    var id [16]byte
    id[5] = uint8(kind)
    binary.LittleEndian.PutUint64(id[8:16], uint64(task))
    return TransferID{tb.BytesToUint128(id)}
}

func CreditExpiryID(grant GrantID) TransferID {
    var id [16]byte
    id[5] = uint8(KindCreditExpiry)
    binary.LittleEndian.PutUint64(id[8:16], uint64(grant))
    return TransferID{tb.BytesToUint128(id)}
}
```

Reverse functions exist for debugging — given any TigerBeetle ID from logs or the REPL, extract the original fields without a mapping table:

```go
func (a AccountID) Parse() (org OrgID, acctType uint16) {
    b := a.raw.Bytes()
    acctType = binary.LittleEndian.Uint16(b[0:2])
    org = OrgID(binary.LittleEndian.Uint64(b[8:16]))
    return
}

func (t TransferID) Parse() (sourceID uint64, seq uint32, leg uint8, kind uint8) {
    b := t.raw.Bytes()
    seq = binary.LittleEndian.Uint32(b[0:4])
    leg = b[4]
    kind = b[5]
    sourceID = binary.LittleEndian.Uint64(b[8:16])
    return
}
```

##### user_data fields

TigerBeetle indexes `user_data_128`, `user_data_64`, and `user_data_32` for `QueryFilter` operations. The packed ID handles derivation; the `user_data` fields handle queries.

| Field | Accounts | Transfers |
|-------|----------|-----------|
| `user_data_64` | `org_id` — find all accounts for an org | `org_id` — find all transfers for an org |
| `user_data_32` | 0 (unused) | `window_seq` — find transfers for a specific reservation window |
| `user_data_128` | 0 (unused) | 0 (reserved for future use) |

##### Asset scale

All TigerBeetle amounts use a fixed-point scale of `10⁷` (7 decimal places). USD has 2 decimal places, but sub-cent pricing for vCPU-seconds requires more precision. `10⁷` gives 5 digits below the cent, handling rates like `$0.0000325/vCPU-second` without rounding until the final invoice.

`$1.00 = 10,000,000 ledger units`. All accounts use the same `ledger` value (1). Multi-currency would use a second ledger value with its own scale.

##### Batch size limits

TigerBeetle's protocol limits each batch to **8191 items** (accounts or transfers). `CreateAccounts` and `CreateTransfers` accept up to 8191 entries per call. Exceeding this returns `ErrMaximumBatchSizeExceeded`. Query operations (`QueryAccounts`, `QueryTransfers`, `GetAccountTransfers`) return at most **8189 results** per call — pagination uses `TimestampMin`/`TimestampMax` from the last result's `Timestamp` field.

For operations processing more items (e.g., `ResetFreeTier` across 10,000 orgs), batch into groups of 8190 (leaving 1 entry of headroom).

##### Debugging: reading a raw u128

When inspecting TigerBeetle state via the REPL or logs, every ID is self-describing. No mapping table to consult, no hash to reverse.

Account ID example (`0x027f4e8a5c0100000100000000000000` in hex):
```
bytes [8:16] → org_id = 180025476050993153  (decimal)
bytes [0:2]  → type = 1                     (free-tier)
→ This is org 180025476050993153's free-tier account.
```

Transfer ID example:
```
bytes [8:16] → source_id = 4217   (job_id)
bytes [0:4]  → seq = 3            (window 3)
byte  [4]    → leg = 1            (credit leg)
byte  [5]    → kind = 2           (vm_settlement)
→ This is the settlement of job 4217, window 3, credit leg.
```

#### Org provisioning sequence

When a new customer org appears for the first time, three systems need state: Zitadel (org already exists — created during onboarding or self-service signup), PostgreSQL (orgs row), and TigerBeetle (free-tier + credit accounts). These cannot be provisioned atomically across three systems.

```
User's first authenticated request
  │
  ├─ 1. Zitadel: org exists (created out-of-band)
  │     Token carries org_id in urn:...:resourceowner:id
  │
  ├─ 2. Next.js middleware: extract org_id from token
  │
  ├─ 3. PostgreSQL: INSERT INTO orgs ... ON CONFLICT DO NOTHING
  │     (idempotent — concurrent first-requests from same org are safe)
  │
  ├─ 4. TigerBeetle: create_accounts([
  │       OrgAccountID(orgID, FreeTier),
  │       OrgAccountID(orgID, Credit)
  │     ])
  │     (idempotent — duplicate account IDs return `exists`, not error)
  │
  └─ 5. Return response (org context ready)
```

Both PostgreSQL (`ON CONFLICT DO NOTHING`) and TigerBeetle (`create_accounts` returns `exists` for duplicate IDs) handle concurrent provisioning without locking. Two requests from the same org arriving simultaneously both succeed — the second is a no-op in both systems.

Failure matrix:

| Failure point | State | Recovery |
|--------------|-------|----------|
| After step 3, before step 4 | PostgreSQL has orgs row, TigerBeetle has no accounts | Next request retries step 4. Deterministic ID derivation ensures the same account IDs are submitted. |
| Step 4 partial (free-tier created, credit fails) | One account exists | `create_accounts` is a batch — TigerBeetle creates both or neither per batch. If the batch is split across calls, the retry recreates both; the existing one returns `exists`. |
| Zitadel org deleted after provisioning | PostgreSQL and TigerBeetle have orphaned state | No automatic cleanup. The reconciliation cron can detect orgs in PostgreSQL with no corresponding Zitadel org and flag them. TigerBeetle accounts with zero balance are inert. |

Provisioning is lazy (triggered by first authenticated request), not eager (triggered by org creation in Zitadel). Lazy provisioning is simpler — no Zitadel webhook or action needed — and the idempotent retry paths make it robust. An org that exists in Zitadel but has never authenticated has no PostgreSQL row and no TigerBeetle accounts, which is correct (no free tier, no billing state for an unused org).

#### VM launch: authorization gate → balance gate

The VM launch request passes through two sequential gates. The authorization gate (Zitadel) is a local JWT validation. The balance gate (TigerBeetle) is a network call.

```
Client → POST /api/v1/vms {vcpus: 2, mem_mib: 512}
  │
  ├─ 1. JWT validation (in-process, ~0ms)
  │     Extract org_id, roles from token claims
  │     └─ Invalid token → 401 Unauthorized
  │
  ├─ 2. Role check: roles includes "sandbox:user" or "sandbox:admin"
  │     └─ Missing role → 403 Forbidden
  │
  ├─ 3. Org provisioning (idempotent, skip if exists)
  │     PostgreSQL upsert + TigerBeetle create_accounts
  │
  ├─ 4. Pricing lookup under pg_advisory_xact_lock(org_id::bigint) (~1ms)
  │     Read active subscription, plan, and optional org_pricing_overrides
  │     Select pricing phase: free_tier, included, or overage
  │     Compute reservation = rate_card(dimensions) × WINDOW
  │
  ├─ 5. TigerBeetle reservation (two round trips, ~2ms)
  │     a. Pending transfer: debit {org}/free-tier → credit operator/free-tier-expense
  │        (balancing_debit — clamps to available free-tier balance)
  │     b. Read back clamped amount, compute remainder
  │     c. If remainder > 0: pending transfer: debit {org}/credit → credit operator/revenue
  │     └─ Insufficient funds → 402 Payment Required
  │     └─ TigerBeetle unavailable → 503 Service Unavailable
  │
  ├─ 6. Firecracker VM boot (~125ms from snapshot)
  │     └─ Boot failure → void pending transfers, 500 Internal Server Error
  │
  └─ 7. Return {job_id, status: "running"}
```

**Fail-closed policy**: If TigerBeetle is unavailable, no VMs launch (503). Fail-open on a billing gate means giving away free compute with no ledger record. The orchestrator does not maintain a local balance cache or "grace period" — TigerBeetle is the single source of truth for balance enforcement.

**Latency budget**: JWT validation is in-process (~0ms). PostgreSQL pricing lookup plus advisory lock is ~1ms. TigerBeetle reservation is ~2ms (two round trips at ~1ms each). Total gate overhead is still negligible against Firecracker's ~125ms snapshot boot.

**Void-on-boot-failure**: If Firecracker fails to start after funds are reserved, the orchestrator immediately voids the pending transfers. The transfer IDs for the void are deterministic — `VMTransferID(jobID, 0, leg, KindVoid)` — so a crash between the failed boot and the void is recovered on restart by resubmitting the same void.

#### Machine user auth boundaries

The orchestrator, reconciliation cron, and Stripe webhook worker are Zitadel machine users. TigerBeetle has no authentication — any process on `127.0.0.1:3320` has full read-write access. The Zitadel identity serves two purposes: accessing Zitadel's own Management API (e.g., listing orgs), and providing audit trail metadata (which service performed which operation).

```
                         Zitadel-authenticated      Direct (localhost, no auth)
orchestrator             Zitadel API (list orgs)    TigerBeetle :3320 (Go client)
                         PostgreSQL (pricing)        ClickHouse (metering writes)

reconciliation-cron      —                          TigerBeetle :3320 (Go client)
                                                     ClickHouse (metering reads)
                                                     PostgreSQL (watermark)

stripe-webhook-worker    Next.js API (webhook)      TigerBeetle :3320 (Go client)
                         PostgreSQL (task queue)
```

The orchestrator talks to TigerBeetle directly via the Go client — not through a Next.js API layer. This means billing logic (reservation, settlement, void) lives in the Go orchestrator binary, not in the Next.js application. The Next.js Sandbox app reads TigerBeetle for aggregate balance display and PostgreSQL for product-specific grant attribution.

The machine user identity is stored in TigerBeetle's `user_data` fields on transfers for audit. It is not used for access control — network binding (`127.0.0.1:3320`) is the only access control mechanism.

#### Subscription credit deposit flow

Included credits are deposited per subscription period, not via a global "reset every org on the 1st" job. The scheduler walks active subscriptions whose current period has started and materializes the corresponding grant for that period exactly once.

```
Cron (daily, systemd timer)
  │
  ├─ 1. Query PostgreSQL: active subscriptions whose current_period_start <= now()
  │
  ├─ 2. For each subscription:
  │     ├─ Load plan
  │     ├─ If included_credits = 0: skip
  │     ├─ Check UNIQUE(subscription_id, period_start, account_type)
  │     ├─ Derive transfer ID from subscription_id + period marker
  │     ├─ Choose source/account:
  │     │   free plan       → source=free_tier,   debit FreeTierPool  → credit org/FreeTier
  │     │   paid metered    → source=subscription, debit StripeHolding → credit org/Credit
  │     └─ Insert credit_grants row for this product period
  │
  └─ 3. Log summary to ClickHouse / billing_events
```

Idempotency is two-layered: PostgreSQL blocks duplicate grant rows for the same `(subscription_id, period_start, account_type)`, and TigerBeetle blocks duplicate transfers by deterministic ID. Annual subscriptions still deposit monthly drips rather than a single twelve-month grant.

#### Stripe payment → org credit: identity resolution

Stripe webhooks contain a Stripe customer ID, not a Zitadel org ID. The mapping is stored in PostgreSQL.

```
Stripe webhook (payment_intent.succeeded)
  │
  ├─ 1. Payload: {customer_id, amount, payment_intent_id}
  │
  ├─ 2. PostgreSQL: SELECT org_id FROM orgs WHERE stripe_customer_id = $1
  │     (mapping created when the org first initiates a Stripe Checkout session)
  │
  ├─ 3. PostgreSQL: INSERT INTO tasks (task_type, payload, idempotency_key)
  │     VALUES ('stripe_purchase_deposit', {...}, payment_intent_id)
  │     ON CONFLICT (idempotency_key) DO NOTHING
  │     RETURNING task_id
  │     (UNIQUE constraint = first idempotency layer)
  │
  ├─ 4. Worker picks up task via SKIP LOCKED:
  │     ├─ Derive transfer ID: StripeDepositID(taskID, KindStripeDeposit)
  │     ├─ Derive account IDs:
  │     │   debit:  OperatorAccountID(StripeHolding)
  │     │   credit: OrgAccountID(orgID, Credit)
  │     ├─ Submit transfer
  │     │   (TigerBeetle deduplication = second idempotency layer)
  │     └─ Mark task completed
  │
  └─ 5. If transfer fails → task enters retrying/dead state per billing queue policy
```

`stripe_customer_id` is owned by the billing package in the `sandbox.orgs` table. Product-local databases key off `org_id`; they do not duplicate Stripe customer mapping or billing subscription state.

### Payments: Stripe (external)

Accepted external dependency. Go SDK: `github.com/stripe/stripe-go/v85`. The implementation uses `stripe.NewClient(apiKey)` and `webhook.ConstructEvent(payload, signature, secret)`.

Stripe is cash collection and subscription-lifecycle truth. It is not the metering sink. The billing system branches on the product's billing model.

#### Prepaid credits (one-time purchase)

Customer buys credits via `POST /v1/checkout/sessions` in `mode=payment`. On `payment_intent.succeeded`, the worker enqueues `stripe_purchase_deposit`, derives `StripeDepositID(task_id, KindStripeDeposit)`, and posts:

```
debit:  OperatorAccountID(StripeHolding)
credit: OrgAccountID(orgID, Credit)
amount: purchased_amount_in_ledger_units
```

The corresponding `credit_grants` row is product-scoped (`source='purchase'`) even though TigerBeetle remains aggregate per org.

#### Metered subscriptions (included credits plus optional overage)

Customer subscribes via `POST /v1/checkout/sessions` in `mode=subscription`. Stripe collects the invoice; the periodic deposit job materializes the included credits into TigerBeetle plus PostgreSQL:

```
free plan       → debit FreeTierPool   → credit org/FreeTier   (source='free_tier')
paid metered    → debit StripeHolding  → credit org/Credit     (source='subscription')
```

The overage decision is not made in Stripe. It is made at reservation boundaries in PostgreSQL by checking whether included-grant eligibility remains for that product under the org advisory lock. If included credits are exhausted and the plan exposes `overage_unit_rates`, the next reservation window is priced at the overage rate card. There is no mid-window split between included and overage phases.

Annual billing uses the same subscription rows with `cadence='annual'`, but included credits are still deposited monthly. This reduces operator exposure if the annual invoice is later disputed.

#### Licensed subscriptions

Licensed products do not create `credit_grants`. On `invoice.paid`, the worker enqueues `stripe_licensed_charge` and records the recurring invoice directly in TigerBeetle:

```
debit:  OperatorAccountID(StripeHolding)
credit: OperatorAccountID(Revenue)
amount: licensed_invoice_amount
```

Access is then gated by `subscriptions.status`, not by a draw-down account balance.

#### Smart Retries and cancellation

Smart Retries is a Stripe Dashboard setting, not an API parameter. This design assumes:

- Smart Retries enabled
- retry window configured to `8` attempts within `2 weeks`
- terminal action configured to cancel the subscription

Under that configuration, every failed attempt emits `invoice.payment_failed`, eventual recovery emits `invoice.paid`, and retry exhaustion emits `customer.subscription.deleted`. If the Stripe account is configured to leave subscriptions `past_due` or `unpaid` instead, the billing package assumptions no longer hold.

Annual cancellation with refund uses `POST /v1/credit_notes` against the finalized invoice before deleting the subscription. `DELETE /v1/subscriptions/{id}` with `prorate=true` alone is not the refund artifact this architecture relies on.

#### Storefront payments

Storefront still uses standard Stripe Checkout and Stripe Billing for hardware/server purchases. Those are product transactions, not metered balance movements. The billing package owns customer correlation and subscription cash collection, but storefront order provisioning remains product-local.

#### Customer-facing usage

Usage visibility is a Next.js page backed by ClickHouse plus TigerBeetle, not Stripe's dashboard. Stripe is the payment rail, not the usage display.

### Task Queue: PostgreSQL SKIP LOCKED

No message broker. Async billing work (webhook side effects, periodic deposits, dispute handling, trust-tier evaluation) uses PostgreSQL as a task queue with retries and dead-letter visibility:

```sql
UPDATE tasks
SET status = 'claimed', claimed_at = now(), attempts = attempts + 1
WHERE task_id = (
    SELECT task_id
    FROM tasks
    WHERE status IN ('pending', 'retrying')
      AND (next_retry_at IS NULL OR next_retry_at <= now())
    ORDER BY scheduled_at
    FOR UPDATE SKIP LOCKED
    LIMIT 1
)
RETURNING *;
```

On failure, the worker transitions the row to `retrying` with exponential backoff (`5s, 10s, 20s, 40s, 80s`). When `attempts >= max_attempts`, the row moves to `dead` and requires explicit operator replay or resolution.

This covers the current single-node concurrency patterns without adding infrastructure. When fan-out or multi-node coordination becomes necessary, NATS JetStream is the first upgrade path. Kafka remains categorically excluded for single-node deployments.

### Messaging: Deferred

No NATS, no Kafka, no Redis pub/sub. Current architecture has no fan-out pattern (multiple consumers of the same event). All async flows are point-to-point task execution. Revisit when adding a second node or when a genuine pub/sub need emerges.

## What Is Not In This Stack

| Excluded | Reason |
|----------|--------|
| Kafka | Designed for distributed clusters. ~1 GB+ RAM, ZooKeeper/KRaft overhead. No single-node justification. |
| NATS JetStream | Good technology, no current need. PostgreSQL SKIP LOCKED covers task queue patterns. First candidate when multi-node or fan-out arrives. |
| Lago | AGPL license (copyleft, incompatible with distribution). Overlaps with TigerBeetle on credit management. Ruby/Rails + Redis + Sidekiq adds ~1 GB RAM for billing UI we don't need. |
| OpenMeter | Requires Kafka. Overlaps with existing ClickHouse metering pipeline. |
| Keycloak | 1.5-2 GB JVM footprint. Multi-tenant orgs bolted on in v25, not native. Zitadel covers the same OIDC/SAML surface in ~512 MB. |
| Authentik | ~4 GB RAM (Python server + worker). Soft multi-tenancy via brands/domains — no first-class org model or org-admin delegation. |
| Separate metering service | ClickHouse already ingests microsecond-resolution events from smelter. Adding a metering service creates a redundant event store. |

## Resource Budget

Estimated memory footprint of new components alongside existing services:

| Component | RAM | Status |
|-----------|-----|--------|
| Caddy | ~50 MB | existing |
| ClickHouse | ~1 GB | existing |
| HyperDX + MongoDB | ~1.5 GB | existing |
| OTel Collector | ~200 MB | existing |
| Forgejo | ~300 MB | existing |
| Firecracker VMs (per job) | ~256 MB each | existing |
| **Zitadel** | **~512 MB** | **new** |
| **PostgreSQL** | **~500 MB** | **new** |
| **TigerBeetle** | **~1-2 GB** | **new** |
| **Sandbox app (Next.js)** | **~200 MB** | **new** |
| **Storefront app (Next.js)** | **~200 MB** | **new** |

New components add ~2.4-3.4 GB. Total platform footprint ~6.3-7.3 GB, well within a 64 GB bare-metal box. The TigerBeetle figure reflects the actual runtime working set (grid cache locked into memory), not the binary size. The Zitadel figure reflects production PostgreSQL caching overhead, not the binary size (~50 MB).

## Deployment Model

All new components follow the existing pattern: Nix closure for binaries, Ansible role for configuration, systemd for lifecycle, Caddy for TLS termination.

```
Nix closure additions:
├── zitadel          # single binary
├── postgresql       # server + client
├── tigerbeetle      # single binary
└── (Next.js apps built separately, deployed as Node processes)

Ansible roles (new):
├── postgresql/      # instance, databases, users, pg_hba
├── zitadel/         # config, systemd, initial org bootstrap
├── tigerbeetle/     # data file format (idempotent), systemd, CAP_IPC_LOCK
├── sandbox_app/     # Next.js process, env, systemd
└── storefront_app/  # Next.js process, env, systemd

Caddy routes (additions):
├── auth.<domain>    → Zitadel :8085 (h2c — required for gRPC)
├── sandbox.<domain> → Sandbox app :3001
└── store.<domain>   → Storefront app :3002
```

## Implementation Order

1. **PostgreSQL role** — dependency for everything else
2. **Zitadel role** — auth must exist before apps can authenticate
3. **TigerBeetle role** — stand up in isolation with Ansible role, systemd unit, OTel Collector StatsD receiver
4. **TigerBeetle stress test** — Go harness that creates the account model, hammers transfers at increasing batch sizes, validates two-phase reservation flow, measures TPS ceiling on Latitude.sh hardware
5. **Storefront app** — simpler CRUD, proves auth + Stripe Checkout end-to-end
6. **Sandbox metering table** — extend ClickHouse schema
7. **Sandbox app + orchestrator billing** — two-phase transfers wired into VM lifecycle
8. **Reconciliation cron** — hourly ClickHouse ↔ TigerBeetle consistency check + Stripe usage sync
