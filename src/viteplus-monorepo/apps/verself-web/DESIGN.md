# Verself Web Design

## Current Direction

Verself Web should use one shared shell for public, authenticated, and debug surfaces. The shell stays dark, quiet, and infrastructure-native; the center of the screen carries a small number of high-signal product apps instead of a dashboard grid or marketing hero.

The near-term console direction is a single iOS-like app surface that melts into the ambient shell. The focused app fills the available vertical lane on one shared rail, while live activity floats over it without reserving layout space. The content belongs to Verself: CI flight status, golden environment readiness, cache warmth, runner capacity, security posture, and showback/billing.

## Reference Analysis

Local analysis assets live outside the repo:

- Source videos: `/tmp/verself-ui-twitter-videos/videos/2060188251118542848.mp4` and `/tmp/verself-ui-twitter-videos/videos/2060476937152471040.mp4`
- Extracted frames: `/tmp/verself-ui-twitter-videos/frames/2060188251118542848/` and `/tmp/verself-ui-twitter-videos/frames/2060476937152471040/`
- Contact sheets: `/tmp/verself-ui-twitter-videos/2060188251118542848-contact.jpg` and `/tmp/verself-ui-twitter-videos/2060476937152471040-contact.jpg`

Reusable observations:

- The wrapper has generous breathing room, minimal top chrome, and one centered object.
- The active panel is a tall rounded rectangle with stable dimensions and a soft shadow.
- The active app can feel tactile without side-peeking Tinder cards; transitions should feel like iOS app surfaces dissolving into the shell.
- Bottom controls are compact, icon-led, and aligned to the shell's lower rhythm.
- Motion is atmospheric and slow: parallax, soft blob movement, or image drift. It does not compete with the text.
- Typography is sparse. There are one or two lines inside the primary panel and almost no explanatory UI copy.

## Shell

The Verself shell keeps the Guardian mark and top navigation as persistent product chrome. It should not switch to a separate signed-in app frame after authentication. Signed-in and signed-out users see the same silhouette, background, deck geometry, Navee, and mainstay controls. Authentication, organization selection, and subscription state change the contents of panels and banners inside the shell; they do not swap the shell itself.

Desktop and mobile should share the same composition:

- The console stage is capped at the iPhone 16 Pro Max logical height: Apple lists the device at `2868 x 1320` physical pixels, so the 3x logical height is `956px`. Shorter devices use `100svh`; taller desktop windows center the `956px` stage.
- Top chrome sits near the top safe area and uses labelled, icon-led controls.
- The app surface occupies the visual center, filling the vertical lane between top chrome and bottom controls.
- The entire console object clamps to the `sm` breakpoint on desktop; wide screens center the mobile composition instead of expanding into a dashboard.
- The main panel keeps a tall aspect ratio, with stable dimensions across loading, hover, and data changes.
- Cards are objects, not page sections. Do not wrap the whole page or whole shell in card styling.

All horizontal slices share one rail: `w-[calc(100%-3rem)] max-w-sm`. The top bar, live activity, card deck, and Navee dock must use this same rail. Do not tune widths independently per slice.

Primary UI element tokens:

- Primary background: `#151514`. This is the gunmetal card background sampled from the 2026-05-31 reference and should be rendered as a solid color, not a gradient.
- Primary text: `#d5d5d5`. This is the light gray text color sampled from the 2026-05-31 repository-list reference.
- Primary surface radius: `42px`. The center app surface uses this radius so its visible border rhythm aligns with the CI live activity card.

## Console Chrome

The signed-in console owns its own top and bottom chrome; the public `Code` / `Docs` nav is hidden inside the console stage.

Top bar:

- Left: `Account` button with the Guardian wings mark.
- Center: current page title. The default home/feed surface is `For You`.
- Right: `New` button with a lucide `Plug` icon.

Navee dock:

- Center: Navee button labelled `For You`; it links back to the home/feed app when the shell is docked.
- There are no persistent left/right bottom buttons. The app surface stretches into that lower horizontal slice, and only Navee overlays it.
- Navee occupies the full shell rail with the same `0.75rem` side inset as the live activity. Its radius is derived from nested-radius math against the primary app surface: `42px - 12px = 30px`.
- Navee uses the same `0.75rem` inset vertically and horizontally. Its bottom offset is the app surface bottom offset plus that inset, so short devices like iPhone SE do not push the control too far into the content.
- Navee dock height is responsive: `clamp(2.25rem, 5.7svh, 2.75rem)`. It keeps the Pro Max shape at normal heights while shrinking on compact screens.

