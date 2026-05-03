# First Light — landing hero

A liquid light source that arrives on the Guardian landing page (`guardianintelligence.org/`), passes through the headline along the word _succeed_, draws back, and settles in a glacial orbit beside the wings. Landing route only. Designers and frontend engineers collaborate from this brief.

## 1. Intent

A single moment, at first-paint settle, where the page goes from dim Argent to lit Argent. The light enters from off-canvas, crosses the headline, draws back, and anchors near the wings, where it remains in a barely-perceptible orbit. The wings are _revealed_ by the light, not its source.

We evoke: protectorship, benevolence, sharing prosperity, helping those without anyone else.

We never evoke: descent, ascension, salvation, or the wings emitting light. The word _angel_ is banned in copy and the metaphor of one is banned in pixels. The wings receive light and reflect it. Custodial, not divine.

## 2. References

- **Liquid Series Seed reel** (image attached to the brief) — celestial body, rim light across curvature, lens flare, heavy grain. We take the _frame_ and the _grain density_; we are not putting a planet on the page.
- **Alan Zucconi — _Improving the Rainbow_** (https://www.alanzucconi.com/2017/07/15/improving-the-rainbow-2/). Canonical wavelength→RGB approximation. Source for the trace of cool-shifted dispersion in the falloff; we use a six-bump physical approximation rather than RGB-channel offsets.
- **Julia Poo — _More accurate Iridescence_** (https://www.shadertoy.com/view/ltKcWh). Thin-film interference from first principles. Source for the brushed-metal iridescence on the wings during arrival.
- **Inigo Quilez — _Articles_** (https://iquilezles.org/articles/). Reference for gradient noise, fbm, easings, and palette construction used inside the bloom region.
- **lygia** (https://github.com/patriciogonzalezvivo/lygia). Curated GLSL/WGSL primitive library. Vendor-pin specific files we use; do not depend as a package.

## 3. Banned moves

- Strobe or flash. The arrival has no peak above ~80% relative luminance and no instantaneous intensity transitions.
- Continuous swirl. After settle, motion is below 1° per second, perceptible only to a viewer who lingers ten seconds.
- Wings emitting light, glowing from within, or appearing tinted by anything but reflected Argent.
- Modal or blocking behavior on first paint. Text and wings render on the first frame; the canvas mounts after.
- Visible "loading the effect" UI.

## 4. Sequence

| t            | event                                                                                                                                                                                                 |
| ------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 0 ms         | Route renders. Text + Argent wings present, flat-lit. Page-level `FilmGrain` already on.                                                                                                              |
| 0–700 ms     | Hold. Reader's eye lands on the kicker and headline.                                                                                                                                                  |
| ~700 ms      | WebGL canvas mounted, shader compiled, first frame buffered.                                                                                                                                          |
| 700 ms       | Light source enters viewport from upper-right, off-canvas origin.                                                                                                                                     |
| 700–1300 ms  | **Arrival** — high intensity, high velocity, eased out. A caustic trail follows the source.                                                                                                           |
| 1300–2100 ms | **Trail** — light traverses the bounding box of the marked headline span (default: _succeed_), elongating along its baseline, intensifying as it crosses the word's centroid.                         |
| 2100–2800 ms | **Draw-back** — light contracts toward the anchor near the wings; intensity falls to ~30% of arrival peak.                                                                                            |
| 2800 ms+     | **Settled** — anchor oscillates within a ~24px radius on a Lissajous-like curve at <1°/s. Anisotropic shimmer on wings. Caustic grain inside the lit region only. Permanent state, no further events. |

Total arrival ≈ 2.8s.

Easing:

- Arrival: `easeOutQuint(t)` — fast in, slow finish.
- Trail: linear along the span's baseline; intensity envelope `sin(πt)` (rises to peak at the word's centroid, falls as it exits).
- Draw-back: `easeInOutCubic(t)` on both position and intensity.

## 5. Visual specification

### Color

- Light source: warm white (`#FFFAF0`), shifted ~5° toward amber at peak. No color cycling.
- **Falloff edge**: ~5–8% cool-blue mix at the outer edge of the bloom, zero at the source. Carries the trace of mystery without tipping into ominous; emerges from spectral dispersion (§2 — Zucconi), not RGB-channel offsets.
- Caustic ripples: derived from the source color via fbm noise. No saturated rainbow dispersion.
- Argent shimmer on wings: existing Argent token, modulated up to +20% luminance where the light sits. Never _below_ base Argent.
- Background ink: unchanged (`var(--color-ink)`, `#0E0E0E`). The light does not raise the global luminance of the page.

### Light model

- The SVG wings receive a measured light field from the canvas layer. A future MSDF pass can add per-sample brushed-metal shimmer once the path is lifted into a geometry constant (see §7).
- Soft volumetric bloom on the source, ~6% viewport diameter at peak.
- Caustic noise (fbm, ~3 octaves) modulates the source's interior, never its outline.
- No hard rim line. The source has no edge — only falloff.

### Type interaction

- The marked span (default: the word _succeed_) receives the trail.
- Shader reads the span's bounding box at mount and on resize, passes it as a uniform.
- During trail phase, the letters of the marked span briefly increase in luminance via a CSS variable `--firstlight-luminance` driven by a `requestAnimationFrame` callback that mirrors the shader's intensity envelope. **Type does not move.**

### Wing interaction

- Wings remain SVG during the entire arrival sequence.
- The shader receives the wings' DOM rect and anchors the settled light beside that rect.
- When the light is fully off (e.g., after a context loss fallback), the wings are the unchanged SVG component.

### Grain

- Page-level `<FilmGrain intensity={0.22}>` continues unchanged.
- Inside the lit region, an additional shader-internal caustic grain runs at ~0.08 intensity — lower than the page grain so the light never reads noisier than its surroundings.

## 6. Renderer choice

**Direct Three.js WebGL2 with generated GLSL3 modules.**

### Stack

- `three` owns renderer setup, material lifecycle, geometry disposal, and the full-screen plane.
- React owns capability gating, DOM measurement, pause/resume, reduced-motion fallback, and telemetry.
- GLSL source lives as checked-in `.glsl` files under `shader-src/`. Bazel resolves `#include` composition and emits `shader/first-light.generated.ts`, which is the only TypeScript transport module imported by React/Three.
- Three runs the material as GLSL3 (`glslVersion: GLSL3`). The source files intentionally omit `#version`; Three injects the version line before its built-in attribute/uniform prelude.

The first implementation uses WebGL2 only. WebGPU and TSL remain plausible later upgrades, but the current goal is a stable editable shader surface with the smallest runtime surface area. The earlier r3f/TSL direction introduced runtime hook incompatibilities in this Vite+ Start app and pulled in scene abstractions before the visual grammar was settled.

### Architectural invariant

One file per visual primitive. The composition file (`shader-src/first-light.frag`) imports the pieces with `#include`; the Bazel generator performs the string assembly, validates the source for GLSL100 footguns, extracts uniform metadata, and writes the generated TypeScript module. React never builds shader strings inline, and the Three scene wrapper never owns visual math.

```
apps/company/src/features/first-light/
  FirstLight.tsx                     -- entry: capability check, lazy mount, CSS fallback
  use-first-light.ts                 -- telemetry, DOM measurement, lifecycle, CSS luminance
  scene/
    FirstLightCanvas.tsx             -- Three renderer, plane, uniforms, RAF, disposal
    metrics.ts                       -- arrival frame-time summary
  shader/
    envelopes.ts                     -- CPU timing constants and CSS luminance envelope
    first-light.generated.ts         -- ignored generated TS transport module
  shader-src/
    first-light.vert                 -- fullscreen plane vertex shader
    first-light.frag                 -- final GLSL3 composition
    lib/
      color.glsl                     -- source color, spectral edge, tonemapping
      easing.glsl                    -- arrival/trail/draw-back easing
      geometry.glsl                  -- rect and transform helpers
      motion.glsl                    -- source path and intensity envelopes
      noise.glsl                     -- fbm and caustic grain primitives
  types.ts                           -- renderer, geometry, and degradation contracts
```

### What we don't take from precedent

We do not borrow third-party scene-graph structure, postprocessing config, lensflare-textures-as-procedural-bloom, or ray-bouncing primitives. The implementation borrows only the public shader references cited in §2.

## 7. Wing geometry

The wings live in `@verself/brand` as the `WingsArgent` SVG component. The path data is currently embedded in JSX. Lift it before any WebGL work begins:

```ts
// packages/brand/src/wings.geometry.ts
export const WINGS_PATH_D = "M…";
export const WINGS_VIEWBOX = { w: 320, h: 200 } as const;
```

`WingsArgent` (existing component, used everywhere in the repo) renders from this constant. A later WebGL wing pass can consume it via a baked **multi-channel signed distance field** (MSDF):

- Build step: a Bazel rule runs `msdfgen` over `WINGS_PATH_D` at the canonical viewBox. `msdfgen` is the industry standard for resolution-independent vector-on-GPU rendering and is used in every modern font atlas pipeline.
- Output: `wings.msdf.png` (~16KB), shipped as a generated static asset under `apps/company/public/`. Not committed.
- The fragment shader samples the MSDF and computes the gradient at the sample point for anisotropic shimmer direction.

The first implementation keeps the SVG wings as the source of truth and measures their DOM rect for light anchoring. The shader lights the region around the wings; it does not redraw the wings. A future MSDF pass must preserve the same component contract.

## 8. Component contract

Path: `apps/company/src/features/first-light/`

```tsx
// FirstLight.tsx
type FirstLightProps = {
  // Span the light sweeps through during the trail phase.
  trailTargetRef: RefObject<HTMLElement>;

  // Element whose bounding box determines where the light anchors after draw-back.
  wingsAnchorRef: RefObject<HTMLElement>;

  // Override capability detection. Defaults: respect prefers-reduced-motion;
  // render a still fallback when WebGL2 is unavailable or context is lost.
  motion?: boolean;
};
```

- The component owns an absolutely positioned canvas host inside the hero container, behind the text via `z-index`.
- Lifecycle: dynamic-import the Three scene after `load` → capability + reduced-motion check → mount `<FirstLightCanvas>` → create `WebGLRenderer` + `ShaderMaterial` → first frame on the next RAF tick → cleanup by disposing material, geometry, renderer, and DOM canvas.
- Pause on `document.visibilitychange === 'hidden'`. Resume on visible.
- IntersectionObserver: pause when the hero leaves the viewport (long-scroll case).
- `<canvas aria-hidden="true">`. Skip-to-main link unaffected. The text is the SEO and screen-reader source of truth; the canvas adds nothing semantic.

## 9. Performance budget

The right metric for this feature is critical-path impact, not raw chunk bytes. The chunk is async, post-`load`, gated on capability + reduced-motion checks; its size competes with no user-facing budget.

- **TTI / LCP / FID impact: zero.** The feature module is dynamically imported only after `load` and only if the capability check passes. Nothing on the critical path references it.
- **Async chunk footprint: ~130KB gzipped** for the current Three scene chunk. Treated as cosmetic. Optimize only if telemetry shows real-device frame time or load behavior regressing from baseline.
- **Shader compile: <50ms** on baseline hardware (M1, mid-range Android).
- **Frame time: <4ms on M1 / iPhone 12, <12ms on baseline Android.** On battery <50% (where `navigator.getBattery()` is available), cap RAF at 30fps.
- **Lighthouse mobile performance**: regression budget per §16.4 (no more than 4 points vs. baseline). This is the actual user-impact gate.

## 10. Accessibility

- `prefers-reduced-motion: reduce` → render the CSS still frame instead of the live shader. No animation, no shader compile.
- WCAG 2.3.1 (no flashes >3 Hz): satisfied by design.
- WCAG 2.3.3 (motion-actuated animations): the arrival is auto-played; reduced-motion users get the still. Compliant.
- The text content is unchanged. The marked span's `--firstlight-luminance` modulation is disabled under reduced-motion.

## 11. Telemetry

Three new spans, all under the existing `company.*` namespace, emitted from the component:

- `company.first_light.capability` — one-shot, on mount. Attrs: `renderer_backend` ∈ `{webgl2, none}`, `prefers_reduced_motion`, `device_pixel_ratio`, `viewport.{w,h}`.
- `company.first_light.arrival_complete` — one-shot, ~2.8s after mount. Attrs: `frame_time.p50`, `frame_time.p99` for the arrival phase. Skipped under reduced-motion.
- `company.first_light.degraded` — one-shot, on fallback. Attrs: `reason` ∈ `{reduced_motion, no_renderer, renderer_init_failed, compile_error, context_lost}`, with the compile log truncated to 256 chars when applicable.

Querying these in ClickHouse answers: how often does the effect actually render, what is real-device frame time, how often do we fall back?

## 12. Fallbacks

- WebGL2 unavailable → render the CSS still, emit `degraded` with `reason=no_renderer`.
- Renderer construction throws → fall back to CSS still, emit `degraded` with `reason=renderer_init_failed` and the error name.
- Shader compile fails → fall back to CSS still, `reason=compile_error` with the error truncated to 256 chars.
- Context lost → dispose the renderer and fall back to CSS still with `reason=context_lost`.

## 13. Phasing

1. **`<FirstLight>` component.** Direct Three.js + modular GLSL. Wire into `_workshop/index.tsx` only. Telemetry on day one.
2. **Reduced-motion still.** CSS fallback first; replace with a baked PNG only when visual review needs exact parity.
3. **Wings geometry refactor.** Lift `WINGS_PATH_D` into `@verself/brand` before any shader pass redraws the wings.
4. **MSDF bake pipeline.** Add only after the SVG-lighting pass no longer gives enough control.

## 14. Content shape change

The trail target is currently the word _succeed_ in `landing.hero`. The hero remains a single editorial string. The route splits that string around the configured trail word, wraps the word in a measured span, and fails fast if the configured word is missing. The `<h1>` carries `aria-label={landing.hero}` so assistive technology receives the exact editorial copy.

## 15. Open questions

1. **Anchor-point geometry — exact pixel offset.** Default: ~12–18% outside the upper-right exterior of `WingsArgent`'s bounding box, ~10% above its vertical midpoint. Rationale: the arrival enters from upper-right, traverses the headline, draws back to the same upper-right region — one continuous arc. Anisotropic specular reads naturally with the key light at this position. Designer tunes the exact pixels.
2. **Resize during arrival.** Default: re-measure target uniforms via `ResizeObserver` and continue from the current position; only snap to settled if either anchor has moved >25% of viewport width since arrival began (covers the orientation-flip case). Light's current position never teleports.
3. **Should the light persist on every visit?** Default: every visit, with same-tab SPA-navigation suppression. `sessionStorage` flag set after `arrival_complete`; cleared after 30 minutes. New tab / new session / >30min lapse replays. Same-tab return within 30 min mounts directly into the settled state.
4. **LUT-driven grading vs. shader math.** A `.cube` LUT pass after bloom can deliver the warm-amber peak / cool-blue periphery as art-directable color shaping rather than fixed shader code. Ship without one first; add a postprocessing pass later if visual review needs finer control over the look.

## 16. Acceptance

The feature is done when, on the production landing route:

1. A fresh Chrome session shows the arrival sequence within 3.5s of navigation, settling into the orbit state.
2. ClickHouse `company.first_light.arrival_complete` returns rows from at least three distinct user-agent classes within 24h of release.
3. `company.first_light.degraded` rate is below 5% of `company.first_light.capability` rows.
4. Lighthouse mobile performance score on the landing route does not regress more than 4 points from pre-release baseline.
5. Visiting with `prefers-reduced-motion: reduce` shows the still frame, no canvas mounted, no console warnings.
6. Tab-switching pauses the RAF loop (verifiable in DevTools Performance panel).

Pixel-perfect motion review is a designer sign-off. The acceptance criteria above are necessary; designer approval of the rendered moment is sufficient.
