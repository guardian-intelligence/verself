# verself-web Frontend

Single TanStack Start (SSR) + React Query + Electric SQL app served at the
verself.sh apex. Hosts the signed-in console (`_shell/_authenticated/*`),
auth entry routes (`/login`, `/logout`), public docs (`/docs`), public
policy (`/policy`), and the same-origin OTLP forwarder (`/api/otel/v1/traces`).
Public routes nest under the `_workshop` pathless layout, which carries the
ink-on-argent workshop chrome from `@verself/brand`; the console keeps its
own sidebar shell until the planned workshop migration of the console.

Read @src/viteplus-monorepo/packages/ui/src/components/ui/page.tsx for visual hierarchy rules.

Lives at `src/viteplus-monorepo/apps/verself-web/` within a pnpm workspace managed by Vite+. Shared UI primitives (`cn`, `Skeleton`) come from canonical `@verself/ui` subpath exports (`packages/ui/`).

## React Patterns

### No useEffect

Do not introduce useEffect. Every common `useEffect` pattern has a proper TanStack or other library replacement:

| Anti-pattern                                                                      | Correct replacement                                                                                                          |
| --------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------- |
| `useEffect` to fetch data                                                         | `useQuery` from `@tanstack/react-query`                                                                                      |
| `useEffect` + `useState(mounted)` for SSR hydration guard                         | `useHydrated()` or `<ClientOnly>` from `@tanstack/react-router`                                                              |
| `useEffect` to run side effects on navigation (e.g. Stripe redirect invalidation) | `beforeLoad` on the route definition — it runs once per navigation, not per render                                           |
| `useEffect` to trigger login/logout auth flows                                    | Route-level `beforeLoad` plus iam-service `/api/v1/auth/*` navigation                                                        |
| `useEffect` to invalidate queries when external data changes                      | `onSuccess` / `onSettled` on the `useMutation` that caused the change                                                        |
| `useEffect` for DOM interactions (scroll, focus, resize)                          | `use-stick-to-bottom` for scroll-follow; for other DOM cases, evaluate whether a library exists before writing a `useEffect` |

