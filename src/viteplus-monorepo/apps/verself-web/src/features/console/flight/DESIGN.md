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
- `<Squircle>` is the card corner source. The commit chip uses the compact
  repo-native `GitCommitGlyph` and tabular count inside the gold button.
- The card corner is **bulbous and only slightly asymmetric**: the prior
  `{ 28, 46 }` read as a tight top over an oblong bottom. The fix is the radius,
  not the smoothing — `cornerSmoothing` stays Apple's canonical `0.6` (the
  figma plugin's default; changing it would make the curve _less_ Apple-true).
  Both corners grow and converge; the top still tucks marginally tighter than
  the free bottom edge (the Dynamic-Island wheel metaphor) but the gap is now
  small, so the card reads full and rounded like the reference.

Radii (single source of truth, `flight-widget.tsx`): card
`{ top: 42, bottom: 52 }`. The commit chip radius comes from the reference
image crop.

## Color and type

The palette is exactly **four colors** (brief note f) and expressed only in
`oklch` (no sRGB anywhere in the widget): white, the card black, one green,
and the gold button. Both chromatic values are **sampled from the reference
photograph itself**, not chosen from a system palette:

- The single accent is the reference's green — `#8FF28F`, a soft,
  low-saturation spring-green, `oklch(0.8767 0.1631 144.03)`. This is _not_
  iOS `systemGreen`: an earlier pass locked `systemGreen` `#30D158` on the
  theory that the photo merely blooms the platform color, but the brief is
  "the color used is the same as the one in the photo / single shade
  everywhere", and the pixel sample is decisively lighter and less saturated.
  That prior decision (and the brand-Flare one before it) is **reverted** —
  see git history; there is no trace of either in the code.
- The commit chip uses the reference marigold gold with `NODE_INK` glyph and
  count painted directly on top.

There is **one accent and it never varies**: `late` is label-only and does
not recolor anything, so the old green⇄amber crossfade is gone (the accent is
a constant, not an animated channel).