Every command button must have a visible word label and an accessible label. Icons are SVGs; prefer lucide icons unless the button explicitly needs the Guardian brand mark.

Controls should not use visible outline borders. Use translucent fills, blur, and soft shadows; focus rings remain visible for accessibility.

## Live Activity Layer

The CI flight card is the shell's live activity, not a second center app widget and not a layout input. It is absolutely overlaid above the app surface, can overlap the focused app, and later owns the downward shadow/compositing pass.

Rules:

- Keep the original CI flight board as the only flight-tracking object.
- The center `ci-flight` app may show contextual insight, but it must not duplicate the live activity's source-to-destination tracker.
- The live activity shares the same `sm` max-width shell clamp as the deck and bottom controls.
- The live activity is inset from the app surface by `0.75rem` on the top, left, and right so the spacing reads as one frame.
- The app surface must not reserve top space for the live activity; only content padding may account for overlap when needed.
- Debug fixtures keep flowing through `flight`, `src`, `dst`, `state`, `remaining`, and `commits` URL params until real data sources replace them.

## Authentication And Entitlements

Authentication is a capability of the current session, not a reason to render a different product.

Each app declares an access policy:

- `public`: useful while signed out and signed in.
- `signed_in`: renders a signed-out in-content banner or panel when anonymous.
- `organization`: renders organization selection or onboarding in-content when no active organization is available.
- `entitled`: renders an upgrade or billing banner when the user lacks the required plan, SKU, or quota.
- `ceremony`: redirects only when the next step must leave the shell, such as login, account selection, checkout, OAuth provider authorization, or device authorization.

The default failure mode is an in-content gate. Redirects are exceptions for ceremonies, not the generic way to explain requirements. When redirecting, preserve the intended app, panel, focus item, and banner state in the return URL so the user lands back in the same shell context.

Examples:

- Anonymous user opens `/shovon?app=ci-flight`: show the CI flight panel shape with a signed-out banner explaining that live organization data requires sign-in.
- Signed-in user without an active org opens the same URL: keep the shell, render an organization picker/onboarding panel.
- Signed-in user opens a premium runner insight without the needed billing tier: keep the panel and show an upgrade banner with the exact missing capability.
- User clicks `Sign in`, `Select account`, or `Upgrade`: perform the redirect ceremony and return to the same URL state.

This is also the product invariant: never throw a user to a redirect screen without first making the authentication or billing requirement visible in the product surface.

## App Surface

The center content is an app surface, not a stack of Tinder cards and not route pages embedded inside a dashboard. Each app owns one focused operational surface; navigation can later happen through gestures, command controls, or URL state without showing peeking neighbor cards.

Each app should answer one operational question quickly:

- CI flight: What is moving from source to destination, and is it on time?
- Golden environment: Is the durable snapshot ready to run from?
- Cache health: Are build artifacts warm enough to make the next run fast?
- Runner capacity: Is the bare-metal cell calm, busy, or blocked?
- Security posture: Is there any authentication, secret, or policy action required?
- Showback: What did this activity cost or save in the current billing window?

Panel rules:

- One primary metric or status per panel.
- At most two supporting labels inside the focused panel.
- Operational identifiers may appear, but only when they clarify action: `ASH`, `PR47`, `MAIN`, `4 commits`.
- The default app surface is a single solid gunmetal gray object using the primary background token, with no gradient treatment.
- First-run repo onboarding is minimal: a large plus icon and `Add a repo to get started`.
- Interaction can be swipe, keyboard, or small icon buttons. Avoid visible instructional text.

## App Architecture

Apps are in-repo modules registered with a shared shell. They are not microfrontends: one router, one dependency graph, one auth model, one query client, one design system, one telemetry pipeline, and one release artifact. The shell controls navigation, providers, mainstay controls, app lifecycle, and live-data subscriptions; each app controls its own view model, focused panel, detail surface, and optional scene.

