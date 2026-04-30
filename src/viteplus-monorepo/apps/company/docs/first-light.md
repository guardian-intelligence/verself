# First Light — landing hero

A liquid light source that arrives on the Guardian landing page (`guardianintelligence.org/`), passes through the headline along the word *succeed*, draws back, and settles in a glacial orbit beside the wings. Landing route only. Designers and frontend engineers collaborate from this brief.

## 1. Intent

A single moment, at first-paint settle, where the page goes from dim Argent to lit Argent. The light enters from off-canvas, crosses the headline, draws back, and anchors near the wings, where it remains in a barely-perceptible orbit. The wings are *revealed* by the light, not its source.

We evoke: protectorship, benevolence, sharing prosperity, helping those without anyone else.

We never evoke: descent, ascension, salvation, or the wings emitting light. The word *angel* is banned in copy and the metaphor of one is banned in pixels. The wings receive light and reflect it. Custodial, not divine.

## 2. References

- **Anveio prism** (`https://www.anveio.com/`) — prior art for raw GLSL composed into a React tree. We borrow the integration pattern; the visual signature is gentler and asymmetric, not a chromatic-dispersion fan.
- **Liquid Series Seed reel** (image attached to the brief) — celestial body, rim light across curvature, lens flare, heavy grain. We take the *frame* and the *grain density*; we are not putting a planet on the page.

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
- Caustic ripples: derived from the source color via fbm noise. No saturated rainbow dispersion.
- Argent shimmer on wings: existing Argent token, modulated up to +20% luminance where the light sits. Never *below* base Argent.
- Background ink: unchanged (`var(--color-ink)`, `#0E0E0E`). The light does not raise the global luminance of the page.

### Light model

- Anisotropic specular on the wings, reading as brushed metal under a moving source. Direction comes from the wings' MSDF gradient (see §7).
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

**WebGL2 as the floor. WebGPU as a follow-up progressive enhancement after we have telemetry from real devices.**

### Why not Three.js / react-three-fiber

The scene is a single fullscreen quad with one fragment shader and (at most) two render passes. Three.js's scene graph, camera abstractions, geometry/material objects, and built-in lighting are all unused weight here — ~600KB gzipped that we cannot justify on a marketing landing whose budget is dominated by font payload.

### Why not gl-react

Stagnant; last meaningful release in 2021.

### Why hand-rolled GLSL + a thin React wrapper

Matches the precedent from anveio.com. For a single-shader effect, the React layer is a `useEffect` with mount/cleanup, a `<canvas ref>`, and shaders imported as raw strings via Vite's `?raw` suffix.

If the renderer ever grows past three passes, reach for **`regl`** (~50KB, functional WebGL2 wrapper) before reaching for Three.js.

### WebGPU plan

Phase 2: same shader logic ported to WGSL behind a capability detect. Identical visual output. We do **not** adopt TSL (Three.js Shading Language) — the shader is small enough that two parallel sources (`first-light.frag` + `first-light.wgsl`) are cheaper than a translation layer.

Phase 1 ships WebGL2-only. Confirm the phasing in §11.

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

- The component owns its own `<canvas>`, positioned absolute inside the hero container, behind the text via `z-index`.
- Lifecycle: `useEffect` → check WebGL2 → load shaders + MSDF → compile → start RAF loop → cleanup on unmount.
- Pause on `document.visibilitychange === 'hidden'`. Resume on visible.
- IntersectionObserver: pause when the hero leaves the viewport (long-scroll case).
- `<canvas aria-hidden="true">`. Skip-to-main link unaffected. The text is the SEO and screen-reader source of truth; the canvas adds nothing semantic.

## 9. Performance budget

- Initial JS payload added by the feature: **<12KB gzipped** (component + shaders inlined as strings, no Three.js).
- MSDF asset: ~16KB PNG, served from the same origin.
- Shader compile: <50ms on baseline hardware (M1, mid-range Android).
- Frame time: <4ms on M1 / iPhone 12, <12ms on baseline Android. If we are on battery and below 50%, cap RAF at 30fps via `navigator.getBattery()` where available.
- TTI impact: zero. The component is lazy-imported after first paint; no shader work blocks the main thread before `load`.

