# rent-a-sandbox Frontend

Customer-facing UI for the sandbox rental product. TanStack Start (SSR) + React Query + Electric SQL real-time sync.

Lives at `src/viteplus-monorepo/apps/rent-a-sandbox/` within a pnpm workspace managed by Vite+. Shared UI primitives (`cn`, `Skeleton`) come from `@forge-metal/ui` (`packages/ui/`).

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
