<!--VITE PLUS START-->

# Using Vite+, the Unified Toolchain for the Web

This project is using Vite+, a unified toolchain built on top of Vite, Rolldown, Vitest, tsdown, Oxlint, Oxfmt, and Vite Task. Vite+ wraps runtime management, package management, and frontend tooling in a single global CLI called `vp`. Vite+ is distinct from Vite, but it invokes Vite through `vp dev` and `vp build`.

## Vite+ Workflow

`vp` is a global binary that handles the full development lifecycle. Run `vp help` to print a list of commands and `vp <command> --help` for information about a specific command.

### Build

- build - Build for production
- pack - Build libraries
- preview - Preview production build

### Manage Dependencies

Vite+ automatically detects and wraps the underlying package manager such as pnpm, npm, or Yarn through the `packageManager` field in `package.json` or package manager-specific lockfiles.

- add - Add packages to dependencies
- remove (`rm`, `un`, `uninstall`) - Remove packages from dependencies
- update (`up`) - Update packages to latest versions
- dedupe - Deduplicate dependencies
- outdated - Check for outdated packages
- list (`ls`) - List installed packages
- why (`explain`) - Show why a package is installed
- info (`view`, `show`) - View package information from the registry
- link (`ln`) / unlink - Manage local package links
- pm - Forward a command to the package manager

### Maintain

- upgrade - Update `vp` itself to the latest version

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