An app definition should be a typed object with:

- `id`: stable URL and telemetry identity.
- `label`: short display label for accessibility and switcher surfaces.
- `icon`: lucide or brand icon used by shell controls.
- `route`: canonical route/search state owned by TanStack Router.
- `access`: public/auth/org/entitlement requirements and in-content gate copy.
- `queries`: route-loader-backed TanStack Query options for initial data.
- `collections`: optional TanStack DB/Electric collections for live projections.
- `scene`: optional scene descriptor, never an ad-hoc canvas mount.
- `notifications`: mapping from durable notification kinds to app badge state.
- `errorPolicy`: how the app recovers from data, auth, and scene failures.

The first app is `ci-flight`. Likely next apps are `golden-environments`, `cache-health`, `runner-capacity`, and `account-security`.

## Data Spine

The app needs several data classes, all owned by the shell/runtime rather than invented per widget:

- Auth/session: `_shell` fetches the auth snapshot, seeds the auth-partitioned query cache, and remounts authenticated providers on partition change.
- Product reads: TanStack Start server functions wrap generated SDK calls and typed service clients. App modules expose feature-local `queryOptions(...)` factories; route loaders call `ensureQueryData(...)`.
- Product writes: mutations call typed server functions, then invalidate or rebase the affected app queries and collections.
- Live projections: TanStack DB/Electric collections carry account/session and product projection changes that may be updated by agents, webhooks, or other devices.
- Durable notifications: notifications-service is the source of truth for user-visible out-of-band changes. Toasts are presentation, not storage.
- Runtime data: scene capability, reduced motion, viewport, pointer, and visibility are runtime inputs owned by the scene scheduler, not product state.
- Telemetry: browser spans, fetch spans, runtime errors, unhandled rejections, and CSP reports flow through the existing browser telemetry surfaces.

Live product widgets should consume normalized view models, not raw API responses. Each app gets a small adapter layer that merges route-loader data, live collection rows, and notification summary state into one immutable app snapshot. Animation components can then conflate snapshots like the flight machine does: latest state wins, intermediate server states are not replayed unless the domain requires an audit timeline.

## Notification Mainstays

The bottom-right shell control should become a mainstay notification/account dock. It stays mounted across app switches and route errors.

Behavior:

- A durable unread count or severity halo appears around the bottom-right button.
- Agent or webhook changes that affect the user's accounts create notifications with stable IDs and target app IDs.
- Clicking the button opens a compact notification/account surface without replacing the center app.
- Opening a notification can focus the relevant app and item through URL state.
- Mark-read/dismiss writes go through notifications-service, then rebase the live projection.
- Toasts may mirror high-priority arrivals, but the dock is the persistent recovery surface.

Examples:

- An agent adds a repository credential out of band: the dock gets a small account/security badge, and `account-security` can focus the changed credential.
- A run moves from cold to on-time: `ci-flight` updates in place without a toast.
- A policy or billing action needs attention: the dock shows severity, and Navee can surface a two-word status.

## Error Boundary Surface

Error handling should be layered so the shell remains useful even when an app fails.

- Route boundary: the existing TanStack Router default boundary catches document/route failures and offers retry plus a safe console fallback.
- Shell boundary: keeps Guardian chrome, mainstay dock, auth/account access, and app switcher alive even if the center app crashes.
- App boundary: wraps each registered app and renders an app-shaped failure panel with retry, diagnostics, and a route-safe fallback.
- Widget boundary: isolates optional subwidgets so a log viewer, mini chart, or scene overlay does not blank the app.
- Scene boundary: catches WebGL/WASM initialization and runtime faults and swaps to a static panel treatment.
- Mutation boundary: conflicts, permission failures, and validation failures stay near the command that caused them; they do not become whole-page route errors.

Error surfaces should normalize SDK/service errors into stable UI cases:

- `auth_required`: explain the authentication requirement and send the user to login.
- `permission_denied`: lampshade the missing org/account capability.
- `not_found`: show a resource-focused empty state and safe app fallback.
- `conflict`: show the current server version and let the user rebase.
- `rate_limited`: show product quota/billing context when relevant.
- `service_unavailable`: preserve stale data if available and mark the app degraded.
- `client_runtime`: report telemetry and let the user reset the app.
- `scene_fault`: keep the app's data panel alive and disable only the scene layer.

