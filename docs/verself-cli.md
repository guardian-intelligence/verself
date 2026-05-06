# Verself CLI

`verself` is the customer, operator, and bootstrap facade for the Verself
platform. It sits above the curated SDKs, which wrap generated OpenAPI clients
for product services. Browser server functions, the CLI, and customer
automation use the same service contracts with different auth flows and local
state handling.

The CLI has two execution modes:

- service mode: authenticated commands call public or SPIFFE-only service APIs
  through SDK packages;
- bootstrap mode: commands materialize the local repo/site substrate required
  before product services exist, then hand product state to services once IAM is
  reachable.

`aspect` remains the repo task runner for contributors and agents. `verself`
becomes the public product CLI and can wrap selected `aspect` tasks when it is
running from a checkout.

## Layering

The command pipeline is:

```text
CLI command
  -> local profile and project resolution
  -> curated SDK operation
  -> generated OpenAPI client
  -> service API
  -> service-owned PostgreSQL, Zitadel, SpiceDB, ClickHouse, or external adapter
```

Service calls go through SDK operations. Missing service behavior is added at
the Huma route/OpenAPI layer, regenerated into clients, and wrapped by the SDK.

Bootstrap has a bounded pre-service path:

```text
bootstrap input
  -> normalized manifest
  -> site files and encrypted bootstrap material
  -> provisioning and host convergence
  -> component/service deployment
  -> IAM bootstrap claim
  -> verified owner login
```

Pre-service bootstrap writes files under `src/host/sites/<site>/` and invokes
the existing provisioning/deploy surfaces. Product state, including organization
ownership and trust tier, is written through service APIs once those services
exist.

## Command Shape

Commands follow a small global grammar:

```text
verself [global flags] <noun> <verb> [args] [flags]
```

Global flags:

| Flag | Meaning |
| --- | --- |
| `--profile <name>` | Selects the local profile for endpoint and credential resolution. |
| `--org <id-or-slug>` | Selects the organization for org-scoped operations. |
| `--project <id-or-slug>` | Selects the project for project-scoped operations. |
| `--json` | Emits structured JSON without progress text. |
| `--no-color` | Disables ANSI styling. |
| `--cwd <path>` | Resolves project-local state from another working directory. |
| `--traceparent <value>` | Joins an existing distributed trace. |

Resolution order:

1. explicit command flags;
2. environment variables such as `VERSELF_PROFILE`, `VERSELF_ORG`, and
   `VERSELF_PROJECT`;
3. project-local `.verself/project.json`;
4. active profile defaults;
5. interactive selection when stdin is a terminal;
6. a typed ambiguity error.

When multiple valid organizations or projects exist, interactive commands
prompt and non-interactive commands return a typed ambiguity error.

## XDG State

The CLI uses XDG locations for durable local state. Platform adapters may map
these locations to native operating-system directories when the XDG variables
are unset, but the logical layout remains stable.

| Purpose | Directory | Default POSIX path | Contents |
| --- | --- | --- | --- |
| Config | `$XDG_CONFIG_HOME/verself` | `~/.config/verself` | Non-secret profile and CLI configuration. |
| State | `$XDG_STATE_HOME/verself` | `~/.local/state/verself` | Mutable command state, selected org/project history, bootstrap run records. |
| Data | `$XDG_DATA_HOME/verself` | `~/.local/share/verself` | Durable non-secret data such as downloaded discovery manifests and plugin metadata. |
| Cache | `$XDG_CACHE_HOME/verself` | `~/.cache/verself` | Rebuildable caches: OpenAPI discovery, org/project display-name cache, SDK schema cache. |
| Runtime | `$XDG_RUNTIME_DIR/verself` | process-local `0700` temp dir when unset | Locks, auth callback sockets, PKCE state, nonce files, and short-lived IPC. |

Directories are created with mode `0700`. Config and state files are written
with mode `0600` through an atomic write, fsync, and rename sequence. Commands
that mutate a profile acquire a per-profile lock under the runtime directory.

Secrets are stored through the OS credential store:

| Platform | Store |
| --- | --- |
| macOS | Keychain |
| Linux desktop | Secret Service or KWallet |
| Windows | Credential Manager |
| Headless Linux | Explicit token file or environment variable supplied per command |

XDG config/state/cache files remain free of refresh tokens, API credentials,
Cloudflare tokens, Latitude.sh tokens, and Zitadel admin material. A profile
stores only a credential reference.

## Profile Model

A profile represents one Verself installation plus one local account context.
Profiles are named with `[A-Za-z0-9_.-]+` and contain no path separators.

`$XDG_CONFIG_HOME/verself/config.toml`:

```toml
version = 1
active_profile = "guardian-prod"

[profiles.guardian-prod]
kind = "operator"
root_url = "https://verself.sh"
discovery_url = "https://verself.sh/.well-known/verself"
auth_issuer = "https://auth.verself.sh"
console_url = "https://verself.sh"
credential_ref = "verself://profiles/guardian-prod/oauth"
default_org = "guardianintelligence.org"
default_project = "verself"

[profiles.local-clone]
kind = "bootstrap-local"
repo_root = "/home/ubuntu/Projects/verself-sh"
site = "prod"
```