| Token      | Value                      | Use                               |
| ---------- | -------------------------- | --------------------------------- |
| `INK`      | `oklch(1 0 0)`             | terminals, header, marker         |
| `INK_DIM`  | `oklch(0.62 0 0)`          | empty-state text                  |
| `CARD`     | `oklch(0.05 0 0)`          | OLED card                         |
| `NODE_INK` | `oklch(0 0 0)`             | disc glyphs                       |
| `ACCENT`   | `oklch(0.8767 0.1631 144)` | arc, both discs, the status group |

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
1m`) are **one group**: both set at the actor/header size, the **same green**,
and the **same weight as `ASH`** (the shared `REGION_WEIGHT` constant) with
leadings collapsed. The brief is explicit that the pair matches the region
actor, and "single shade everywhere" forbids a dimmed second hue, so the only
thing distinguishing the two lines is their text — no headline/caption weight
or luminance hierarchy. (This is a deliberate departure from the reference,
which carries a subtle weight difference; the brief overrides it.)

## Scale model

Composition over ad-hoc numbers. One pure function, `scaleOf(cardW, routeH)`,
maps the measured card width **and the route band the layout computed** to the
entire figure scale; nothing downstream invents a size. The card is measured
once; SSR and the first client render fall back to the max-width scale, so
there is no hydration shift.

`Scale = { terminalPx, discPx, arrowPx, arcStroke, markerR, headerPx,
statusPx, pillH }`, all derived from `terminalPx`:

- `terminalPx = clamp(min(cardW · 0.085, routeH · 0.7), 28, 46)` — width
  governs (the reference's terminals are bold-not-huge, note h) **but it is
  also capped to the route band the safe-rect layout produced**, so the
  terminals can never overflow the band and crush the arc at any card
  height — the composition loop is closed by construction.
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
exactly `CARD_H_PX = 160`px tall** — its padding lives inside the 160; only
the surrounding page gutter is outside (brief note c).

**Safe rectangle (correct by construction).** Content must never enter a
bulbous corner or it clips. `layoutOf(cardW, cardH, radius)` is the single
pure function that computes the largest inner rectangle provably clear of the
corner curve and splits it into header / route / status bands. The proof needs
no SVG-path parsing: figma's corner-smoothing bows the curve _outward_ (a
fuller corner than a plain rounded rect), so for any `cornerSmoothing ≥ 0` the
inward depth of the vendored superellipse at an edge offset is `≤` the depth
of a **circular** arc of the same radius; `cornerDepth(x, r)` returns that
circular bound, and the vertical pad is `max(that floor, aesthetic air)`. So
clipping is impossible _and_ the air matches the reference. The aesthetic air
is `VERTICAL_AIR_RATIO · cardH` (the reference reads ≈1.5–2.5× the prior fixed
16px — note c); side padding tracks width continuously (the old responsive
`px-7 → px-9`) and feeds the safety math as a single source. `CARD_H_PX` is a
parameter, so the future **lock-screen variant (320px, not yet designed)** is
one argument to `layoutOf`, never a forked layout.

**Elevation.** Depth is the `elevation(liftPt)` framework, not hand-written
numbers: one overhead light source and a single _lift_ choice produce the
three stacked layers (contact/key/ambient; vertical offsets, geometry a fixed
multiple of the lift). The card picks `LIVE_ACTIVITY_LIFT` (5pt — Live
Activities float low), which evaluates to exactly contact `0 1px 2px /.07`,
key `0 5px 12px -4px /.16`, ambient `0 22px 46px -16px /.30`. The signed-in
surface keeps the **default light console background**: on light the dark
shadow reads directly (it would vanish on black, where a light hairline would
replace it — a different surface treatment, intentionally out of the
function's scope), matching the reference. Below 598px every element shrinks
fluidly; terminals are capped + `shrink-0` so the arc keeps the dominant
central span. Vertically centered, `5` horizontal page gutter.

Card vertical structure (deterministic `layoutOf` band split — **not**
`flex`/`justify-between`; `h-full` against an indefinite flex parent collapses
the arc to zero, so the band heights are computed px, padding inside):

- **Header row.** Region actor (`ASH`) left. The card carries no top-right
  house mark; that branded affordance belongs to the surrounding shell.
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
diverted, or landed phase. The current console uses fixtures until the flight
data model is real; completion is still not rendered as a separate phase.

Pipeline:

```
data update ─► classify ─► Phase ─► project ─► Projection ─► springs morph
```

The machine is **extensible by construction** (open/closed):

- Add a phase = add a `Phase` variant + one `CLASSIFIERS` row (ordered, first
  match wins) + one `project` arm. Existing rows are untouched.
- The renderer never switches on `Phase`. It consumes a flat `Projection`
  (`progressTarget`, `headline`, `detail`, `phaseKind`), so a new phase
  changes values, not components. There is no `accent` field: the accent is a
  single constant (note f), not a per-phase animated channel.
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
boundaries (intermediates lopped); the one continuous quantity (progress `t`)
uses spring **retargeting**, which conflates by physics — smooth, no replay.

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
- **`late` is label-only.** The phase changes the headline to `Running Late`
  and the marker holds at `LATE_ASYMPTOTE = 0.92`. It does **not** recolor
  anything — single shade everywhere (note f), so there is no accent crossfade.
  No further `late` geometry or treatment is designed pending mockups.

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
  inset by the disc radius. The control points are **numerically fitted**
  (least-squares) to the traced reference arc, not eyeballed: a **shallow,
  front-loaded lob** — a steep climb out of the source disc through an apex at
  x-fraction ≈0.47, then a long gentle descent into the destination (the
  brief's "left third more intense, right two-thirds more gradual", note a).
  Rise is expressed against the disc-to-disc **span**, not the band height, so
  the lob keeps the reference's proportions at any width and may overshoot the
  band into the safe air (the squircle clip is the only real bound).
- The marker is **always** the Verself brand triangle (`▽`), a solid white
  equilateral mark whose circumradius is `markerR` (derived from the disc), at
  `pointAt(t)` and rotated to `tangentDeg(t)` so its apex leads the path. It is
  never a plane and never swapped; the slot exists only so the brand mark is
  injected rather than hardcoded in geometry.
- `t` is the phase-derived progress after the monotone gate, driven by a spring,
  so the marker glides and never reverses.
- **Progress shimmer (brief note g).** A soft bright band flows **continuously**
  origin → marker along the solid head — the standard "work is in motion" cue.
  One period of the gradient is tiled across the path (`spreadMethod="repeat"`,
  ≈1.5 bands visible — "a gradient and a half") and translated by **exactly one
  period** at constant velocity on an indefinite loop; because the pattern is
  period-periodic, +1 period maps it onto itself, so the motion is perfectly
  **seamless — it flows, it does not flash once and pause** (the prior
  sweep-then-hold strobed). It is a **decorative layer with no state-machine
  coupling**: pure SMIL, never reads or moves `t`, never feeds the monotone
  gate, gated to `enroute` only (off at `boarding` t≡0 and `late`). Removing it
  changes nothing about phase, progress, or the marker.
- Geometry (`pointAt`, `tangentDeg`, `splitAt`, `arcGeometry`) is pure and holds
  no React or time. Control-point ratios are the numeric fit above.

## Interpolation

Spring physics are a hand-rolled critically-damped spring (no overshoot — a
flight marker must never bounce backward) on `requestAnimationFrame`, in
`springs.ts`, kept behind hooks so the component tree never imports the engine
directly. Vendored rather than `@react-spring/web` for the same supply-chain
reason as the squircle math (no agent-side lockfile path); the physics is ~40
lines and the monotone guarantee already lives in the machine. The **only**
sprung quantity is arc progress / marker glide — the accent is a single
constant now (no crossfade), so the per-channel L/C/H springs are gone.
Monotonicity is enforced on the target, never inside the spring; the spring's
at-rest signal is the machine's discrete-phase morph boundary. The **progress
shimmer is the one animation deliberately outside the spring engine** — a
bounded, self-contained decorative loop (note g) with no input from and no
output to phase, progress, or the monotone gate, so it is structurally unable
to perturb the marker.

## Content and iconography

- Actor label is uppercased (`ASH`). Source is `PR<n>` for PRs, otherwise the
  uppercased head branch. Destination is the uppercased base branch.
- The card header has no house mark; top-right chrome belongs to the shell.
- No "jobs left" anywhere. The concept is removed from the model, not hidden.
- The commit pill is the gold rounded-rectangle carrying `GitCommitGlyph` and
  the commit count.

## Mocking

No backend. The default console state is a fixture, and all states are
reachable through the console index search params:

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

| Concern                                                                    | Module              | Pure |
| -------------------------------------------------------------------------- | ------------------- | ---- |
| Raw flight row → `Flight`                                                  | `model.ts`          | yes  |
| classify `Phase`, `project` → `Projection`, `MORPH` table, monotone gate   | `phase.ts`          | yes  |
| conflating cell, sampler, FSM driver (`useFlightMachine`)                  | `machine.ts`        | no   |
| bezier point/tangent/split                                                 | `geometry.ts`       | yes  |
| full per-corner figma-squircle (uniform or `{top,bottom}`)                 | `squircle.tsx`      | no   |
| critically-damped scalar spring (progress only)                            | `springs.ts`        | no   |
| `elevation(lift)` → composed 3-layer shadow                                | `elevation.ts`      | yes  |
| arc draw + marker + continuous decorative shimmer (consumes a bezier)      | `flight-arc.tsx`    | no   |
| safe-rect `layoutOf`, scale model, one bezier, tangent arrows, composition | `flight-widget.tsx` | no   |

`model.ts` carries no clock, phase, or presentation. `phase.ts` is pure and the
state machine is independently auditable there; `machine.ts` is the only
stateful seam (clock, conflation, morph boundary). `elevation.ts` and the
`layoutOf`/`scaleOf`/`cornerDepth` helpers are pure; `flight-widget.tsx`
carries no math beyond them and consumes only a `Projection`.
