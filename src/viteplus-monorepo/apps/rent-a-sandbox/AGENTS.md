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
| `useEffect` to trigger login/logout/callback auth flows | Route-level `beforeLoad` plus `@forge-metal/auth-web` server helpers |
| `useEffect` to invalidate queries when external data changes | `onSuccess` / `onSettled` on the `useMutation` that caused the change |
| `useEffect` for DOM interactions (scroll, focus, resize) | `use-stick-to-bottom` for scroll-follow; for other DOM cases, evaluate whether a library exists before writing a `useEffect` |

The one exception: `useEffect` is acceptable for DOM manipulation that has no library equivalent (and you've checked). Even then, prefer a community hook (e.g. from `usehooks-ts` or similar) over a hand-rolled `useEffect`.

### Auth + Query Cache

Auth state is server-owned (`@forge-metal/auth-web` + HTTP-only session cookie + `frontend_auth_sessions`). `/login`, `/callback`, and `/logout` are route-level `beforeLoad` flows that run on the server and during client navigations. Do not mirror auth state into React Query or persist bearer tokens in the browser. Treat auth as route context and server function context.

### SSR + Loading States

- Never treat `undefined` (query still loading) the same as `[]` (loaded but empty). Use `isPending` from `useQuery` to show `<Skeleton>` placeholders during loading. Show empty-state messages only after the query resolves.
- `<Skeleton>` is exported from `@forge-metal/ui` (shadcn pattern: `animate-pulse rounded-md bg-primary/10`).
- Gate protected routes with `beforeLoad` / `requireViewer`, not `useHydrated()` or `<ClientOnly>`.
- Reserve `<ClientOnly>` for browser-only leaf widgets, not auth or route protection.

### Query Keys

All query keys are centralized in `~/lib/query-keys.ts`. Always use `keys.*` — never inline key arrays.