Profile kinds:

| Kind | Use |
| --- | --- |
| `customer` | Normal API/console usage against a Verself installation. |
| `operator` | Human operator usage with access to operator-only APIs. |
| `bootstrap-local` | A checkout that can materialize site files and run provisioning/deploy commands. |
| `ci` | Non-interactive profile using an API credential or workload credential. |

Selection commands:

```text
verself profiles list
verself profiles add guardian-prod --root-url https://verself.sh
verself profiles use guardian-prod
verself profiles inspect guardian-prod --json
verself profiles remove guardian-prod
```

`verself profiles add` performs service discovery from `root_url` and stores the
resolved endpoints. A failed discovery can be repaired with `verself profiles
refresh <name>`.

## Project-Local State

`verself link` writes `.verself/project.json` in the repository or application
root:

```json
{
  "version": 1,
  "profile": "guardian-prod",
  "org": "guardianintelligence.org",
  "project": "verself",
  "root_directory": "."
}
```

This file is non-secret and may be committed by projects that want deterministic
deployment linkage. User-specific overrides belong in XDG profile config.

## Auth UX

Interactive auth:

```text
verself auth login
verself auth logout
verself auth whoami
verself auth token --audience iam-service
```

`auth login` uses OAuth device authorization when the server supports it and
authorization code with PKCE otherwise. PKCE verifiers, nonce values, and local
callback listener state live only under the runtime directory. Refresh tokens
are stored in the OS credential store. Access tokens are held in process memory
and refreshed as needed.

Headless auth:

```text
VERSELF_TOKEN=... verself deploy --profile ci --json
verself auth login --token-file /run/secrets/verself-token
```

Token files must be regular files with owner-only permissions. The CLI refuses
world-readable token files.

## Vercel-Compatible Product UX

The first public command surface should feel familiar to Vercel users while
remaining aligned with Verself's organization and service model:

```text
verself login
verself whoami
verself switch
verself link
verself deploy
verself pull
verself env ls|get|set|rm
verself domains ls|add|rm|verify
verself projects ls|create|inspect|update|rm
verself orgs ls|create|use|inspect|update
verself orgs members ls|invite|update|remove
verself logs
verself billing status
verself iam policies get|set|add-binding|remove-binding
verself iam test-permissions
```

`teams` can be accepted as an alias for `orgs` for migration ergonomics:

```text
verself teams list
verself teams switch guardianintelligence.org
```

Aliases should be visible in help output and share the same implementation as
the canonical command.

## Bootstrap UX

Bootstrap accepts business-level inputs and derives the site manifest.

```text
verself bootstrap init \
  --site prod \
  --product-domain verself.sh \
  --company-domain guardianintelligence.org \
  --company-name "Guardian Intelligence" \
  --owner-alias shovon \
  --owner-name "Shovon Hasan" \
  --latitude-project-id <project-id> \
  --latitude-region ASH \
  --latitude-plan s3-large-x86
```

Provider credentials are supplied through environment variables or stdin:

```text
CLOUDFLARE_API_TOKEN=... LATITUDESH_AUTH_TOKEN=... verself bootstrap init ...
printf '%s' "$CLOUDFLARE_API_TOKEN" | verself bootstrap set-secret cloudflare-api-token --stdin
printf '%s' "$LATITUDESH_AUTH_TOKEN" | verself bootstrap set-secret latitudesh-auth-token --stdin
```

Token-valued command-line flags are avoided because shells record argv in
history and process listings.

Bootstrap phases:

```text
verself bootstrap init
verself bootstrap plan
verself bootstrap provision
verself bootstrap converge-host
verself bootstrap deploy
verself bootstrap claim prepare
verself auth login
verself bootstrap verify
```

`bootstrap init` writes the local shared variable store for a checkout:

- `src/host/sites/<site>/vars.yml`;
- `src/host/sites/<site>/provisioning.tfvars.json`;
- `src/host/sites/<site>/secrets/*.sops.yml`;
- profile entries under `$XDG_CONFIG_HOME/verself/config.toml`;
- bootstrap run records under `$XDG_STATE_HOME/verself/bootstrap/<run-id>.json`.

`bootstrap claim prepare` calls `iam-service` after deployment and creates a
pending platform-owner claim. `auth login` completes the claim after Zitadel
verifies the owner email.

For Guardian, the derived claim is:

| Field | Value |
| --- | --- |
| Owner email | `shovon@guardianintelligence.org` |
| Organization name | `guardianintelligence.org` |
| Company domain | `guardianintelligence.org` |
| Trust tier | `platform` |

## Derivation Rules

The manifest stores operator input and derived values separately:

```yaml
version: verself.bootstrap.v1
site: prod
inputs:
  product_domain: verself.sh
  company_domain: guardianintelligence.org
  company_name: Guardian Intelligence
  owner_alias: shovon
  owner_name: Shovon Hasan
  platform_repo_slug: verself
derived:
  owner_email: shovon@guardianintelligence.org
  organization_name: guardianintelligence.org
  platform_company_slug: guardian-intelligence
  platform_company_display_name: Guardian Intelligence
  zitadel_domain: auth.verself.sh
  iam_service_domain: iam.api.verself.sh
  sandbox_rental_service_domain: sandbox.api.verself.sh
  forgejo_domain: git.verself.sh
```

Derived locally:

- email addresses from alias and company domain;
- public origins from the product domain and service subdomain catalog;
- slugs from explicit values or normalized display names;
- DNS desired state from the site record catalog;
- stable product UUIDs for repo-owned project/repository/backend records;
- OIDC redirect/logout URLs from service origins.

Resolved from providers:

- Cloudflare zone IDs;
- Latitude.sh server IDs and public IP addresses;
- Zitadel organization, project, application, and authorization IDs;
- Forgejo repository numeric IDs;
- secret values and key material.

Generated:

- random passwords, master keys, API secrets, and webhook secrets;
- Age recipients when the operator has not provided one;
- one-use bootstrap claim identifiers;
- idempotency keys for mutating service calls.

## Hosted Bootstrap Service

A hosted bootstrap service can expose the same manifest contract:

```text
verself bootstrap remote plan --from manifest.yaml
verself bootstrap remote validate --from manifest.yaml
verself bootstrap remote bundle --from manifest.yaml --age-recipient age1...
```

The first supported hosted mode should validate inputs, render a signed plan,
and return an encrypted bootstrap bundle. The local CLI performs provider calls
with local credentials. A later delegated mode may accept scoped provider OAuth
grants, but the service must treat those grants as one-use bootstrap material
and redact them from logs, traces, and persistent state.

## Service-Side Owner Claim

The platform owner flow belongs to `iam-service`.

`bootstrap claim prepare` creates a pending claim:

```json
{
  "site": "prod",
  "org_name": "guardianintelligence.org",
  "owner_email": "shovon@guardianintelligence.org",
  "trust_tier": "platform"
}
```

During browser or CLI auth callback, IAM verifies:

- the OIDC token issuer and audience;
- the nonce and PKCE state;
- `email_verified == true`;
- the normalized email matches a pending claim;
- the claim is active, unexpired, and unclaimed.

IAM then:

- materializes the product organization profile;
- records the trust tier;
- grants owner authorization in Zitadel for required product projects;
- reconciles authorization graph state;
- records audit and domain-event evidence;
- forces fresh token minting so the caller receives role assignments in the
  next access token.

The normal member-management API remains a standard owner/admin/member workflow.
The platform bootstrap owner claim is a separate command with narrower input and
stronger preconditions.

## Security Controls

- Profile config stores no bearer tokens, refresh tokens, provider tokens, or
  admin credentials.
- Provider tokens enter bootstrap through stdin, environment variables, OS
  credential stores, or encrypted SOPS bags.
- The Zitadel admin PAT is consumed by `iam-service` and component reconcilers;
  routine CLI commands call service APIs.
- Mutating commands set idempotency keys through SDK middleware.
- Every service request carries trace context; service-side audit and ClickHouse
  evidence are the durable completion record.
- Interactive commands refuse ambiguous org/project selection.
- Non-interactive commands fail when required profile, org, or project context
  is missing.
- Token files require owner-only permissions and are opened as regular files.
- CLI logs and errors redact tokens, authorization headers, cookies, signed
  URLs, and webhook secrets.
- Runtime files are created under a `0700` directory and removed after the
  command exits.

## Testing

CLI implementation should have deterministic tests for:

- XDG path resolution and directory permissions;
- profile precedence and ambiguity errors;
- credential-store references without secret leakage into config files;
- project-local `.verself/project.json` resolution;
- bootstrap manifest derivation from product domain, company domain, company
  name, owner alias, and owner name;
- Cloudflare and Latitude provider resolution using fake API servers;
- generated-client request shapes for SDK-backed commands;
- idempotency key generation and retry behavior for mutating commands;
- JSON output stability for automation.

Live e2e coverage should exercise:

- `verself bootstrap init --dry-run --json`;
- repo-local bootstrap through DNS/provision/host/deploy phases on a disposable
  site;
- `bootstrap claim prepare` followed by `auth login` for a verified owner email;
- a successful org switch and IAM owner-permission check;
- service-side audit and domain-event rows in ClickHouse for the bootstrap
  claim and owner grant.

Completion evidence for bootstrap work is the combination of CLI JSON output,
service audit events, domain-update ledger rows, and traces linked by the same
trace ID.

## Implementation Boundaries

Suggested source layout:

```text
src/sdks/go/verself/             curated public Go SDK
src/cli/verself/                 public CLI binary
src/tools/operator/              legacy/internal operator helpers during cutover
src/services/iam-service/        owner-claim and organization APIs
src/host/sites/<site>/           pre-service site variable store
```

The current `aspect operator platform --action=seed` behavior can become a
migration reference for service APIs. The public `verself` CLI should converge
durable product state through SDK calls once the relevant service exists.
