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
- `vp fmt . --write ` - Format the monorepo with Oxfmt

### Maintain

- `upgrade` - Update `vp` itself to the latest version

These commands map to their corresponding tools. For example, `vp dev --port 3000` runs Vite's dev server and works the same as Vite. `vp test` runs JavaScript tests through the bundled Vitest. The version of all tools can be checked using `vp --version`. This is useful when researching documentation, features, and bugs.

## Common Pitfalls

- **Not using shadcn/ui components:** They have accessibility, default cohesive styling, extensibility, and cross-browser compatibility fixes baked in. Use them over regular DOM elements where possible.
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

## Quickstart After Pull

Run the frontend setup from a clean checkout before trusting TypeScript output:

```bash
# repo root
git pull --ff-only
aspect dev install

# Vite+ workspace
cd src/viteplus-monorepo
vp install

# repo root again
cd ../..
bazelisk run //src/viteplus-monorepo/apps/company:dev_update
bazelisk run //src/viteplus-monorepo/apps/verself-web:dev_update
```

The generated files under `apps/*/src/__generated` and
`apps/*/src/routeTree.gen.ts` are ignored source projections.
`company:dev_update` materializes the company route tree at TanStack's default
path plus the generated First Light shader TypeScript module. `verself-web:dev_update`
materializes its route tree plus OpenAPI clients and copied specs from service-owned
Bazel targets. Run the verself-web generator even when working on `apps/company`
if `vp check` reports missing generated clients; workspace checks type both apps.

On a local laptop without the hosted Verdaccio mirror, temporarily set
`src/viteplus-monorepo/.npmrc` to:

```ini
registry=https://registry.npmjs.org/
```

Restore the registry to `http://127.0.0.1:4873/` before committing unless the
registry change is the intended patch.

## Local Frontend Development

Frontend apps (TanStack Start) run locally via `vp dev` with HMR. They talk to remote services over SSH tunnels. Auth goes through real Zitadel (HTTPS, external).

Avoid `as` assertions. Prefer `satisfies`.

Functional core, imperative shell. Parse at the boundaries once. Know the shape of the data you're working with rather than doing imperative null checks.

Avoid useState -- sync small bits of imperative state to search params. For truly bespoke state management, use useReducer.

### Zitadel OIDC Architecture

Only iam-service owns interactive browser OIDC apps. Frontends start the
iam-service browser auth flow and consume its HTTP-only session snapshot.
Other Go backend services validate JWTs that iam-service exchanged for
their audience. A backend only needs the Zitadel **project ID** as the `audience`
claim to validate against.

### Supply Chain Security

We start from clean build root
-> vp install --frozen-lockfile from hosted Verdaccio
-> pnpm/store integrity checks
-> cdxgen source SBOM from pnpm-lock.yaml
-> vp check / test / typecheck / build
-> Syft scan of final .output / release tar
-> Grype scan of SBOMs
-> in-toto/SLSA-style attestation
-> publish artifact + evidence to zot
-> ClickHouse records digests and admission decision

### Running a frontend locally

```bash
# run verself-web locally against the deployed services
aspect dev verself-web

# print the resolved env without starting HMR
aspect dev verself-web --print-env
```

`aspect dev verself-web` opens the required SSH tunnels, reads the rendered
Nomad job env for production facts, and exports the current runtime env for the
local server:

- `VERSELF_DOMAIN`
- `IAM_SERVICE_BASE_URL`
- `SANDBOX_RENTAL_SERVICE_BASE_URL`
- service auth audiences for iam-service token exchange

Open the `app:` URL printed by `aspect dev verself-web`. The launcher prefers
`http://127.0.0.1:4244` but will move to a higher local port if that one is
busy, then records the chosen URL in `/tmp/verself-web-dev.env`. Vite HMR gives
sub-second feedback on every file save. API calls, Electric shapes, and OTLP
traces all flow through the SSH tunnels to the deployed single-node stack.
Interactive browser login is owned by iam-service and the public apex
route; the frontend does not create local OIDC apps or local auth-session
databases.

Remote frontend deploys go through `aspect deploy`; Nomad supervises the
Bazel-built node-app artifacts.

### External Data Sources

Electric SQL delivers real-time data via `useLiveQuery`. This is not a React Query data source — it's a separate reactive primitive. Do not bridge Electric into React Query with `useEffect`. They coexist: React Query for request/response API calls, Electric for live-streamed PG replication.

## UI Components

- `cn()` and `Skeleton` are in the shared `@verself/ui` package (`packages/ui/`). Import from canonical subpaths: `@verself/ui/lib/utils` and `@verself/ui/components/ui/skeleton`.
- App-specific components live in `src/components/` (e.g. `error-callout.tsx`). Cross-feature panels live under `src/features/<feature>/` (e.g. `features/billing/entitlements/`).
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

