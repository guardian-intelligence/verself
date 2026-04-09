<!--VITE PLUS START-->

# Using Vite+, the Unified Toolchain for the Web

This project is using Vite+, a unified toolchain built on top of Vite, Rolldown, Vitest, tsdown, Oxlint, Oxfmt, and Vite Task. Vite+ wraps runtime management, package management, and frontend tooling in a single global CLI called `vp`. Vite+ is distinct from Vite, but it invokes Vite through `vp dev` and `vp build`.

## Vite+ Workflow

`vp` is a global binary that handles the full development lifecycle. Run `vp help` to print a list of commands and `vp <command> --help` for information about a specific command.

### Build

- `vp build` - Build for production
- `vp pack` - Build libraries
- `vp preview` - Preview production build

### Manage Dependencies

Vite+ automatically detects and wraps the underlying package manager such as pnpm, npm, or Yarn through the `packageManager` field in `package.json` or package manager-specific lockfiles.

- `vp add` - Add packages to dependencies
- `vp remove` (`rm`, `un`, `uninstall`) - Remove packages from dependencies
- `vp update` (`up`) - Update packages to latest versions
- `vp dedupe` - Deduplicate dependencies
- `vp outdated` - Check for outdated packages
- `vp list` (`ls`) - List installed packages
- `vp why` (`explain`) - Show why a package is installed
- `vp info` (`view`, `show`) - View package information from the registry
- `vp link` (`ln`) / unlink - Manage local package links
- `vp pm` - Forward a command to the package manager

### Maintain

- `upgrade` - Update `vp` itself to the latest version

These commands map to their corresponding tools. For example, `vp dev --port 3000` runs Vite's dev server and works the same as Vite. `vp test` runs JavaScript tests through the bundled Vitest. The version of all tools can be checked using `vp --version`. This is useful when researching documentation, features, and bugs.

## Common Pitfalls