## 10. Accessibility

- `prefers-reduced-motion: reduce` → render the **baked still frame** (a PNG of the settled orbit at t=3s) instead of the live shader. No animation, no shader compile.
- WCAG 2.3.1 (no flashes >3 Hz): satisfied by design.
- WCAG 2.3.3 (motion-actuated animations): the arrival is auto-played; reduced-motion users get the still. Compliant.
- The text content is unchanged. The marked span's `--firstlight-luminance` modulation is disabled under reduced-motion.

## 11. Telemetry

Three new spans, all under the existing `company.*` namespace, emitted from the component:

- `company.first_light.capability` — one-shot, on mount. Attrs: `webgl2_supported`, `webgpu_supported`, `prefers_reduced_motion`, `device_pixel_ratio`, `viewport.{w,h}`.
- `company.first_light.arrival_complete` — one-shot, ~2.8s after mount. Attrs: `frame_time.p50`, `frame_time.p99` for the arrival phase. Skipped under reduced-motion.
- `company.first_light.degraded` — one-shot, on fallback. Attrs: `reason` ∈ `{no_webgl2, compile_error, context_lost}`, with the compile log truncated to 256 chars when applicable.

Querying these in ClickHouse answers: how often does the effect actually render, what is real-device frame time, how often do we fall back?

## 12. Fallbacks

- WebGL2 unavailable → render the baked still PNG, emit `degraded` with `reason=no_webgl2`.
- Shader compile fails → fall back to PNG, emit `degraded` with `reason=compile_error`.
- WebGL context lost → re-attempt once, then fall back to PNG with `reason=context_lost`.
- The fallback PNG is the same baked still used for `prefers-reduced-motion`. One asset, multiple roles.

## 13. Phasing

1. **Wings geometry refactor** (single small PR, no behavior change). Lift `WINGS_PATH_D` into `@verself/brand`. Ship and verify the SVG looks identical across every consumer.
2. **MSDF bake pipeline** (Bazel rule, generated asset).
3. **`<FirstLight>` component, WebGL2 only.** Wire into `_workshop/index.tsx` only. Telemetry on day one.
4. **Reduced-motion still + fallback PNG** baked from the WebGL output at t=3s, committed to `public/`.
5. **WebGPU port** — only after Phase 1's telemetry confirms frame-time issues on a meaningful slice of real visitors. Otherwise this is gold-plating.

Phases 1–4 are the MVP. Phase 5 is conditional.

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

1. **Anchor-point geometry.** Where does the light settle, exactly? Default proposal: the bounding-box midpoint of `WingsArgent`, offset ~8% rightward so the light catches the wing edge rather than centering on it. Designer can tune.
2. **Resize during arrival.** Edge case: viewport resize between t=0 and t=2.8s. Cheapest answer: cancel in-flight arrival, snap to settled. More elegant: re-measure and continue. Default proposal: snap.
3. **Should the light persist on every visit, or only on first session per device?** Default proposal: every visit. Repeat visitors are rare for a marketing landing; truncating the moment for them costs more than it saves.
4. **WebGPU phasing — confirm.** Ship WebGL2 alone in Phase 1; revisit after telemetry.

## 16. Acceptance

The feature is done when, on the production landing route:

1. A fresh Chrome session shows the arrival sequence within 3.5s of navigation, settling into the orbit state.
2. ClickHouse `company.first_light.arrival_complete` returns rows from at least three distinct user-agent classes within 24h of release.
3. `company.first_light.degraded` rate is below 5% of `company.first_light.capability` rows.
4. Lighthouse mobile performance score on the landing route does not regress more than 4 points from pre-release baseline.
5. Visiting with `prefers-reduced-motion: reduce` shows the still frame, no canvas mounted, no console warnings.
6. Tab-switching pauses the RAF loop (verifiable in DevTools Performance panel).

Pixel-perfect motion review is a designer sign-off. The acceptance criteria above are necessary; designer approval of the rendered moment is sufficient.