Every error surface should emit telemetry with `app_id`, `route_path`, `org_slug`, `query_key` or `collection_id` when present, and a stable problem type. The user-facing copy stays short; ClickHouse and traces carry the detail.

## Scene Runtime

Multiple WASM or Three scenes are allowed, but they are runtime modules inside the unified app, not independently booted frontends.

Rules:

- Scene modules register with one `SceneRuntime` and one scheduler.
- The shell decides which scene is active, warm, paused, or disposed based on the focused app, adjacent deck cards, document visibility, reduced motion, and device capability.
- Heavy WASM scenes run behind a typed message protocol, preferably in a worker when they can do meaningful work off the main thread.
- React passes coarse scene props; scene frame state must not drive React renders.
- The initial shell must be useful before scene code loads.
- App switches may preload the adjacent app's data and scene bundle, but only the focused app gets full render budget.
- Scene failures degrade to static card art and remain observable through the app error surface.

This gives us the feel of multiple live scenes without paying the TTI, memory, and transition-latency cost of separate frontend runtimes.

## Concurrency Model

The browser app should behave like a concurrent control surface:

- SSR renders the shared shell and a stable initial app frame.
- TanStack Router owns navigation, intent preloading, search params, and route-level data seeding.
- TanStack Query owns async service reads, mutation invalidation, and stale data retention.
- TanStack DB/Electric owns live collection reads from durable projections.
- App snapshots are pure derived values, so render components can stay simple.
- Live updates are conflated unless the app is explicitly showing a timeline.
- Long-running visual work belongs to the scene scheduler, not React state.
- URL state identifies selected app, focused item, debug fixture, and shareable filters.

Avoid adding a second global state store until there is a concrete state class that TanStack Query, TanStack DB, Router search state, refs, or CSS cannot represent cleanly.

## URL State

Use TanStack Router search state as the default URL-state system. The app already uses `validateSearch`, typed `Link` search objects, route loaders, redirects, and `Route.useSearch()`. Keeping search state router-owned means loaders, preloads, SSR, redirects, and links all agree on the same parsed shape.

Do not add `nuqs` for the first cut. It solves a real class of query-string ergonomics problems, but here it would create a second URL-state authority beside TanStack Router. Reconsider only if route-level search schemas become too noisy after we have several apps and repeated typed helpers have failed to keep them readable.

Split search params into two groups:

- Shell params: stable across apps and owned by the shell.
- App params: interpreted only by the focused app.

Candidate shell params:

- `app`: focused center app, for example `ci-flight`, `cache-health`, or `account-security`.
- `panel`: app-local panel inside the deck when the app has multiple cards.
- `dock`: open shell dock, for example `notifications`, `account`, or absent.
- `banner`: requested shell banner, for example `signin`, `upgrade`, or `org`.
- `focus`: stable resource ID that an app can highlight after navigation or notification open.
- `intent`: short-lived ceremony intent, for example `login`, `select-account`, or `checkout`.
- `debug`: fixture/debug mode for local design review.

Candidate app params stay namespaced by the app parser. For example, `ci-flight` may accept `flight`, `src`, `dst`, `state`, and `commits` while `account-security` may accept `session` or `account`.

Rules:

- Every app owns a parser that converts raw search into a small typed state.
- Unknown values are ignored or coerced to safe defaults; they should not crash render.
- Shareable app state belongs in the URL.
- Server-owned mutable state belongs in services and projections, not the URL.
- Ephemeral animation state belongs in refs/CSS/scene runtime, not the URL.
- Search updates use router navigation so route preloading and scroll restoration remain coherent.
- Banner state can be URL-addressed, but service-derived gates take precedence over requested banners.

The desired shape is `route search -> parsed shell state -> app state -> pure app snapshot`, with no component reading raw `URLSearchParams`.

## Navee

Navee is the unified DotMatrix scene, status actor, and dock control. It is not controlled by auth or route props. It lives near the root, reads observed route/auth/data facts itself, and owns a small reducer that later grows to process durable notifications, product summaries, and inference-provider messages.

