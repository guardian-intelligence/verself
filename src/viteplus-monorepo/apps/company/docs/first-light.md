# First Light — optical plate

First Light is the full-viewport optical plate behind the Guardian landing
route. It recreates the visual grammar of the Liquid Series Seed reference:
near-black film stock, diagonal glass bands, a large offscreen refractive
meniscus, amber-white specular bloom, cool chromatic edge separation, caustic
interior motion, and heavy film grain.

The landing body is intentionally blank while this visual system is being
rebuilt. The top navbar remains the only page chrome on the first viewport.

## Renderer

Direct Three.js WebGL2 with generated GLSL3 modules.

- React owns capability gating, reduced-motion fallback, pause/resume, and the
  canvas host.
- Three owns `WebGLRenderer`, `ShaderMaterial`, the fullscreen plane, render
  loop, and disposal.
- GLSL source lives under `shader-src/`. Bazel resolves `#include` composition
  and emits the ignored `shader/first-light.generated.ts` transport module.
- Three injects the GLSL3 version line through `glslVersion: GLSL3`; source
  files intentionally omit `#version`.

## Component Contract

`<FirstLight motion />` has no DOM measurement inputs. The shader is a
viewport-space optical composition, not a light attached to copy, logo, or wing
geometry. The only live geometry data is:

- `uResolution`
- `uDpr`
- `uAspect`
- `uTime`
- `uActive`
- `uMotionScale`
- `uSeed`

## Shader Modules

```
apps/company/src/features/first-light/
  FirstLight.tsx                     -- capability gate, fallback, lazy canvas
  use-first-light.ts                 -- runtime, viewport frame, visibility
  scene/
    FirstLightCanvas.tsx             -- Three renderer, uniforms, RAF, disposal
    metrics.ts                       -- frame-time summary
  shader/
    first-light.generated.ts         -- ignored generated TS transport module
  shader-src/
    first-light.vert                 -- fullscreen plane vertex shader
    first-light.frag                 -- final optical plate composition
    lib/
      camera.glsl                    -- aspect-correct UVs and diagonal basis
      plate.glsl                     -- dark stock, vignette, low-frequency haze
      meniscus.glsl                  -- offscreen curved refractive body
      bands.glsl                     -- diagonal glass slabs and hairline edges
      flare.glsl                     -- amber-white core and diagonal streaks
      aberration.glsl                -- cool/warm edge separation
      caustics.glsl                  -- elongated internal ridge noise
      noise.glsl                     -- hash, value noise, fbm, ridge primitives
      tone.glsl                      -- exposure curve, grain, dither
```

## Fallbacks

- `prefers-reduced-motion: reduce` renders a still optical plate and skips live
  shader animation.
- WebGL2 unavailable emits `company.first_light.degraded` with
  `reason=no_renderer`.
- Renderer construction, shader compile/render failure, or context loss emits
  `company.first_light.degraded` with the specific reason and swaps to the
  still plate.

## Verification

The deployable slice is valid when:

1. `/` renders the navbar, a blank body, no footer, and one canvas after load.
2. E2E pixel assertions prove the canvas is nonblank, diagonal energy dominates
   the counter-diagonal, and warm/cool channel split exists in bright regions.
3. Reduced-motion renders the still fallback with no canvas.
4. `company:dev_update`, `company:dev_check`, `vp check`, `vp test`,
   `vp build`, and `company:node_app_nomad_artifact` pass.
