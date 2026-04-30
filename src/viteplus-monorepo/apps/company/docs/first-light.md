# First Light — landing hero

A liquid light source that arrives on the Guardian landing page (`guardianintelligence.org/`), passes through the headline along the word *succeed*, draws back, and settles in a glacial orbit beside the wings. Landing route only. Designers and frontend engineers collaborate from this brief.

## 1. Intent

A single moment, at first-paint settle, where the page goes from dim Argent to lit Argent. The light enters from off-canvas, crosses the headline, draws back, and anchors near the wings, where it remains in a barely-perceptible orbit. The wings are *revealed* by the light, not its source.

We evoke: protectorship, benevolence, sharing prosperity, helping those without anyone else.

We never evoke: descent, ascension, salvation, or the wings emitting light. The word *angel* is banned in copy and the metaphor of one is banned in pixels. The wings receive light and reflect it. Custodial, not divine.

## 2. References

- **Liquid Series Seed reel** (image attached to the brief) — celestial body, rim light across curvature, lens flare, heavy grain. We take the *frame* and the *grain density*; we are not putting a planet on the page.
- **Alan Zucconi — *Improving the Rainbow*** (https://www.alanzucconi.com/2017/07/15/improving-the-rainbow-2/). Canonical wavelength→RGB approximation. Source for the trace of cool-shifted dispersion in the falloff; we use a six-bump physical approximation rather than RGB-channel offsets.
- **Julia Poo — *More accurate Iridescence*** (https://www.shadertoy.com/view/ltKcWh). Thin-film interference from first principles. Source for the brushed-metal iridescence on the wings during arrival.
- **Inigo Quilez — *Articles*** (https://iquilezles.org/articles/). Reference for gradient noise, fbm, easings, and palette construction used inside the bloom region.
- **lygia** (https://github.com/patriciogonzalezvivo/lygia). Curated GLSL/WGSL primitive library. Vendor-pin specific files we use; do not depend as a package.

## 3. Banned moves

- Strobe or flash. The arrival has no peak above ~80% relative luminance and no instantaneous intensity transitions.
- Continuous swirl. After settle, motion is below 1° per second, perceptible only to a viewer who lingers ten seconds.
- Wings emitting light, glowing from within, or appearing tinted by anything but reflected Argent.
- Modal or blocking behavior on first paint. Text and wings render on the first frame; the canvas mounts after.
- Visible "loading the effect" UI.

## 4. Sequence

| t | event |
|---|---|
| 0 ms | Route renders. Text + Argent wings present, flat-lit. Page-level `FilmGrain` already on. |
| 0–700 ms | Hold. Reader's eye lands on the kicker and headline. |
| ~700 ms | WebGL canvas mounted, shader compiled, MSDF loaded, first frame buffered. |
| 700 ms | Light source enters viewport from upper-right, off-canvas origin. |
| 700–1300 ms | **Arrival** — high intensity, high velocity, eased out. A caustic trail follows the source. |
| 1300–2100 ms | **Trail** — light traverses the bounding box of the marked headline span (default: *succeed*), elongating along its baseline, intensifying as it crosses the word's centroid. |
| 2100–2800 ms | **Draw-back** — light contracts toward the anchor near the wings; intensity falls to ~30% of arrival peak. |
| 2800 ms+ | **Settled** — anchor oscillates within a ~24px radius on a Lissajous-like curve at <1°/s. Anisotropic shimmer on wings. Caustic grain inside the lit region only. Permanent state, no further events. |

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
- Argent shimmer on wings: existing Argent token, modulated up to +20% luminance where the light sits. Never *below* base Argent.
- Background ink: unchanged (`var(--color-ink)`, `#0E0E0E`). The light does not raise the global luminance of the page.

### Light model

- Anisotropic specular on the wings, reading as brushed metal under a moving source. Direction is computed **per-sample** from the MSDF gradient at that sample point (see §7) so the shimmer reads as brushed (varying along the surface) rather than frosted (uniform).
- Soft volumetric bloom on the source, ~6% viewport diameter at peak.
- Caustic noise (fbm, ~3 octaves) modulates the source's interior, never its outline.
- No hard rim line. The source has no edge — only falloff.

### Type interaction

- The marked span (default: the word *succeed*) receives the trail.
- Shader reads the span's bounding box at mount and on resize, passes it as a uniform.
- During trail phase, the letters of the marked span briefly increase in luminance via a CSS variable `--firstlight-luminance` driven by a `requestAnimationFrame` callback that mirrors the shader's intensity envelope. **Type does not move.**

### Wing interaction

- Wings render through the WebGL canvas during the entire arrival sequence.
- Anisotropic shimmer is computed from the MSDF gradient and the light direction.
- When the light is fully off (e.g., after a context loss fallback), the wings render pixel-identical to the SVG version.

### Grain

- Page-level `<FilmGrain intensity={0.22}>` continues unchanged.
- Inside the lit region, an additional shader-internal caustic grain runs at ~0.08 intensity — lower than the page grain so the light never reads noisier than its surroundings.

## 6. Renderer choice

**react-three-fiber v9 with TSL (Three.js Shading Language). One shader source, two backends, selected at runtime.**

### Stack

- `three` — pinned to the latest stable; `WebGPURenderer` is default-stable since r170.
- `@react-three/fiber` v9 — small core, tree-shakes aggressively.
- `@react-three/drei` — utility imports only (`useFBO`, helpers as needed). No scene presets.
- `@react-three/postprocessing` — bloom, optional LUT pass.
- TSL graphs composed as ES modules under `tsl/`. One TypeScript-flavored function per visual primitive; the renderer lowers each to GLSL or WGSL at canvas creation.

### Why not raw GLSL + a thin React wrapper

Two earlier arguments against r3f no longer hold:

1. **Bundle size.** The feature module (three + r3f + drei + postprocessing + the TSL graphs) is dynamically imported behind a `prefers-reduced-motion` and renderer-capability check that runs after `load`. The chunk is post-TTI, post-LCP, post-FID. Bundle bytes for a non-critical-path async chunk are cosmetic — they compete with no user-facing budget.
2. **Parallel WGSL + GLSL sources.** TSL eliminates the duplication. The functions in `tsl/` (`dispersion.ts`, `caustic.ts`, `aniso.ts`, `envelopes.ts`) are TypeScript shader graphs, and the renderer lowers each to whichever backend it constructed. The "Phase 5 WebGPU port" plan in earlier drafts dissolves entirely; backend selection is a runtime detail of the same code path.

Raw GLSL also forfeits a composable post-processing chain. The bloom and optional LUT pass are meaningfully cleaner as `<EffectComposer>` children than as hand-rolled FBO ping-pong.

### Architectural invariant — preserved across the stack change

One file per visual primitive, each exporting one TSL function. The composition file (`scene/LightArrival.tsx`) imports them and assembles the final `MeshBasicNodeMaterial`. No god-shader. No inline noise. The rule from earlier drafts holds; the language is now TSL rather than GLSL.

```
apps/company/src/features/first-light/
  FirstLight.tsx                     — entry: capability check, lazy mount of <Canvas>
  use-first-light.ts                 — telemetry, lifecycle, settled-state suppression
  scene/
    LightArrival.tsx                 — r3f scene, composes TSL nodes into the material
    Wings.tsx                        — MSDF-sampled wings with anisotropic shimmer
    Bloom.tsx                        — postprocessing config
  tsl/
    dispersion.ts                    — wavelength→RGB, cool-shifted falloff (Zucconi)
    iridescence.ts                   — thin-film interference (Poo)
    aniso.ts                         — anisotropic specular from MSDF gradient
    caustic.ts                       — fbm-modulated interior grain
    bloom.ts                         — soft volumetric falloff
    envelopes/
      arrival.ts                     — easeOutQuint position envelope
      trail.ts                       — bbox-traversal intensity envelope
      drawback.ts                    — easeInOutCubic contraction
      settled.ts                     — Lissajous orbit
  assets/
    wings.msdf.png                   — generated by the §7 Bazel rule
```

### What we don't take from precedent

State of the art has advanced since earlier WebGL-on-marketing-pages reference points. We do not borrow any third party's scene-graph structure, postprocessing config, lensflare-textures-as-procedural-bloom approach, or ray-bouncing primitives. We borrow only the public articles cited in §2.

## 7. Wing geometry — single source of truth

The wings live in `@verself/brand` as the `WingsArgent` SVG component. The path data is currently embedded in JSX. Lift it before any WebGL work begins:

```ts
// packages/brand/src/wings.geometry.ts
export const WINGS_PATH_D = "M…";
export const WINGS_VIEWBOX = { w: 320, h: 200 } as const;
```

`WingsArgent` (existing component, used everywhere in the repo) renders from this constant. The landing's `<FirstLight>` consumes it via a baked **multi-channel signed distance field** (MSDF):

- Build step: a Bazel rule runs `msdfgen` over `WINGS_PATH_D` at the canonical viewBox. `msdfgen` is the industry standard for resolution-independent vector-on-GPU rendering and is used in every modern font atlas pipeline.
- Output: `wings.msdf.png` (~16KB), shipped as a generated static asset under `apps/company/public/`. Not committed.
- The fragment shader samples the MSDF and computes the gradient at the sample point for anisotropic shimmer direction.

Two render targets, one geometry. Wings stay pixel-identical between SVG and WebGL.

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
  // render still PNG when WebGL2 is unavailable or context is lost.
  motion?: boolean;
};
```

- The component owns an r3f `<Canvas>`, positioned absolute inside the hero container, behind the text via `z-index`. Renderer (`WebGPURenderer` or `WebGLRenderer`) is selected by three.js at canvas creation based on browser capabilities.
- Lifecycle: dynamic-import the feature module → capability + reduced-motion check → mount `<Canvas>` → r3f's `onCreated` triggers MSDF + LUT loads → first frame on the next RAF tick → cleanup via component unmount.
- Pause on `document.visibilitychange === 'hidden'`. Resume on visible.
- IntersectionObserver: pause when the hero leaves the viewport (long-scroll case).
- `<canvas aria-hidden="true">`. Skip-to-main link unaffected. The text is the SEO and screen-reader source of truth; the canvas adds nothing semantic.

## 9. Performance budget

The right metric for this feature is critical-path impact, not raw chunk bytes. The chunk is async, post-`load`, gated on capability + reduced-motion checks; its size competes with no user-facing budget.

- **TTI / LCP / FID impact: zero.** The feature module is dynamically imported only after `load` and only if the capability check passes. Nothing on the critical path references it.
- **Async chunk footprint: ~200–300KB gzipped** (three + r3f + drei + postprocessing + TSL graphs). Treated as cosmetic. Optimize only if telemetry shows real-device frame time or load behavior regressing from baseline.
- **MSDF asset: ~16KB PNG**, lazy-loaded with the chunk, served from the same origin.
- **Shader compile: <50ms** on baseline hardware (M1, mid-range Android). TSL adds negligible overhead vs. raw GLSL/WGSL.
- **Frame time: <4ms on M1 / iPhone 12, <12ms on baseline Android.** On battery <50% (where `navigator.getBattery()` is available), cap RAF at 30fps.
- **Lighthouse mobile performance**: regression budget per §16.4 (no more than 4 points vs. baseline). This is the actual user-impact gate.

## 10. Accessibility

- `prefers-reduced-motion: reduce` → render the **baked still frame** (a PNG of the settled orbit at t=3s) instead of the live shader. No animation, no shader compile.
- WCAG 2.3.1 (no flashes >3 Hz): satisfied by design.
- WCAG 2.3.3 (motion-actuated animations): the arrival is auto-played; reduced-motion users get the still. Compliant.
- The text content is unchanged. The marked span's `--firstlight-luminance` modulation is disabled under reduced-motion.

## 11. Telemetry

Three new spans, all under the existing `company.*` namespace, emitted from the component:

- `company.first_light.capability` — one-shot, on mount. Attrs: `renderer_backend` ∈ `{webgl2, webgpu, none}`, `prefers_reduced_motion`, `device_pixel_ratio`, `viewport.{w,h}`. Single chosen-backend attr because three.js makes the choice at construction time; recording booleans for "supported" separately is redundant.
- `company.first_light.arrival_complete` — one-shot, ~2.8s after mount. Attrs: `frame_time.p50`, `frame_time.p99` for the arrival phase. Skipped under reduced-motion.
- `company.first_light.degraded` — one-shot, on fallback. Attrs: `reason` ∈ `{no_webgl2, compile_error, context_lost}`, with the compile log truncated to 256 chars when applicable.

Querying these in ClickHouse answers: how often does the effect actually render, what is real-device frame time, how often do we fall back?

## 12. Fallbacks

- Neither WebGL2 nor WebGPU available → render the baked still PNG, emit `degraded` with `reason=no_renderer`.
- Renderer construction throws → fall back to PNG, emit `degraded` with `reason=renderer_init_failed` and the error name.
- Shader (TSL) lowering or compile fails → fall back to PNG, `reason=compile_error` with the error truncated to 256 chars.
- Context lost → r3f's `gl.domElement` recovery path re-attempts once, then falls back to PNG with `reason=context_lost`.
- The fallback PNG is the same baked still used for `prefers-reduced-motion`. One asset, multiple roles.

## 13. Phasing

1. **Wings geometry refactor** (single small PR, no behavior change). Lift `WINGS_PATH_D` into `@verself/brand`. Ship and verify the SVG looks identical across every consumer.
2. **MSDF bake pipeline** (Bazel rule, generated asset).
3. **`<FirstLight>` component.** r3f + TSL. Wire into `_workshop/index.tsx` only. Telemetry on day one. Renderer (WebGL2 / WebGPU) selected at runtime by three.js — not a separate phase.
4. **Reduced-motion still + fallback PNG** baked from the canvas output at t=3s, committed to `public/`.

All four phases are MVP. There is no separate WebGPU phase; TSL ships both backends with Phase 3.

## 14. Content shape change

The trail target is currently the word *succeed* in `landing.hero`. The hero is a single string today; routes consume it as one. To mark the span without touching JSX, restructure `landing.ts`:

```ts
export const landing = {
  kicker: "Seattle, Washington",
  hero: {
    before: "The world needs your business to ",
    trail: "succeed",
    after: ". We're here to help.",
  },
  // …
};
```

The route renders `<h1>{hero.before}<span data-firstlight="trail">{hero.trail}</span>{hero.after}</h1>`. The `<FirstLight>` component selects the marked span via `[data-firstlight="trail"]` to obtain the trail target ref.

Keeps the marker out of voice (`landing.ts` stays editor-friendly), out of JSX (route stays structural), and lets the brief's defaults match the current copy.

## 15. Open questions

1. **Anchor-point geometry — exact pixel offset.** Default: ~12–18% outside the upper-right exterior of `WingsArgent`'s bounding box, ~10% above its vertical midpoint. Rationale: the arrival enters from upper-right, traverses the headline, draws back to the same upper-right region — one continuous arc. Anisotropic specular reads naturally with the key light at this position. Designer tunes the exact pixels.
2. **Resize during arrival.** Default: re-measure target uniforms via `ResizeObserver` and continue from the current position; only snap to settled if either anchor has moved >25% of viewport width since arrival began (covers the orientation-flip case). Light's current position never teleports.
3. **Should the light persist on every visit?** Default: every visit, with same-tab SPA-navigation suppression. `sessionStorage` flag set after `arrival_complete`; cleared after 30 minutes. New tab / new session / >30min lapse replays. Same-tab return within 30 min mounts directly into the settled state.
4. **LUT-driven grading vs. shader math.** A `.cube` LUT pass after bloom can deliver the warm-amber peak / cool-blue periphery as art-directable color shaping rather than fixed shader code. Designer-optional: ship without first; add a LUT later if the designer wants finer control over the look. The hook in `Bloom.tsx` is one component prop away.

## 16. Acceptance

The feature is done when, on the production landing route:

1. A fresh Chrome session shows the arrival sequence within 3.5s of navigation, settling into the orbit state.
2. ClickHouse `company.first_light.arrival_complete` returns rows from at least three distinct user-agent classes within 24h of release.
3. `company.first_light.degraded` rate is below 5% of `company.first_light.capability` rows.
4. Lighthouse mobile performance score on the landing route does not regress more than 4 points from pre-release baseline.
5. Visiting with `prefers-reduced-motion: reduce` shows the still frame, no canvas mounted, no console warnings.
6. Tab-switching pauses the RAF loop (verifiable in DevTools Performance panel).

Pixel-perfect motion review is a designer sign-off. The acceptance criteria above are necessary; designer approval of the rendered moment is sufficient.
