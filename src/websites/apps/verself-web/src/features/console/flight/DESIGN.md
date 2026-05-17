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
  OLED card, the commit pill, and the commit-glyph **well** (a dark inset
  squircle inside the pill — note d's negative-space treatment). There is **no
  light tray** and **no Guardian chip** (the mark is bare wings) — depth is a
  layered shadow (see Layout), not a border. The pill's radius is held well
  under its half-height so the corner reads as a squircle _rounded rectangle_,
  not a stadium; the well's radius is `concentric(pill, inset)` so it nests on
  Apple's nested-rounded-rect rule rather than an ad-hoc value.
- The card corner is **bulbous and only slightly asymmetric**: the prior
  `{ 28, 46 }` read as a tight top over an oblong bottom. The fix is the radius,
  not the smoothing — `cornerSmoothing` stays Apple's canonical `0.6` (the
  figma plugin's default; changing it would make the curve _less_ Apple-true).
  Both corners grow and converge; the top still tucks marginally tighter than
  the free bottom edge (the Dynamic-Island wheel metaphor) but the gap is now
  small, so the card reads full and rounded like the reference.

Radii (single source of truth, `flight-widget.tsx`): card
`{ top: 42, bottom: 52 }`, pill `13`. The glyph well derives from the pill via
`concentric()` — not a checked-in constant.

## Color and type

The palette is OLED-tuned and expressed only in `oklch` (no sRGB anywhere in
the widget). The on-time accent is **iOS `systemGreen` (dark)** — `#30D158`,
the value the reference renders on a dark lock screen — computed once from
sRGB → OKLab as `oklch(0.7556 0.2082 146.98)`. The widget mirrors the iOS
Live-Activity context literally: brand Flare was a documented divergence here
and has been **reverted** (see git history) so the green is the platform's,
not the Newsroom treatment's. The running-late accent keeps the reference
marigold amber. The commit glyph alone paints iOS `systemOrange` (dark) —
`#FF9F0A` → `oklch(0.7824 0.1711 67.22)`.

| Token               | Value                      | Use                          |
| ------------------- | -------------------------- | ---------------------------- |
| `INK`               | `oklch(1 0 0)`             | terminals, header, marker    |
| `INK_DIM`           | `oklch(0.62 0 0)`          | empty-state text             |
| `CARD`              | `oklch(0.05 0 0)`          | OLED card, commit-glyph well |
| `NODE_INK`          | `oklch(0 0 0)`             | glyphs on accent fills       |
| accent · iOS green  | `oklch(0.7556 0.2082 147)` | on-time / boarding           |
| accent · amber      | `oklch(0.80 0.16 70)`      | running late, commit pill    |
| commit · iOS orange | `oklch(0.7824 0.1711 67)`  | commit glyph in its well     |

Typography uses the Apple system stack
(`-apple-system, BlinkMacSystemFont, "SF Pro Display", "SF Pro Text",
system-ui, sans-serif`) so the widget renders true San Francisco on Apple
devices and a near-equivalent neo-grotesque elsewhere. Brand Geist is not used.
Terminals (`PR47`, `MAIN`) are the dominant visual mass but the reference's
SFO/JFK is bold-not-heavy: weight `620` (between semibold and bold), tracking
`-0.01em`, line-height `1`, sized a touch under the prior pass so the arc keeps
the central span. The region actor sits at the header size with near-zero
tracking (`0.01em`) so a short all-caps run like `ASH` reads as one tight
word, not spaced-out letters. The two status lines (`On Time` / `Building in
1m`) are **one group**: both set at the actor/header size, leadings collapsed,
near-identical weight (headline `590`, detail `560`) so the difference is
luminance not boldness — the headline carries full accent, the detail the same
hue dimmed; the pair reads as a unit, not a headline plus a caption.

## Scale model

Composition over ad-hoc numbers. One pure function, `scaleOf(cardW)`, maps the
measured card width to the entire figure scale; nothing downstream invents a
size. The card is measured once (`useMeasuredWidth`, the same sanctioned
DOM-measurement exception as `<Squircle>`/`<FlightArc>`); SSR and the first
client render fall back to the max-width scale, so there is no hydration shift.

`Scale = { terminalPx, discPx, arrowPx, arcStroke, markerR, headerPx }`, all
derived from `terminalPx`:

- `terminalPx` is clamped (`28 … 48`) off card width — terminals dominate like
  SFO/JFK but the reference's are bold-not-huge, so the band is a touch under
  the prior `30 … 52` (brief note h).
- `discPx = round(terminalPx · SF_CAP_RATIO · DISC_TO_CAP)`. The endpoint
  disc diameter is **≈ half the airport-code cap height** (brief note a);
  `SF_CAP_RATIO = 0.714` (SF Pro Display cap-height ÷ em), `DISC_TO_CAP` tuned
  near `0.5`.
- `arrowPx`, `arcStroke`, `markerR` are fractions of `discPx` — the prior pass
  had the stroke, dotted dots and triangle 2–3× too heavy against the
  reference (brief note a). They drop hard: the arc is a **thin confident
  line**, the marker a small leading triangle, the disc arrow a fine 2-weight
  stroke. One scale, retuned in one place; nothing downstream invents a size.
  `discPx` is also the arc's endpoint inset, so the curve provably runs
  disc-centre → disc-centre.
- `headerPx` sizes the actor, **and the two status lines** (note e: the status
  group matches the `ASH` size), so the bottom-left group is derived from the
  scale, never a fixed rem.

## Layout

`WIDGET_MAX_PX = 598` is the whole widget width. The card's **black box is
exactly `CARD_H_PX = 160`px tall** — its padding lives inside the 160; only the
surrounding page gutter is outside (brief note c). `CARD_H_PX` is a parameter,
not a literal sprinkled through the layout: `bandsOf(height)` is the single
pure function that splits any card height into header / route / status bands,
so the future **lock-screen variant (320px, not yet designed)** is one
argument, never a forked layout. Interior vertical padding is generous (note c
asks for more top/bottom air than the prior pass). Depth is a three-layer
`box-shadow` — contact `0 1px 2px /.07`, key `0 5px 12px -4px /.16`, ambient
`0 22px 46px -16px /.30`, one overhead light source (offsets vertical, opacity
falling as blur grows) — so the card reads as a low-elevation Live Activity
floating on the page. The signed-in surface keeps the **default light console
background** (`app-shell.tsx` unchanged): on light the dark shadow reads
directly (it would vanish on black, where a faint light hairline would replace
it instead), matching the reference. Below 598px every element shrinks fluidly
through the scale model; terminals are capped + `shrink-0` so the arc keeps the
dominant central span. Vertically centered, `5` horizontal page gutter.

Card vertical structure (deterministic `bandsOf(CARD_H_PX)` split — **not**
`flex`/`justify-between`; `h-full` against an indefinite flex parent collapses
the arc to zero, so the band heights are computed px, padding inside):

- **Header row.** Region actor (`ASH`) left; the house mark right — mirroring
  the reference's `FL234 … ✈ FLIGHTY`. The mark is **bare white wings only**:
  no chip, no `GUARDIAN` wordmark. It is the same argent glyph the company
  masthead carries (`@verself/brand` `WINGS_PATH_D`), painted `INK`, sized
  quiet — restraint over a lockup, and it keeps the widget free of brand Geist
  so the whole card stays one San-Francisco type system.
- **Route row** (centered). Source terminal, then a no-gap path group
  [source disc · flight arc · dest disc], then dest terminal. The route band
  owns the measured box and computes the bezier **once**; the arc consumes it
  and so do the two endpoint arrows — each disc arrow is rotated to
  `tangentDeg(bezier, 0)` / `tangentDeg(bezier, 1)` (brief note b: the arrow
  carries the curve's climb-out / descent direction, not a hardcoded 45°
  glyph). One geometry source feeds arc + marker + both arrows; the lucide
  directional icons are removed. The arc host is an absolute layer with a
  lower `z`-index than the discs, so the curve tucks **under** both discs and
  reads as one continuous line.
- **Status / pill row** (`items-end`, `justify-between`). The two status lines
  on the left as one tight group (no vertical padding — brief note e); the
  commit pill on the right.

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

The arc is its own component. The **route band** measures the box and builds
the one cubic bezier (so the same curve drives the arc _and_ the two endpoint
arrow angles — single geometry source, brief note b); the arc consumes that
bezier and owns the split at the marker and the marker's position, rotation,
and the progress shimmer.

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
- **Progress shimmer (brief note l).** The solid head periodically flashes a
  brighter band that sweeps origin → marker, the standard "work is in motion"
  cue. It is a **decorative layer with no state-machine coupling**: a looping
  SVG gradient masked to the solid sub-path, NOT a second progress channel. It
  never reads or moves `t`, never feeds the monotone gate, and is gated to the
  building phases (off at `boarding` t≡0 and `late`). Removing it changes
  nothing about phase, progress, or the marker.
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
phase change (each of L, C, H rides its own spring; iOS green → amber is a
smooth sweep). Monotonicity is enforced on the target, never inside the
spring; the spring's at-rest signal is the machine's discrete-phase morph
boundary. The **progress shimmer is the one animation deliberately outside the
spring engine** — a bounded, self-contained decorative loop (note l) with no
input from and no output to phase, progress, or the monotone gate, so it is
structurally unable to perturb the marker.

## Content and iconography

- Actor label is uppercased (`ASH`). Source is `PR<n>` for PRs, otherwise the
  uppercased head branch. Destination is the uppercased base branch.
- The house mark is bare white wings, not a per-run datum; it is the same on
  every card. No chip, no wordmark.
- No "jobs left" anywhere. The concept is removed from the model, not hidden.
- The commit pill keeps the production git-commit glyph (solid centre node,
  heavier connector bars). The pill is amber; inside it, a small dark
  **well** — an OLED-black `<Squircle>` whose radius is `concentric()` of the
  pill's — holds the glyph painted iOS `systemOrange` (brief note d's
  negative-space treatment: the glyph reads as orange light through a punched
  recess, not ink on amber). The well and the glyph are sized to ≈ the
  reference baggage icon. The pill is a `<Squircle>` with an integer-px box (so
  the clip-path never hairline-aliases — brief note k) and links to the logs.

