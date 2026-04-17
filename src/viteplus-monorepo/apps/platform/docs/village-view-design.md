# Village View — Operator Console Design Brief (v0)

## 1. Context

forge-metal is a self-hosted product substrate that a solo non-technical founder ("the
operator") uses to run an online business off a single bare-metal server. The substrate
bundles identity, billing, inbound mail, source control, CI runners, isolated VM workloads,
observability, and frontend apps. It is designed to replace an armful of SaaS vendors at
commodity infrastructure cost.

The operator is not an SRE. They are a founder who wants a five-minute check-in, usually on
their phone, to answer three questions:

1. Is anything on fire?
2. Did we make money today?
3. Did anyone new show up?

Every existing observability surface in this space (Grafana, Datadog, New Relic, CloudWatch)
answers these with charts, tables, and yaml. That is the wrong instrument for this audience.

## 2. Product Concept

A game-like, illustrative operator console styled after builder games (Clash of Clans,
Townscaper, Dorfromantik). The platform is rendered as a _village_: each service is a
_building_, each customer is a _villager_, _tent_, _house_, or _building_ according to spend
tier, and incoming external dependencies (Stripe, Cloudflare, Resend, Latitude.sh) arrive
as _caravans_ from off-map.

The village is the primary surface. Existing engineer-facing surfaces (Grafana, raw logs,
traces) remain accessible through a "developer mode" but are not the main experience.

### Why the metaphor earns its keep

- **Legibility at a glance.** A burning building communicates more than a red dot on a dashboard.
- **Affective feedback.** A growing village that fills with tents as customers sign up creates
  an intrinsic reward loop for the operator during the early-stage slog.
- **Teachability.** Infrastructure concepts (trust tiers, service dependencies, ingress) map
  naturally onto castle/keep/gate archetypes the operator already has intuition for.

### What it is not

- Not a replacement for Grafana. Grafana still exists for deep debugging.
- Not a policy editor. The topology's trust tiers are enforced server-side; rearrangement is
  aesthetic only (see §7).
- Not a direct action surface. All write operations in v0 go through an agent console
  (see §10), not through buttons on buildings.

## 3. Primary User & Jobs-to-be-Done

**Primary user.** Solo operator. Non-technical founder. Checks the console two to six times
per day, mostly on phone, occasionally on desktop when something needs attention.

**Jobs:**

| #   | Job                                                  | Answered by   |
| --- | ---------------------------------------------------- | ------------- |
| 1   | "Is everything OK right now?"                        | System Health |
| 2   | "Did anything important happen since I last looked?" | Raven feed    |
| 3   | "Did we make money today / this week / this month?"  | Financials    |
| 4   | "Did anyone new sign up?"                            | Growth        |
| 5   | "Something looks wrong — can you fix or explain it?" | Agent console |

All five jobs must be completable in under sixty seconds on a phone.

## 4. Information Architecture

Three top-level sections. Mobile renders these as a bottom tab bar; tablet as a collapsed
icon rail; desktop as a full left rail with submenus, mirroring the pattern already used at
`platform.<domain>`.

```
System Health              Financials                 Growth
├── Village (map)          ├── This month             ├── Arrivals (signups)
├── Gate District          ├── Revenue by product     ├── Conversion
├── Town Proper            ├── Outstanding invoices   ├── Active cohorts
├── The Keep               ├── Credit inventory       ├── Referral & attribution
├── Raven feed             ├── Dunning queue          └── Traffic sources
└── Developer mode         └── Ledger explorer
```

On mobile, two additional persistent tabs live at the bottom bar:

- **Raven** — the event feed (§8), available from any section
- **Oracle** — the agent console (§10), available from any section

On desktop these are side rails rather than tabs.

## 5. The Village: Districts and Buildings

The village is divided into three _districts_. The districts map 1:1 onto the product's
three security rings. The operator understands them as Outer Walls / Town Proper / Keep.
The engineering team understands them as Ring 1 / Ring 2 / Ring 3.

Within each district the operator may rearrange buildings freely. They may not drag a
building across a district boundary. Attempting to do so snaps the building back with a
subtle haptic and a toast explaining _why_ ("The Smelter must live inside the Keep").

### 5.1 Gate District (Ring 1, internet-exposed)

Walled frontage. This is where all external traffic arrives. Buildings sit along or just
inside the wall.

| Building           | Service                                   | Notes                                                         |
| ------------------ | ----------------------------------------- | ------------------------------------------------------------- |
| The Gate           | Caddy + Coraza WAF + nftables ingress     | Animated guards; "under attack" posture on WAF spikes         |
| Herald's Stage × N | Frontend apps (TanStack Start)            | One per public app (this console, rent-a-sandbox, letters, …) |
| Customs House      | sandbox-rental-service public API         | Packages and crates in the yard scale with active rentals     |
| Letter Slot        | Stalwart SMTP/JMAP + mailbox-service hook | Mail sacks pile up with queue depth                           |
| Tollbooth          | billing-service Stripe webhook            | A visible coin-chute; clinks on charge                        |
| Code Forge         | Forgejo                                   | Anvil sparks on pushes                                        |
| Watchtower         | Grafana                                   | Deep-link surface for developer mode                          |

### 5.2 Town Proper (Ring 2, private userspace)

Interior district. No direct external exposure. Services and stateful stores.

| Building             | Service                                  | Notes                                                    |
| -------------------- | ---------------------------------------- | -------------------------------------------------------- |
| Town Hall            | Zitadel                                  | Bell rings on new signup                                 |
| Counting House       | billing-service core                     | Ledgers visible through open doors                       |
| Treasury Vault       | TigerBeetle                              | Sits in the Counting House courtyard, not a separate pin |
| Library Row × N      | Postgres databases (one building per DB) | billing, mailbox, sandbox-rental, zitadel, forgejo, …    |
| Observatory          | ClickHouse                               | Telescope tracks event velocity                          |
| Scribe's Office      | OTel collector                           | Scribe bustle scales with span rate                      |
| Post Office          | Stalwart internal + mailbox-service core | Mail carts shuttle to/from Letter Slot                   |
| Bazaar               | Verdaccio (npm mirror)                   | Stalls restock on fetches                                |
| Cartographer's Guild | Cloudflare DNS client                    | Only Ring 2 building with a dotted road off-map          |

### 5.3 The Keep (Ring 3, root/host)

Inner sanctum. Walled and moated from Town Proper. Only vm-orchestrator's gRPC socket
bridges in and out; this is rendered as a single portcullis.

| Building    | Service                                | Notes                                                |
| ----------- | -------------------------------------- | ---------------------------------------------------- |
| The Smelter | vm-orchestrator (privileged Go daemon) | Smoke plume intensity ≈ VM spawn rate                |
| Quarry      | ZFS pool                               | Stockpiles of stone = zvol snapshots / checkpoints   |
| Jail        | jailer + Firecracker microVMs          | One structure, occupancy meter, not one-per-VM       |
| Scout's Hut | vm-guest-telemetry (inside each cell)  | Tiny antennae on each cell; visible only when zoomed |

### 5.4 Off-map caravans (external providers)

External dependencies are roads leading off the edge of the canvas, not placeable buildings.
This reinforces the self-hosting narrative: everything substantive is _inside_ the village.

- **Stripe** — gold caravan into Tollbooth
- **Resend** — mail caravan into Letter Slot
- **Cloudflare** — cartographer's raven flying off to Cartographer's Guild
- **Latitude.sh** — the ground itself; rendered as a foundation stone stamp at map corner

## 6. Customer Representation (operator-configurable)

Different operators run different businesses. A SaaS with a generous free tier will look
very different from a B2B usage-billed product. The mapping between spend tier and visual
representation is therefore configurable, with opinionated defaults.

### 6.1 Default tier → visual mapping

| Tier                    | Visual      | Placement                                       |
| ----------------------- | ----------- | ----------------------------------------------- |
| Free / trial            | Villager    | Walks the town square; idle animation           |
| Active paid (below mid) | Tent        | Encampment district just outside Town Proper    |
| Mid ($1k–$10k MRR)      | House       | Neighborhood district with assigned plots       |
| Enterprise ($10k+ MRR)  | Named Manor | Own micro-district with crest, name floats over |

### 6.2 Operator configuration

The operator edits thresholds (in a simple, opinionated settings screen — not free-form
yaml) to match their revenue model. Default thresholds are per-operator business-type
presets: "SaaS with free tier", "B2B usage-billed", "Marketplace", etc.

### 6.3 Scale behavior

Villagers and tents are aggregated once counts exceed a screen-legibility threshold
(suggest ~40 per district); beyond that a _crowd_ texture fills the plot with a live count
label. Individual named buildings (Enterprise) never aggregate.

## 7. Interaction Model

### 7.1 Camera

Pan with drag, pinch-zoom on touch, scroll/wheel on desktop. Two zoom presets:

- **Overview** — whole village fits; used for "at a glance" health
- **Street** — single district fills viewport; buildings fully detailed

Double-tap any building to snap-zoom to it.

### 7.2 Tap a building → drill-in

Opens a **bottom sheet** on mobile, a **right drawer** on tablet/desktop. Content:

- Building name (thematic) and service name (small, subdued)
- Current status chip (Healthy / Degraded / Critical / Offline / Upgrading)
- Three key metrics per building, chosen by the building (not a generic template)
- Recent relevant events filtered to this building
- "Ask the Oracle" button (§10)
- "Open in Grafana" link (developer mode)

### 7.3 Rearrange within district

Long-press a building, drag, release. Snaps to a grid. Cross-district drags bounce back
with a short toast. No save button; position persists immediately.

### 7.4 No disabled buttons

Per platform convention, v0 contains no `disabled` controls. If an action can't be taken
(agent busy, service offline), the button still responds to press and surfaces a toast
explaining why. This applies to every button in this surface.

## 8. The Raven Feed

A persistent event stream. The in-product name is "the Raven" — messenger birds delivering
news to the keep. Events originate from existing wide events already landed in ClickHouse;
this surface is a read model, not a new source of truth.

### 8.1 Event categories

| Category  | Examples                                                                    |
| --------- | --------------------------------------------------------------------------- |
| System    | VM spawn / void, WAF block spike, service restart, Postgres replication lag |
| Financial | New customer, subscription renewed, invoice finalized, credit grant, refund |
| Growth    | Signup, first paid conversion, referral, org created                        |

### 8.2 Event card anatomy

- Icon (tier-coded; matches the originating building's district palette)
- One-line headline ("Ada Lovelace joined Acme Corp")
- Optional subline with amount or object
- Timestamp (relative)
- Quick actions: **Ask the Oracle**, **Pin**, **Snooze**, **Dismiss**

Pinned events persist across sessions. Snoozed events re-surface at a user-chosen horizon.

### 8.3 Filtering

Filter by section (Health / Financial / Growth), severity, or building. Mobile exposes
these as a horizontal chip strip at the top of the feed.

### 8.4 Empty state

Fresh install shows an illustration of a raven waiting at the tower. Copy: _"No news yet.
The raven is watching."_

## 9. States & Visual Language

### 9.1 Per-building states

| State            | Visual                                             | When                                         |
| ---------------- | -------------------------------------------------- | -------------------------------------------- |
| Healthy          | Default animations, ambient smoke, workers visible | No active alerts                             |
| Informational    | Small blue pennant atop building                   | Non-urgent signal (e.g., version available)  |
| Degraded         | Amber smoke plume; warning icon hovers             | Elevated error rate, latency, or queue depth |
| Critical         | Flames, red icon, subtle shake                     | Hard failures, thresholds breached           |
| Offline / Ruined | Collapsed walls; crows circle                      | Service down / health check failing          |
| Upgrading        | Scaffolding overlay                                | Future: in-place version change              |

### 9.2 Animation tempo as data

Health is not the only signal. _Bustle_ — workers, smoke plume rate, mail cart frequency —
conveys throughput. A quiet town at 11pm versus a bustling one at peak hours is itself a
health signal the operator internalizes without reading a number.

Animation tempo is driven by real telemetry via a small number of building-specific data
bindings (see §13).

### 9.3 Connections (roads)

Connections between buildings are **off by default** to keep the village visually calm.
Tapping a building reveals its allowed and actual connections:

- **Solid road** — connection is allowed and actively used
- **Dotted road** — connection is allowed but currently unused
- **Red barrier** — attempted cross-tier connection blocked (should be rare and indicates a bug)

Connection width and particle density encode volume; particle color encodes error rate.

### 9.4 Palette

District palettes are tonal, not loud. They ladder from warm to cool as trust tier
increases (outside → inside):

- **Gate District** — warm sand, stone, timber
- **Town Proper** — neutral earth, moss, slate-grey
- **The Keep** — cold slate, iron, banked ember

State colors (amber / red / blue) overlay the palette and remain constant across districts.

### 9.5 Day / night

Follows device appearance by default. A scheduled "after hours" mode dims the village and
surfaces only critical states. Separate sprite variants for night are in scope for v1, not
v0 (v0 uses a hue-shift overlay).

## 10. The Oracle (Agent Console)

v0's only action surface. A persistent VM running Claude Code, with SSH-level access to the
bare-metal box, acts as the operator's fix-it and explain-it layer. The UI surface is a
chat drawer that can be invoked from:

- Any building drill-in ("Ask the Oracle about the Counting House")
- Any Raven event card ("Ask the Oracle about this")
- Global Oracle tab

### 10.1 Context priming

When invoked from a building or event, the Oracle receives a structured context blob:

- Building name + service identifier
- Current building state + latest three metrics
- Last N relevant Raven events
- A time-bounded slice of relevant ClickHouse rows (latency, error spans, logs)

The operator does not assemble this. It is the difference between "a chat with Claude" and
"a chat with Claude that already knows what I'm looking at."

### 10.2 UX

- Streamed transcript; chat drawer full-height on mobile, right-side split on desktop
- Agent's tool calls and command output are visible but collapsed by default
- A persistent "Stop" control (not disabled when idle — see §7.4)
- Suggested next questions surfaced as chips beneath the composer

### 10.3 Safety

The Oracle can read and act on the box. For v0 we treat it as an operator-authored action
with full trust, but every tool call the agent makes is recorded to ClickHouse for audit.
The Raven feed surfaces each Oracle session as a dedicated event category.

## 11. Responsive Behaviour

### 11.1 Mobile (primary, 320–428px)

- Bottom tab bar: Health / Financials / Growth / Raven / Oracle
- Village fills the viewport above the tab bar
- All drill-ins are bottom sheets, draggable to full height
- Haptics on state changes, snap-to-grid, critical events
- Pull down on the village to refresh

### 11.2 Tablet (768–1024px)

- Collapsed left icon rail
- Village center, drill-ins as right drawer (not bottom sheet)
- Raven is a collapsible right rail; Oracle is a floating button bottom-right

### 11.3 Desktop (1280px+)

- Full left rail with submenus, mirroring `platform.<domain>` pattern
- Village center
- Persistent right rail split vertically: Raven top, Oracle bottom
- Keyboard shortcuts: `1/2/3` switch sections; `r` toggles Raven; `o` opens Oracle; arrows
  pan the map; `+/-` zoom; `/` focuses search

## 12. Accessibility

- Every building has a text label (default hidden, toggleable globally and on focus)
- State is encoded redundantly: color _and_ icon _and_ text chip in drill-ins
- Motion-preference respected: bustle animations and camera parallax suppress under `prefers-reduced-motion`
- All interactions reachable by keyboard; focus ring visible on desktop
- VoiceOver / TalkBack tested on iOS / Android for mobile flows
- Color contrast: state overlays meet WCAG AA against each district palette

## 13. Content & Terminology

### 13.1 User-facing names are thematic

Building names shown in UI are always the thematic ones ("The Smelter", "Counting House").
Technical service names (`vm-orchestrator`, `billing-service`) appear **only** in:

- Developer mode tooltips (long-press on desktop; gear-enabled elsewhere)
- Grafana deep-links
- Audit logs exposed to developers

This is a hard platform rule, not a style preference.

### 13.2 Copy tone

Warm, concrete, short. The village is not winking at the user — it is a sincere metaphor.
Avoid fantasy flourish ("hark!", "thy"). Do say things like "The Smelter is busy."

### 13.3 Localization posture

v0 English only. Thematic names are allowed to be lightly idiomatic; plan for translation
keys from day one so localization does not require re-ink.

## 14. Sprite Production Plan

Art is produced via **Google Nano Banana** (Gemini image generation, AI Ultra subscription).
A separate artifact, [`village-sprite-brief.md`](./village-sprite-brief.md), contains:

- A master style sheet prompt (run first to establish a seed)
- Per-building prompts parameterised against the style sheet
- State variants (healthy / degraded / critical / offline)
- Export and naming conventions

Placeholder assets in v0 may be flat colored rectangles labeled with the building name. The
wireframe should be landable without final art; final art lands iteratively, one district
at a time.

### 14.1 Style direction

Stylized low-geometry flat isometric, with a warm hand-painted finish — Townscaper meets
Monument Valley. **Not** Clash of Clans-literal 3D (expensive, slow to iterate, harder to
animate state transitions, harder to keep consistent across ~25 buildings).

### 14.2 Consistency discipline

All buildings share:

- Camera angle: 3/4 isometric, 30° pitch
- Lighting: single warm sun, soft shadow to the south-east
- Footprint grid: 1×1, 2×2, or 3×3 tiles; no off-grid footprints
- Palette constrained by district (§9.4)

## 15. Out of Scope for v0

- Buildings accept direct actions (restart, deploy). v0 routes all writes through the Oracle.
- Semantic topology editing (dragging connections generates policy). v0 rearrangement is aesthetic.
- Custom per-building templates (marketplace skins). v0 ships a single village.
- Separate night sprite set. v0 uses a hue-shift overlay.
- Multi-operator (multiple villages under one login). Single operator assumed.
- Historic playback ("rewind my village to last Tuesday"). Great for v1.

## 16. Open Design Questions

Explicitly flagged for the head of design to pressure-test before IC work begins.

1. **Bustle vs. numerics.** How far does the bustle-as-data signal carry before the operator
   still needs a number? Suggest: bustle is the ambient signal, exact numbers appear in
   the drill-in, not the map.
2. **Default connection visibility.** Roads off vs. on by default. This document says off;
   worth validating in prototype — it may feel empty.
3. **Onboarding / founding sequence.** On a fresh install the village is empty. As services
   come up, buildings should materialize. This is its own small set piece and needs a
   decision: animated founding cinematic or simple fade-in?
4. **Raven feed as home-screen widget (iOS).** Worth piloting post-v0.
5. **Developer mode toggle.** Global toggle vs. per-drill-in gear. Recommend global,
   surfaced in the gear menu, persisted per device.
6. **Enterprise building naming.** Who writes the "Acme Corp" label? Pulled from Zitadel
   organization display name, or operator-edited? Recommend: pulled by default, editable
   inline by long-press.
7. **Empty state for Financials / Growth on day one.** Both sections will be nearly empty
   for new operators. Needs illustrative empty states that are motivating, not sad.

## 17. Appendix A — Data Sources (for engineering)

| Surface               | Source                                                                                                                  |
| --------------------- | ----------------------------------------------------------------------------------------------------------------------- |
| Building health state | `forge_metal.otel_traces` status codes + `default.otel_logs` severity, per `service.name`, sliding window               |
| Building bustle tempo | Per-building custom metric (RPS for APIs, VM spawn rate for Smelter, span rate for Scribe, queue depth for Letter Slot) |
| Raven feed            | New unified view over ClickHouse wide events; schema to be added                                                        |
| Financials            | TigerBeetle balances + billing Postgres + Stripe events                                                                 |
| Growth                | Zitadel `user.added` + billing new-subscription events                                                                  |
| Oracle audit          | New ClickHouse table for agent tool calls                                                                               |

Wire contracts follow the repo's `apiwire` conventions; Raven and Oracle endpoints are new
Huma v2 handlers on the platform service.

## 18. Appendix B — Component Inventory (indicative)

A head-of-design estimate of novel components. Reuse shadcn primitives where possible.

- `VillageCanvas` — pan/zoom iso map
- `Building` — single building with state, bustle, connections
- `District` — grouping container with drag boundary
- `Caravan` — off-map arrival route
- `RavenFeed`, `RavenEventCard`
- `BuildingDrawer` (responsive: bottom sheet / right drawer)
- `OracleConsole` (chat transcript + composer + tool-call fold)
- `SectionNav` (left rail with submenus on desktop; bottom bar on mobile)
- `CustomerTierLegend` (settings surface for the tier-mapping configuration)
- `EmptyState` (shared across sections)

---

End of brief. Sprite production prompts live in
[`village-sprite-brief.md`](./village-sprite-brief.md).
