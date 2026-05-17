# Flight-tracker widget — design contract

Dark-mode-only iOS Live-Activity rendering of in-flight CI. One card per active
workflow run. The card is a pure projection of `(Flight, now)`; the only
stateful node is the 1s clock. This document is the source of truth for visual
and interactive behavior. Numeric tokens are tuned against the Flighty
reference and are owned in `flight-widget.tsx`; the rules and structure here
are fixed.

## Corner radius framework

Every rounded surface is a continuous-corner superellipse (Apple "squircle"),
never a circular `border-radius`. The geometry is figma-squircle's
corner-smoothing math (the canonical port of Figma's reverse-engineered Apple
curve) **vendored verbatim** into `squircle-path.ts` — MIT, attributed. All
three upstream modules (`distribute.ts`, `draw.ts`, `index.ts`) are inlined
unchanged, including the **full per-corner distribution path**
(`distributeAndNormalize`). It is vendored, not npm-installed: this repo's
hardened npm supply-chain posture has no agent-side lockfile path, and reviewed
pure-math source (no install scripts, no transitive deps) is the sanctioned
mitigation.

- `cornerSmoothing` is `0.6` everywhere (Apple's app-icon value).
- The radius is either a uniform `number` or an asymmetric
  `{ top, bottom }`. The **card is asymmetric**: an iOS Live Activity reads as
  a fixed 3-D corner "wheel" projected onto the lock-screen — the top edge
  tucks under the Dynamic Island on a tighter radius while the free bottom
  edge rides a fuller part of the wheel. `distributeAndNormalize` is exactly
  the math that keeps all four corners on Apple's continuous curve while top ≠
  bottom, so a vertical swipe stays seamless with no ad-hoc radius logic.
- `useSquircle` measures the box with a `ResizeObserver`. Before measurement
  (SSR, first paint) it returns a plain `border-radius` (the 4-value shorthand
  carries the asymmetry), derived from props not box size, so there is no
  hydration shift.
- `<Squircle>`/`useSquircle` is the only corner source. Three surfaces: the
  OLED card, the Guardian chip, and the commit pill. There is **no light
  tray** — depth is a layered shadow (see Layout), not a border. The pill's
  radius is held well under its half-height so the corner reads as a squircle
  _rounded rectangle_, not a stadium. `concentric()` remains exported for
  future nesting but is unused.

Radii (single source of truth, `flight-widget.tsx`): card
`{ top: 28, bottom: 46 }`, pill `12`, chip `6`.

## Color and type

The palette is OLED-tuned and expressed only in `oklch` (no sRGB anywhere in
the widget). The on-time accent is the brand **`Flare`** (`#CCFF00`, Pantone
389 C) from the Newsroom treatment, computed once from sRGB → OKLab as
`oklch(0.9307 0.2286 123.09)`. This is a deliberate divergence from the
reference's iOS systemGreen — brand wins over the literal photo. The
running-late accent keeps the reference marigold amber.

| Token          | Value                      | Use                       |
| -------------- | -------------------------- | ------------------------- |
| `INK`          | `oklch(1 0 0)`             | terminals, header, marker |
| `INK_DIM`      | `oklch(0.62 0 0)`          | empty-state text          |
| `CARD`         | `oklch(0.05 0 0)`          | OLED Live-Activity card   |
| `NODE_INK`     | `oklch(0 0 0)`             | glyphs on accent fills    |
| accent · Flare | `oklch(0.9307 0.2286 123)` | on-time / boarding        |
| accent · amber | `oklch(0.80 0.16 70)`      | running late, commit pill |

Typography uses the Apple system stack
(`-apple-system, BlinkMacSystemFont, "SF Pro Display", "SF Pro Text",
system-ui, sans-serif`) so the widget renders true San Francisco on Apple
devices and a near-equivalent neo-grotesque elsewhere. Brand Geist is not used.
Terminals (`PR47`, `MAIN`) are the dominant visual mass — `font-bold`, tracking
`-0.01em`, line-height `1`. The region actor and the GUARDIAN wordmark share
one small header size; the actor keeps light `0.05em` tracking so an all-caps
run reads as a word, not spaced-out letters. The status headline is ~17px
`font-semibold`; the detail shares the meta `text-sm`.

## Scale model

Composition over ad-hoc numbers. One pure function, `scaleOf(cardW)`, maps the
measured card width to the entire figure scale; nothing downstream invents a
size. The card is measured once (`useMeasuredWidth`, the same sanctioned
DOM-measurement exception as `<Squircle>`/`<FlightArc>`); SSR and the first
client render fall back to the max-width scale, so there is no hydration shift.

`Scale = { terminalPx, discPx, arrowPx, arcStroke, markerR, headerPx }`, all
derived from `terminalPx`:

- `terminalPx` is clamped (`30 … 52`) off card width — terminals dominate like
  SFO/JFK and never starve the arc.
- `discPx = round(terminalPx · SF_CAP_RATIO · DISC_TO_CAP)`. The endpoint
  disc diameter is **≈ half the airport-code cap height** (brief note a);
  `SF_CAP_RATIO = 0.714` (SF Pro Display cap-height ÷ em), `DISC_TO_CAP` tuned
  near `0.5`.
- `arrowPx`, `arcStroke`, `markerR` are fractions of `discPx`, so the disc
  arrows, the flight-path stroke and the brand-triangle marker keep one
  consistent visual weight at every card width. `discPx` is also the arc's
  endpoint inset, so the curve provably runs disc-centre → disc-centre.

## Layout

`WIDGET_MAX_PX = 598` is the whole widget width. The card's **black box is
exactly `CARD_H_PX = 160`px tall** — its padding lives inside the 160; only the
surrounding page gutter is outside (brief note c). Depth is a three-layer
`box-shadow` — a tight contact shadow, a medium key drop, and a wide soft
ambient lift (negative spread), one overhead light source — so the card reads
as an elevated iOS Live Activity floating on the page. The signed-in surface
keeps the **default light console background** (`app-shell.tsx` unchanged), so
the shadow reads as a dark elevation on light, matching the reference. Below
598px every element shrinks fluidly through the scale model; terminals are
capped + `shrink-0` so the arc keeps the dominant central span. Vertically
centered, `5` horizontal page gutter.

Card vertical structure (`justify-between` over 160px, padding inside):

- **Header row.** Region actor (`ASH`) left; the Guardian lockup right —
  mirroring the reference's `FL234 … ✈ FLIGHTY`. The lockup is the Guardian
  wings in OLED-black inside a small **white squircle chip** (our own corner
  primitive) followed by the `GUARDIAN` wordmark: the reference's white-chip +
  dark-glyph + wordmark, rebuilt on-brand.
- **Route row** (flex-filling, centered). Source terminal, then a no-gap path
  group [source disc · flight arc · dest disc], then dest terminal. The arc
  host is an absolute layer with a lower `z`-index than the discs, so the
  curve tucks **under** both discs and reads as one continuous line.
- **Status / pill row** (`items-end`, `justify-between`). Two tight status
  rows on the left (no vertical padding — brief note e); the commit pill on
  the right.

## State machine

The widget tracks only what the product tracks: a run's elapsed time against
the repository's historical p50 for the critical-path job. There is no cold,
diverted, or landed phase. A finished run leaves the active Electric shape and
its card unmounts; completion is never rendered.

Pipeline:

```
data update ─► classify ─► Phase ─► project ─► Projection ─► springs morph
```

The machine is **extensible by construction** (open/closed):

- Add a phase = add a `Phase` variant + one `CLASSIFIERS` row (ordered, first
  match wins) + one `project` arm. Existing rows are untouched.
- The renderer never switches on `Phase`. It consumes a flat `Projection`
  (animated targets: `progressTarget`, `accent`, `headline`, `detail`,
  `phaseKind`), so a new phase changes values, not components.
- Transition behavior is declared in `MORPH`, a `(from→to)` table, not branched
  in code. `interruptible: true` (e.g. `enroute→late`) lets a transition preempt
  an in-flight morph; unlisted pairs wait for the morph boundary.

**Conflation (the data structure).** Updates can arrive far faster than a morph
completes; intermediate transitions must be lopped off, not replayed. A growing
FIFO would replay every step — wrong. Because `phaseOf` is a pure, total
function of the _latest_ snapshot, intermediate snapshots carry no state the
newest one doesn't, so the correct structure is a **capacity-1 conflating cell**
(last-write-wins register), not a queue:

- producer (each data update): `cell ← snapshot` — O(1), overwrites.
- consumer (a morph boundary): `read cell → phaseOf → maybe start next morph`.

"Dequeue 97, then go to 98" is not an explicit dequeue — only one snapshot is
ever stored; skipped phases were never recorded. This is the standard
conflation / signal-sampling pattern (Rx `conflate`+`sample`, game-loop "latest
input wins", a size-1 ring). In React the cell is a ref and the sampler is the
spring's rest/step callback (`machine.ts`). Discrete phase is sampled at morph
boundaries (intermediates lopped); continuous quantities (progress `t`, accent)
use spring **retargeting**, which conflates by physics — smooth, no replay.

`Phase` is a discriminated union, total over `(Flight, now)`:

- `boarding` — no job executing yet (`status` is `Queued` or `Waiting`).
- `enroute` — building, at or under p50. Carries `remainingMs: number | null`.
- `late` — building, elapsed has crossed p50.

```
            job starts                 elapsed > p50 (baseline ≠ null)
 boarding ─────────────────►  enroute ──────────────────────────────►  late
 t ≡ 0                        t = clamp(elapsed / p50)                  t held
 progress pinned 0            "On Time"                                 @ 0.92
```

Transition rules and invariants, enforced by construction:

- **Forward-only.** Once `late`, there is no transition back to `enroute` even
  if the baseline is later revised upward.
- **Monotone marker.** `progressTarget` is the only path to the arc and is
  always passed through `monotone(prevMax, candidate)`. The spring eases toward
  a non-decreasing target, so the marker cannot move backward.
- **`boarding` ≡ progress 0.** Pinned in `progressTarget`, not approximated.
- **`late` cannot show an ETA.** Only the `enroute` variant carries
  `remainingMs`; the renderer cannot read an ETA off a late card.
- **No fabricated ETA.** When `baselineMs` is `null` (no history yet) the run
  stays `enroute` and the detail line shows the verb only — this is a rendering
  detail, not a distinct phase. The arc uses a small fixed
  `INDETERMINATE_DRIFT = 0.06` placeholder until a no-baseline arc treatment is
  specified.
- **`late` is label-only.** The phase models the transition; accent crossfades
  to amber and the marker holds at `LATE_ASYMPTOTE = 0.92`. No further `late`
  geometry or treatment is designed pending mockups.

Status lines (bottom-left, two tight rows, no vertical padding):

| Phase                  | Headline       | Detail                                           |
| ---------------------- | -------------- | ------------------------------------------------ |
| `boarding`             | `On Time`      | `<verb>` (e.g. `Queued`)                         |
| `enroute`, baseline    | `On Time`      | `<verb> in <remaining>` (e.g. `Building in 55m`) |
| `enroute`, no baseline | `On Time`      | `<verb>`                                         |
| `late`                 | `Running Late` | `<verb>`                                         |

`formatRemaining` is minute-resolution: `<1 min` floor, `>60 min` ceiling,
`<n> min` otherwise.

## Flight arc

The arc is its own component. It owns one cubic bezier from the source endpoint
to the destination endpoint, the split of that curve at the marker, and the
marker's position and rotation.

- The traversed head (`origin → marker`) is drawn solid; the remaining tail
  (`marker → destination`) is dotted. The split is a De Casteljau subdivision of
  the curve itself, so the dotted dash rhythm stays stable as the marker
  advances rather than shifting under a dash-offset hack. The dotted dash is
  a stroke-scaled round-capped dot pattern so it reads as even dots at any
  card size.
- Both endpoints sit exactly on the vertical centre (the disc-centre line),
  inset by the disc radius; the controls pull hard toward the top so the apex
  clears the discs and the span between them is the dominant lob — the
  reference's confident climb-out, not a flat hop.
- The marker is **always** the Verself brand triangle (`▽`), a solid white
  equilateral mark whose circumradius is `markerR` (derived from the disc), at
  `pointAt(t)` and rotated to `tangentDeg(t)` so its apex leads the path. It is
  never a plane and never swapped; the slot exists only so the brand mark is
  injected rather than hardcoded in geometry.
- `t` is the phase-derived progress after the monotone gate, driven by a spring,
  so the marker glides and never reverses.
- Geometry (`pointAt`, `tangentDeg`, `splitAt`, `arcGeometry`) is pure and holds
  no React or time. Control-point ratios are tuned to the reference.

## Interpolation

Spring physics are a hand-rolled critically-damped spring (no overshoot — a
flight marker must never bounce backward) on `requestAnimationFrame`, in
`springs.ts`, kept behind hooks so the component tree never imports the engine
directly. Vendored rather than `@react-spring/web` for the same supply-chain
reason as the squircle math (no agent-side lockfile path); the physics is ~40
lines and the monotone guarantee already lives in the machine. Animated
quantities: arc progress and marker glide, and the accent oklch crossfade on
phase change (each of L, C, H rides its own spring; `Flare → amber` is a smooth
green→amber sweep). Monotonicity is enforced on the target, never inside the
spring; the spring's at-rest signal is the machine's discrete-phase morph
boundary.

## Content and iconography

- Actor label is uppercased (`ASH`). Source is `PR<n>` for PRs, otherwise the
  uppercased head branch. Destination is the uppercased base branch.
- The Guardian lockup is the house mark, not a per-run datum; it is the same
  on every card.
- No "jobs left" anywhere. The concept is removed from the model, not hidden.
- The commit pill keeps the production git-commit glyph, redrawn: the center
  node is a solid disc and the connector bar is heavier and thicker. It paints
  true black (`currentColor`) on the amber pill. The pill is a `<Squircle>`
  with a fixed integer-px box (so the clip-path never hairline-aliases) and
  links to the run's logs.

## Mocking

No backend. All states are reachable through the console index search params,
auth-gated and opt-in (live is the default):

- `?flight=no-build | boarding | building-on-time | running-late` — named
  fixtures pushed through `model.ts` as raw string rows, exercising the data
  contract.
- `?flight=debug&actor=&src=&dst=&status=&state=&remaining=&commits=` — one
  card seeded directly from params. `state` is `ontime` or `late`.

## Module seams

| Concern                                                                  | Module              | Pure |
| ------------------------------------------------------------------------ | ------------------- | ---- |
| Electric row → `Flight`                                                  | `model.ts`          | yes  |
| classify `Phase`, `project` → `Projection`, `MORPH` table, monotone gate | `phase.ts`          | yes  |
| conflating cell, sampler, FSM driver (`useFlightMachine`)                | `machine.ts`        | no   |
| bezier point/tangent/split                                               | `geometry.ts`       | yes  |
| full per-corner figma-squircle, `concentric`                             | `squircle.tsx`      | no   |
| interpolation hooks, accent resolution                                   | `springs.ts`        | no   |
| arc composition + Verself-triangle marker                                | `flight-arc.tsx`    | no   |
| scale model + widget composition (wiring only)                           | `flight-widget.tsx` | no   |

`model.ts` carries no clock, phase, or presentation. `phase.ts` is pure and the
state machine is independently auditable there; `machine.ts` is the only
stateful seam (clock, conflation, morph boundary). `flight-widget.tsx` carries
no math or logic beyond the pure `scaleOf` token map and consumes only a
`Projection`.
