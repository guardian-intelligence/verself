# rent-a-sandbox Frontend

Customer-facing UI for the sandbox rental product. TanStack Start (SSR) + React Query + Electric SQL real-time sync.

Lives at `src/viteplus-monorepo/apps/rent-a-sandbox/` within a pnpm workspace managed by Vite+. Shared UI primitives (`cn`, `Skeleton`) come from `@forge-metal/ui` (`packages/ui/`).

## React Patterns

### No useEffect

Do not introduce useEffect. Every common `useEffect` pattern has a proper TanStack or other library replacement:

| Anti-pattern                                                                      | Correct replacement                                                                                                          |
| --------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------- |
| `useEffect` to fetch data                                                         | `useQuery` from `@tanstack/react-query`                                                                                      |
| `useEffect` + `useState(mounted)` for SSR hydration guard                         | `useHydrated()` or `<ClientOnly>` from `@tanstack/react-router`                                                              |
| `useEffect` to run side effects on navigation (e.g. Stripe redirect invalidation) | `beforeLoad` on the route definition — it runs once per navigation, not per render                                           |
| `useEffect` to trigger login/logout/callback auth flows                           | Route-level `beforeLoad` plus `@forge-metal/auth-web/server` helpers                                                         |
| `useEffect` to invalidate queries when external data changes                      | `onSuccess` / `onSettled` on the `useMutation` that caused the change                                                        |
| `useEffect` for DOM interactions (scroll, focus, resize)                          | `use-stick-to-bottom` for scroll-follow; for other DOM cases, evaluate whether a library exists before writing a `useEffect` |

The one exception: `useEffect` is acceptable for DOM manipulation that has no library equivalent (and you've checked). Even then, prefer a community hook (e.g. from `usehooks-ts` or similar) over a hand-rolled `useEffect`.

### Auth + Query Cache

Auth state is server-owned (`@forge-metal/auth-web/server` + HTTP-only session cookie + `frontend_auth_sessions`). `/login`, `/callback`, and `/logout` are route-level `beforeLoad` flows that run on the server and during client navigations. Do not mirror auth state into React Query or persist bearer tokens in the browser.

`src/routes/__root.tsx` calls `getClientAuthSnapshot()` once per navigation, seeds `AuthProvider`, and syncs the React Query cache through `syncAuthPartitionedCache(...)` using the server-issued auth cache partition. Component code should read `useAuth()`, `useSignedInAuth()`, `useUser()`, or `useSession()` from `@forge-metal/auth-web/react`; it should not call server auth helpers directly.

### Routing + Auth

- Public routes stay at the root of `src/routes/`.
- Protected screens live under `src/routes/_authenticated/`.
- Only `src/routes/_authenticated/route.tsx` should call `requireAuth(...)`. Child routes should not repeat auth gating; read the already-authenticated snapshot through `useSignedInAuth()` when query keys or mutations need the cache partition.
- Router-owned transport states come from app-wide boundaries in `src/router.tsx` (`defaultPendingComponent`, `defaultErrorComponent`, `defaultNotFoundComponent`), not per-page `if (!data)` fallbacks.
- Reserve `<ClientOnly>` for browser-only leaf widgets such as Electric-powered tables and logs, not auth or route protection.

### SSR + Loading States

- Never treat `undefined` (query still loading) the same as `[]` (loaded but empty). Use `isPending` from `useQuery` to show `<Skeleton>` placeholders during loading. Show empty-state messages only after the query resolves.
- `<Skeleton>` is exported from `@forge-metal/ui` (shadcn pattern: `animate-pulse rounded-md bg-primary/10`).
- Use route boundaries for transport loading/error/not-found. Use `EmptyState`, `TableEmptyRow`, `Callout`, and `ErrorCallout` for ready-empty and mutation-error states.

### Query Composition

- Co-locate query factories and route loaders in `src/features/*/queries.ts`.
- Seed loader-backed queries with `queryClient.ensureQueryData(...)` or feature `load*` helpers, then read them with `useSuspenseQuery(...)` inside the route component.
- Keep mutation invalidation and navigation glue in `src/features/*/mutations.ts` when possible so route files stay declarative.
- Do not add a shared `query-keys.ts` layer. Feature-local `queryOptions(...)` factories are the source of truth for keys, stale policies, and polling.

## Billing UI Invariants

### Credit Balances single-product invariant

The billing page renders a single flat "Credit Balances" table that pools every
product's SKU rows under one header. This is correct **only while the platform
sells a single billable product** (sandbox today). The `EntitlementsPanel`
component in `src/features/billing/entitlements/index.tsx` is intentionally
written to render one section per `EntitlementsView`, not one section per
product, even though the underlying API returns a `products[]` array.

When a second billable product is added:

- Do **not** reintroduce per-product section headers — the customer reads the
  page as "where can I spend my money" and a header per product breaks that
  scan.
- Replace the flat `Credit Balances` header with a per-row product selector
  (the SKU rows keep their current shape; the product becomes a cell-level
  filter, e.g. an inline pill or a dropdown above the table).
- The Account Balance header at the top of the page stays product-agnostic and
  does not need to change.

Why this lives here: the entitlements DTO has supported multiple products
since the slot-tree refactor. We are not encoding the single-product assumption
into the DTO; we are encoding it into the UI shell with a comment that points
at this AGENTS.md entry. When you add a second product, edit this entry and
the panel together.

### Usage section receipt format

The "Usage" section is the customer-facing invoice preview. Each line is one
(plan, bucket, sku, pricing_phase, unit_rate) row, shown in bank-statement
style:

- The `SKU` cell renders `<bucket display> — <sku display>`. Do not append
  the raw `sku_id`, the `plan_id`, or the `pricing_phase` — those are
  engineer-facing identifiers and belong in logs/traces, not the invoice.
- The `Usage` cell renders the formula `quantity @ rate = charge` followed
  by per-source subtractions. Subtractions use bank-statement convention:
  the source label is on the left as plain text (`Free tier`, `Subscription`,
  `Account balance`, …) and the debit amount is on the right with the minus
  sign adjacent to the `$` sign (`− $0.10`). The formula and subtraction
  rows are the same font size and monospace family — no nested font
  hierarchy.
- There is no per-line receivable roll-up. The only aggregation is the
  `Grand Total` row at the bottom of the table, drawn as a single full-width
  `<td colSpan={2}>` with one thick separator above it, flex
  `justify-between` layout (label on the left, amount on the right), and
  bold same-size text on both sides.

Do not reintroduce per-line roll-ups, per-bucket rollup tables,
gross/credit/due metric cards, or any other secondary aggregation surface
here. Bucket-level analytics belong in Grafana, not the customer UI.

## ShadCN/ui

Use the /shadcn skill when working in this repo. All components are installed. Blocks are not installed.