The one exception: `useEffect` is acceptable for DOM manipulation that has no library equivalent (and you've checked). Even then, prefer a community hook (e.g. from `usehooks-ts` or similar) over a hand-rolled `useEffect`.

### Auth + Query Cache

Auth state is owned by iam-service through same-origin `/api/v1/auth/*` endpoints and an HTTP-only session cookie. `/login` is a thin TanStack UI that starts the iam-service login endpoint, and `/logout` redirects to the iam-service logout endpoint. Do not mirror auth state into React Query or persist bearer tokens in the browser.

`src/routes/__root.tsx` calls `getClientAuthSnapshot()` once per navigation, seeds `AuthProvider`, and syncs the React Query cache through `syncAuthPartitionedCache(...)` using the iam-service-issued auth cache partition. Component code should read `useAuth()`, `useSignedInAuth()`, `useUser()`, or `useSession()` from `@verself/auth-web/react`; it should not call iam-service auth endpoints directly except through the auth provider navigation hooks.

### Routing + Auth

- Public routes stay at the root of `src/routes/`.
- Protected screens live under `src/routes/_authenticated/`.
- Only `src/routes/_authenticated/route.tsx` should call `requireAuth(...)`. Child routes should not repeat auth gating; read the already-authenticated snapshot through `useSignedInAuth()` when query keys or mutations need the cache partition.
- Router-owned transport states come from app-wide boundaries in `src/router.tsx` (`defaultPendingComponent`, `defaultErrorComponent`, `defaultNotFoundComponent`), not per-page `if (!data)` fallbacks.
- Reserve `<ClientOnly>` for browser-only leaf widgets such as Electric-powered tables and logs, not auth or route protection.

### SSR + Loading States

- Never treat `undefined` (query still loading) the same as `[]` (loaded but empty). Use `isPending` from `useQuery` to show `<Skeleton>` placeholders during loading. Show empty-state messages only after the query resolves.
- `<Skeleton>` is exported from `@verself/ui` (shadcn pattern: `animate-pulse rounded-md bg-primary/10`).
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
  the source label is on the left as plain text (`Free tier`, `Contract`,
  `Account balance`, …) and the debit amount is on the right with the minus
  sign adjacent to the `$` sign (`− $0.10`). Everything numeric — the
  quantity, the `@ rate` inline, the charge, the drain amounts, and the
  Amount Owed total — renders in the monospace family with `tabular-nums`
  so digits align across rows. Source labels and SKU names stay in the
  default sans font. Do not bold numeric cells; bold introduces per-digit
  width jitter even under `tabular-nums`.
- Every entitlement class that is active for the SKU in the current cycle
  renders as a subtraction row — including classes the funder has not yet
  touched (drain = 0 but remaining > 0). A class is hidden only when both
  drain and remaining are zero.
- There is no per-line receivable roll-up. The only aggregation is the
  `Amount Owed` row at the bottom of the table, drawn as a single full-width
  `<td colSpan={2}>` with one thick separator above it, flex
  `justify-between` layout (label on the left, amount on the right), and
  bold same-size text on both sides.

Do not reintroduce per-line roll-ups, per-bucket rollup tables,
gross/credit/due metric cards, or any other secondary aggregation surface
here. Bucket-level analytics belong in Grafana, not the customer UI.

## agent-browser login runbook (console QA / design review)

The signed-in console (`_shell/_authenticated/*`, including the flight widget)
uses the Verself auth shell backed by Zitadel. Use the dedicated QA human for
automation.

**QA account (verified working, 2026-05-16):** `qa-flight@verself.sh`, a
password-only Zitadel human associated with the `guardian-intelligence` org
slug, credential in the agent-browser vault profile `verself-qa`. It is
authorized by exactly **one** SpiceDB tuple — membership in the org's `admin`
role — which is the minimum that satisfies iam-service `AccessibleOrganizations`
(`org` permission `read = owner + admin + member`). Do not clone ceo's project
grants; they are root-equivalent and unnecessary.

**Login flow** (the agent-browser env here forces an 800×600 ozone viewport
and the daemon's active page can flip between separate CLI calls — run the
whole flow in **one uninterrupted script**, and resolve refs dynamically and
element-type-aware, e.g. `snapshot -i | grep -E '(button|textbox) "Login
Name"'`, never a bare label grep which also matches the `heading "Password"`):

1. `agent-browser close` (env needs `--args "--no-sandbox"`).
2. `open https://verself.sh/login`.
3. Fill **"Email"** textbox = `qa-flight@verself.sh`.
4. Fill the **"Password"** textbox and submit the **"Sign in"** button.
5. It redirects to `https://verself.sh/<org>`. Append `?flight=…` and
   `screenshot`.

**Re-provisioning a QA human** (sanctioned path; secrets stay on the host /
in the vault, never in tracked files or echoed commands):

- Zitadel admin token: `sudo -n cat /etc/credstore/iam-service/zitadel-admin-token`
  over operator SSH (`ssh ubuntu@prod@access.verself.sh`), fed into a mode-600
  curl config (`curl -K`), not argv. API base `https://verself.sh`,
  `Authorization: Bearer …`.
- Create: `POST /v2/users/human` `{username,organization:{orgId},profile,
email:{isVerified:true},password:{changeRequired:false}}` in the platform
  org (orgId resolved from `POST /v2/users` email search of an existing
  member). Returns `userId`.
- Authorize (the actual console gate is SpiceDB, not Zitadel grants): on the
  host, `zed --endpoint 127.0.0.1:24009 --token "$(sudo -n cat
/etc/credstore/iam-service/spicedb-grpc-preshared-key)" --insecure
relationship create role:org_org_<ORG>_role_admin member
user:b64_<base64(zitadelUserId)>`. Discover `<ORG>` / subject form from
  `zed relationship read org` + `… read role` (subject is
  `user:b64_<base64 of the decimal Zitadel user id>`).
- Vault it: `printf %s '<pw>' | agent-browser auth save verself-qa
--url https://verself.sh/login --username qa-flight@verself.sh
--password-stdin`.

### Flight widget QA states (no backend needed)

The console index renders synthetic states from `?flight=`. Until the flight
data model is real, the default console state is also a fixture. Useful once
logged in:

- `?flight=building-on-time | running-late | boarding | no-build`
- `?flight=debug&actor=ash&src=PR47&dst=MAIN&state=ontime&remaining=12&commits=4`
  — `state` ∈ `ontime|late|cold`; vary `actor/src/dst/remaining/commits`
  (`commits=none` hides the pill). Seeds one card entirely from query params.

## ShadCN/ui

Use the /shadcn skill (if you're Claude) when working in this repo. All components are installed. Blocks are not installed.

## Base UI

This repo uses Shadcn with Base UI, which is extremely new. Please look up docs at https://base-ui.com/react/handbook/styling