When signed out on landing/login routes, Navee presents as the full-screen ambient scene. When the console is active, the same mounted scene is hosted by a dock-sized aperture behind the bottom `For You` control. CSS moves and resizes the aperture at posture boundaries, so the dock renders the living scene itself rather than a dark masked slice of the fullscreen background.

The DotMatrix shader may use a compact-aperture luminance boost for very small hosts. That boost exists only to keep the same WebGL scene legible once it is hosted in the dock; the fullscreen treatment should remain quiet and ambient.

Navee is not a primary CTA. It is a calm status actor that gives the shell a living center of gravity:

- Shape: full-screen ambient field when expanded; compact rounded dock when docked.
- Placement: bottom-center of the shell rhythm when docked, visually tied to the deck.
- Text: two to four words, never a sentence.
- Animation: slow crossfade, slight blur/opacity change only. Respect reduced motion.
- Data source: initially reducer-backed, later from durable notifications, product health summaries, Electric projections, ClickHouse summaries, and inference-provider responses.

Initial reducer state:

- `expanded`: landing/login posture; aperture is full-screen.
- `docked`: console posture; aperture is the Navee dock.
- `attention`: a durable notification or product signal needs recovery.
- `speaking`: an inference-generated message is being displayed.
- `hidden`: non-Navee routes such as docs, policy, device, or API routes.

Example Navee messages:

- `Cache hot`
- `ASH stable`
- `Snapshot ready`
- `4 commits queued`
- `No policy drift`
- `Billing window calm`

## Visual System

Use the shared UI page primitives for ordinary pages. The shell/deck is a product object and can define its own spatial composition, but it should still honor the same restraint:

- Use few text sizes. Operational labels stay small; panel titles stay compact.
- Use tabular numerals for counts, durations, commit totals, and money.
- Prefer lucide icons for controls when an icon exists.
- Do not use visible copy to explain controls that can be expressed by recognizable icons.
- Keep card radii restrained for operational cards; the central deck can be more tactile than normal tables, but should not become bubbly.
- Avoid a dominant purple or wellness-app palette. The reference's pink/purple shader is inspiration for motion and depth, not the Verself color system.
- Use dark neutral surfaces with deliberate accents: Guardian green for healthy state, marigold for queued or attention states, red only for true risk.

## Motion

Motion should make the deck feel alive without making infrastructure feel unserious.

- Prefer CSS keyframes, transforms, and opacity over imperative DOM animation.
- Do not introduce `useEffect` for animation.
- Keep panel transitions short and bounded.
- Keep Navee text cycles slow enough to be ambient, not ticker-like.
- Reduced-motion users should see a stable focused panel and static Navee text.

## Implementation Notes

Likely component boundaries:

- `NaveeProvider`: root-owned reducer, route posture resolver, and future subscription owner.
- `NaveeStage`: mounted DotMatrix scene hosted by the expanded or docked aperture.
- `NaveeDockButton`: semantic control and text layer aligned to the dock aperture.
- `VerselfShell`: shared public/authenticated chrome and content wrapper.
- `InsightDeckShell`: centered app-surface layout and future keyboard/swipe affordances.
- `InsightPanel`: stable panel object with title, status, visual treatment, and metric slots.
- `ConsoleAppRegistry`: typed registry of center apps and their data/scene/error contracts.
- `ConsoleAppHost`: focused app boundary, preload coordinator, and app-shaped pending/error states.
- `SceneScheduler`: shared budget and lifecycle owner for Three/WASM scenes.

State and data rules:

- Do not mirror auth state in component state.
- Keep deck selection URL-addressable when it represents navigation or shareable debug state.
- Use TanStack Query primitives for live product data when the deck leaves fixtures.
- Use TanStack DB/Electric collections for server-originated out-of-band projection changes.
- Keep browser-only animation helpers isolated behind `ClientOnly` only when required.

## Non-Goals

- Do not copy the iPhone device frame, status bar, or consumer wellness content.
- Do not turn the console into a dense dashboard grid.
- Do not import the downloaded video frames into the repo.
- Do not add bottom text buttons that duplicate icon controls.
- Do not create a second authenticated shell.

## Open Decisions

- Which three panels ship first after the CI flight panel.
- Whether the active panel uses live shader motion, still imagery, or a purely CSS surface for the first cut.