## Mocking

No backend. All states are reachable through the console index search params,
auth-gated and opt-in (live is the default):

- `?flight=no-build | boarding | building-on-time | running-late` — named
  fixtures pushed through `model.ts` as raw string rows, exercising the data
  contract.
- `?flight=debug&actor=&src=&dst=&status=&state=&remaining=&commits=` — one
  card seeded directly from params. `state` is `ontime` or `late`.
- `&frame=iphone17` — a **dev-only** calibration overlay that renders the
  card inside a pixel-accurate iPhone 17 bezel + screen radius so the squircle
  can be tuned 1:1 against the device. It is opt-in via search param and never
  part of the shipped console surface (the widget _is_ the production surface;
  it is never wrapped in a device frame in normal use).

## Module seams

| Concern                                                                  | Module              | Pure |
| ------------------------------------------------------------------------ | ------------------- | ---- |
| Electric row → `Flight`                                                  | `model.ts`          | yes  |
| classify `Phase`, `project` → `Projection`, `MORPH` table, monotone gate | `phase.ts`          | yes  |
| conflating cell, sampler, FSM driver (`useFlightMachine`)                | `machine.ts`        | no   |
| bezier point/tangent/split                                               | `geometry.ts`       | yes  |
| full per-corner figma-squircle, `concentric` (well nests in pill)        | `squircle.tsx`      | no   |
| interpolation hooks, accent resolution                                   | `springs.ts`        | no   |
| arc draw + marker + decorative shimmer (consumes a bezier)               | `flight-arc.tsx`    | no   |
| scale model, one bezier, endpoint-tangent arrows, composition            | `flight-widget.tsx` | no   |

`model.ts` carries no clock, phase, or presentation. `phase.ts` is pure and the
state machine is independently auditable there; `machine.ts` is the only
stateful seam (clock, conflation, morph boundary). `flight-widget.tsx` carries
no math or logic beyond the pure `scaleOf` token map and consumes only a
`Projection`.
