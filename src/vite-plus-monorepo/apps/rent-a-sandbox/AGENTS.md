# rent-a-sandbox Frontend

Customer-facing UI for the sandbox rental product. TanStack Start (SSR) + React Query + Electric SQL real-time sync.

Lives at `src/vite-plus-monorepo/apps/rent-a-sandbox/` within a pnpm workspace managed by Vite+. Shared UI primitives (`cn`, `Skeleton`) come from `@forge-metal/ui` (`packages/ui/`).

## React Patterns

### No useEffect

This codebase has zero `useEffect`. Do not introduce any. Every common `useEffect` pattern has a proper TanStack replacement:

| Anti-pattern | Correct replacement |
|---|---|
| `useEffect` to fetch data | `useQuery` from `@tanstack/react-query` |
| `useEffect` + `useState(mounted)` for SSR hydration guard | `useHydrated()` or `<ClientOnly>` from `@tanstack/react-router` |
| `useEffect` to run side effects on navigation (e.g. Stripe redirect invalidation) | `beforeLoad` on the route definition — it runs once per navigation, not per render |
| `useEffect` to sync auth state from `oidc-client-ts` | `useQuery` with `enabled: hydrated`, `staleTime: Infinity`, `refetchOnWindowFocus: true` |
| `useEffect` to invalidate queries when external data changes | `onSuccess` / `onSettled` on the `useMutation` that caused the change |
| `useEffect` for DOM interactions (scroll, focus, resize) | `use-stick-to-bottom` for scroll-follow; for other DOM cases, evaluate whether a library exists before writing a `useEffect` |

The one exception: `useEffect` is acceptable for DOM manipulation that has no library equivalent (and you've checked). Even then, prefer a community hook (e.g. from `usehooks-ts` or similar) over a hand-rolled `useEffect`.

### Auth + Query Cache

Auth state is browser-only (`oidc-client-ts` + `sessionStorage`). The OIDC callback in `src/routes/callback.tsx` writes the user directly into the React Query cache via `queryClient.setQueryData(keys.user(), user)` before navigating. This ensures the Dashboard immediately sees authenticated state without a stale cache miss. Never rely on query refetch for auth transitions — always `setQueryData`.

### SSR + Loading States

- Never treat `undefined` (query still loading) the same as `[]` (loaded but empty). Use `isPending` from `useQuery` to show `<Skeleton>` placeholders during loading. Show empty-state messages only after the query resolves.
- `<Skeleton>` is exported from `@forge-metal/ui` (shadcn pattern: `animate-pulse rounded-md bg-primary/10`).
- Gate auth-dependent UI behind `useHydrated()` or `<ClientOnly>`, not `useState(mounted)`.

### Query Keys

All query keys are centralized in `~/lib/query-keys.ts`. Always use `keys.*` — never inline key arrays.

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