**Frontend SSR footgun:** browser-visible time formatting is hydration-sensitive. `toLocaleString()` / `toLocaleDateString()` / `toLocaleTimeString()` without an explicit timezone will drift between server and browser and can cause React to throw away SSR output during hydration. Do not introduce app-local date formatting helpers for SSR-visible timestamps.

**Shared frontend time abstraction:** use `formatUTCDateTime()` from `src/viteplus-monorepo/packages/web-env/src/time.ts` for SSR-visible timestamps in the web apps. It centralizes `Intl.DateTimeFormat` with `timeZone: "UTC"` and caches formatters so the rendered text agrees on the server and client.

### UI primitives

Use the shadcn/ui components from `src/viteplus-monorepo/packages/ui/src/components/ui`. They are shadcn-shaped exports wrapping Base UI (`@base-ui/react`) primitives, not Radix. Commit 4d7567b cut the whole stack over; Radix, cmdk, and vaul are gone.

#### Base UI gotchas

- **Group parts must live inside `Menu.Group`.** `DropdownMenuLabel` is a `Menu.GroupLabel`, `DropdownMenuRadioGroup` is a `Menu.RadioGroup`, etc. — they throw synchronously if rendered without the surrounding Group/GroupRoot context.
- **Decoding minified production errors.** The `https://base-ui.com/production-error?code=N` URL is a template placeholder — it will not tell you what `N` means. Instead, grep the installed package: `grep -rn "formatErrorMessage(N" node_modules/.pnpm/@base-ui+react*/`. Every throw site pairs the minified code with a readable `NODE_ENV !== "production"` branch right next to it; that branch is the real message.
- Use `<DropdownMenuContent side="top" align="end" sideOffset={8}>` and let Base UI's `Menu.Positioner` handle flipping, collision, and anchor offset.
- **`render` prop composition.** Base UI primitives (Menu.Trigger, Dialog.Trigger, Sidebar's `SidebarMenuButton`) accept a `render` prop that is either a ReactElement (cloned, props merged) or `(props, state) => ReactElement` (function form for conditional rendering). Nesting two layers that both use `useRender` internally works — e.g. `<DropdownMenuTrigger render={<SidebarMenuButton size="lg">…</SidebarMenuButton>} />` composes the trigger's a11y/event props through `SidebarMenuButton`'s own `useRender` without fighting. Pass visual children as children of the outer primitive, not inside the `render` element.
- **Open-state data attribute is `data-popup-open`, not `data-state=open`.** Style the trigger while the menu is open with `data-popup-open:bg-sidebar-accent`. The Radix-era `data-[state=open]:…` selector no longer matches.
- **Anything wrapped in `fastComponent` crashes nitro SSR.** Confirmed broken as of `@base-ui/react@1.4.0`: `MenuRoot`, `MenuTrigger`, `TooltipRoot`, `TooltipTrigger` (so: `DropdownMenu*`, `SidebarMenuButton` with the `tooltip` prop, anything else that pulls those in). The chain is `fastComponent` → `use-sync-external-store/shim` → `require("react")`, which Rolldown bundles into a duplicate React module instance whose hook dispatcher is null at SSR time, surfacing as `TypeError: Cannot read properties of null (reading 'useSyncExternalStore')`. Tracked upstream at `vitejs/rolldown-vite#596` and `mui/base-ui#3194` — both closed, no fix shipped. Workaround: gate the offending subtree on hydration. `useHydrated()` for the conditional `tooltip` prop (spread it: `{...(hydrated ? { tooltip: label } : {})}` to satisfy `exactOptionalPropertyTypes`); `<ClientOnly fallback={<StaticTrigger />}>` around `DropdownMenu` / `Tooltip` blocks with a fallback that renders the trigger shape only. Reference: `apps/verself-web/src/features/shell/app-shell.tsx`. Safe surfaces (no `fastComponent` in the tree): `Sidebar` block body, `Dialog`, `Sheet`, `Popover`, `Avatar`, `Button`, `Separator`, `Skeleton`, `Switch`, `Tooltip` _as long as you skip the `tooltip` prop on `SidebarMenuButton`_. The hand-rolled `command-palette.tsx` and the previous "overlays are banned" comment predate this diagnosis but happen to land in the right place for the wrong reason.

#### Sidebar block patterns

The `@verself/ui/components/ui/sidebar` block is the shadcn App Shell with Base UI under it. Two patterns worth knowing:

- **Bottom-anchored groups.** To pin a `SidebarGroup` to the bottom of `SidebarContent` (evergreen non-product entries like Settings, Support, Status), give the group `className="mt-auto"`. `SidebarContent` is a `flex-col` with `flex-1`; `mt-auto` does the right thing in both expanded and icon-collapsed states.
- **`SidebarInset` is the main column.** Place `<header>` + `<main>` inside `<SidebarInset>`, not as a sibling of `<Sidebar>`. The inset variant handles border-radius, shadow, and the sidebar-collapsed margin correctly; hand-rolled flex layouts will drift.