- **Using the package manager directly:** Do not use pnpm, npm, or Yarn directly. Vite+ can handle all package manager operations.
- **Always use Vite commands to run tools:** Don't attempt to run `vp vitest` or `vp oxlint`. They do not exist. Use `vp test` and `vp lint` instead.
- **Running scripts:** Vite+ built-in commands (`vp dev`, `vp build`, `vp test`, etc.) always run the Vite+ built-in tool, not any `package.json` script of the same name. To run a custom script that shares a name with a built-in command, use `vp run <script>`. For example, if you have a custom `dev` script that runs multiple services concurrently, run it with `vp run dev`, not `vp dev` (which always starts Vite's dev server).
- **Do not install Vitest, Oxlint, Oxfmt, or tsdown directly:** Vite+ wraps these tools. They must not be installed directly. You cannot upgrade these tools by installing their latest versions. Always use Vite+ commands.
- **Use Vite+ wrappers for one-off binaries:** Use `vp dlx` instead of package-manager-specific `dlx`/`npx` commands.
- **Import JavaScript modules from `vite-plus`:** Instead of importing from `vite` or `vitest`, all modules should be imported from the project's `vite-plus` dependency. For example, `import { defineConfig } from 'vite-plus';` or `import { expect, test, vi } from 'vite-plus/test';`. You must not install `vitest` to import test utilities.
- **Type-Aware Linting:** There is no need to install `oxlint-tsgolint`, `vp lint --type-aware` works out of the box.

## Review Checklist for Agents

- [ ] Run `vp install` after pulling remote changes and before getting started.
- [ ] Run `vp check` and `vp test` to validate changes.
<!--VITE PLUS END-->

## Local Frontend Development

Frontend apps (TanStack Start) run locally via `vp dev` with HMR. They talk to remote services over SSH tunnels. Auth goes through real Zitadel (HTTPS, external).

### Zitadel OIDC Architecture

Only frontends need OIDC apps. Go backend services (mailbox-service, billing-service, sandbox-rental-service) validate JWTs that frontends already obtained — they don't have their own OIDC apps. A backend only needs the Zitadel **project ID** (as the `audience` claim to validate against).

| Zitadel Project | OIDC Apps (frontends) | JWT Validators (backends) |
|---|---|---|
| `sandbox-rental` | rent-a-sandbox | sandbox-rental-service, billing-service |
| `mailbox-service` | webmail | mailbox-service |

### Dev Mode OIDC Apps

Each frontend needs **two Zitadel OIDC applications**: one for production and one for local development. Zitadel's `devMode` toggle controls redirect URI enforcement:

- **`devMode: false`** (production): HTTPS-only redirect URIs, exact match
- **`devMode: true`** (development): HTTP allowed, glob patterns in redirect URIs (e.g., `http://localhost:*/callback`)

Production OIDC apps are created automatically by each app's Ansible role (`zitadel_app.yml`). Dev OIDC apps are created once manually or via `seed-demo.yml`.

For each frontend, create a dev OIDC app in the same Zitadel project as the production app. Use the Zitadel console at `https://auth.<domain>` or the Management API:

| Frontend | Zitadel Project | Port | Dev Redirect URI |
|---|---|---|---|
| rent-a-sandbox | sandbox-rental | 4244 | `http://127.0.0.1:4244/callback` |
| webmail | mailbox-service | 4245 | `http://127.0.0.1:4245/callback` |

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
ssh -L 4246:127.0.0.1:4246 \
    -L 3011:127.0.0.1:3011 \
    -L 3010:127.0.0.1:3010 \
    -L 4243:127.0.0.1:4243 \
    fm-dev-w0 -N &

# 2. Start the app with dev credentials
cd apps/mail   # or apps/rent-a-sandbox
AUTH_ISSUER_URL=https://auth.<domain> \
AUTH_CLIENT_ID=<dev-oidc-client-id> \
ELECTRIC_URL=http://127.0.0.1:3011 \
vp dev
```

Open `http://127.0.0.1:<port>`. Vite HMR gives sub-second feedback on every file save. API calls and Electric shapes go through the SSH tunnels to real services.

### External Data Sources

Electric SQL delivers real-time data via `useLiveQuery`. This is not a React Query data source — it's a separate reactive primitive. Do not bridge Electric into React Query with `useEffect`. They coexist: React Query for request/response API calls, Electric for live-streamed PG replication.

## UI Components

- `cn()` and `Skeleton` are in the shared `@forge-metal/ui` package (`packages/ui/`). Import as `import { cn, Skeleton } from "@forge-metal/ui"`.
- App-specific components live in `src/components/` (e.g. `balance-card.tsx`).
- shadcn-compatible theme tokens (OKLCH) are in `src/styles/app.css` via Tailwind v4's `@theme` directive.

## Routing

TanStack Router file-based routing. Route files go in `src/routes/`. The route tree is auto-generated — run `vp dlx @tanstack/router-cli generate` after adding or removing route files.

`beforeLoad` has access to `context.queryClient` for route-level side effects (invalidation, prefetching). Prefer this over component-level `useEffect` for navigation-triggered logic.

# TanStack DB (client-side reactive database)

- task: "TanStack DB core concepts, createCollection, live queries, optimistic mutations"
  load: "node_modules/@tanstack/db/skills/db-core/SKILL.md"
- task: "setting up collections with createCollection, adapter selection, schemas, sync modes"
  load: "node_modules/@tanstack/db/skills/db-core/collection-setup/SKILL.md"
- task: "TanStack DB query builder, where, join, select, groupBy, orderBy, aggregates, operators"
  load: "node_modules/@tanstack/db/skills/db-core/live-queries/SKILL.md"
- task: "TanStack DB mutations, optimistic updates, createOptimisticAction, transactions"
  load: "node_modules/@tanstack/db/skills/db-core/mutations-optimistic/SKILL.md"
- task: "building custom TanStack DB sync adapters, SyncConfig, ChangeMessage format"
  load: "node_modules/@tanstack/db/skills/db-core/custom-adapter/SKILL.md"
- task: "integrating TanStack DB with meta-frameworks, SSR disabled routes, collection preloading"
  load: "node_modules/@tanstack/db/skills/meta-framework/SKILL.md"
- task: "React hooks for TanStack DB: useLiveQuery, useLiveSuspenseQuery, useLiveInfiniteQuery"
  load: "node_modules/@tanstack/react-db/skills/react-db/SKILL.md"

# TanStack Query (data fetching & caching)

- task: "data fetching with TanStack Query, useQuery, useMutation, caching, invalidation, SSR"
  load: ".claude/skills/tanstack-react-query.md"

# Nitro (server runtime)

- task: "configuring Nitro server runtime, deployment, server middleware"
load: "apps/web/node_modules/nitro/skills/nitro/SKILL.md"
<!-- intent-skills:end -->


### ElectricSQL gotchas

Multiple Electric instances on the same PostgreSQL cluster (e.g., one for `sandbox_rental`, one for `mailbox_service`) require three differentiators to avoid collisions:

1. **`ELECTRIC_REPLICATION_STREAM_ID`** — controls the replication slot name suffix. Without it, both instances fight over `electric_slot_default`. Replication slots are cluster-wide, not per-database.
2. **`ELECTRIC_INSTANCE_ID`** — controls the PostgreSQL advisory lock hash. Without it, both instances use the same default advisory lock and the second instance blocks forever on `waiting_on_lock`.
3. **`RELEASE_NAME`** — Elixir/Erlang BEAM node name. Both instances run with `--network=host`, so their Erlang nodes collide on the same hostname. Without a distinct name, the second container exits with "the name electric@hostname seems to be in use by another Erlang node".

Each Electric instance also needs its own publication (`CREATE PUBLICATION ... FOR TABLE ...`) with `REPLICA IDENTITY FULL` on all synced tables. The publication name is derived from the stream ID: `electric_publication_{stream_id}` (default: `electric_publication_default`). Since publications are per-database, the default name works if instances target different databases — but setting `ELECTRIC_REPLICATION_STREAM_ID` changes the expected publication name too.

