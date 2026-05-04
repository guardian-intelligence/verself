# First Light — celestial optical compositor

First Light is the full-viewport celestial optical compositor behind the
Guardian landing route. The base plate still follows the Liquid Series Seed
reference: near-black film stock, diagonal glass bands, a large offscreen
refractive meniscus, amber-white specular bloom, cool chromatic edge separation,
caustic interior motion, and heavy film grain. A typed celestial profile now
adds an art-directed Gargantua-style object: signed-distance shape, lens field,
photon-ring mask, and accretion-disk emission.

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

`<FirstLight motion profile />` has no DOM measurement inputs. The shader is a
viewport-space optical composition, not a light attached to copy, logo, or wing
geometry. React passes one typed `CelestialRenderProfile`; Three flattens it
into uniforms. The live geometry data remains:

- `uResolution`
- `uDpr`
- `uAspect`
- `uTime`
- `uActive`

The profile controls:

- `shape`: SDF center, radius, aspect, rotation, superellipse exponent, pinch,
  ripple, and edge softness.
- `lens`: deflection strength, radius, falloff, ring width, and ring intensity.
- `disk`: tilt, inner/outer radius, intensity, warmth, spin, and anisotropy.
- `uMotionScale`
- `uSeed`

## Composition

The fragment shader is organized as a small graph:

1. `camera.glsl` constructs aspect-correct optical coordinates.
2. `shape.glsl` evaluates the arbitrary celestial SDF and derived rim/normal.
3. `lens.glsl` converts the SDF into a bounded coordinate deflection field.
4. The existing plate modules render the distorted source plate.
5. `disk.glsl` renders art-directed accretion emission around the object.
6. `composite.glsl` applies debug modes or final shadow/ring/disk compositing.
7. `tone.glsl` owns exposure, film grain, and dither.

The current shape primitive is a deformable superellipse. Additional shapes
should enter through the SDF contract rather than by branching React or adding
DOM measurement.

## Debug Modes

`?visual-test=1` renders the final composite with preserved drawing buffer for
pixel assertions. These deterministic debug channels are available for e2e and
visual review:

- `?visual-test=shape`: shape SDF, normal color, rim, and black occlusion.
- `?visual-test=lens`: deflection direction, strength, and photon ring.
- `?visual-test=disk`: accretion disk emission plus the shape shadow.

## Shader Modules

```
apps/company/src/features/first-light/
  FirstLight.tsx                     -- capability gate, fallback, lazy canvas
  profile.ts                         -- typed celestial profile and debug mode
  types.ts                           -- renderer/profile data contracts
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
      shape.glsl                     -- arbitrary celestial SDF field
      lens.glsl                      -- SDF-derived deflection/ring field
      disk.glsl                      -- art-directed accretion disk emission
      plate.glsl                     -- dark stock, vignette, low-frequency haze
      meniscus.glsl                  -- offscreen curved refractive body
      bands.glsl                     -- diagonal glass slabs and hairline edges
      flare.glsl                     -- amber-white core and diagonal streaks
      composite.glsl                 -- debug modes, shadow, ring, final blend
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
3. E2E debug assertions prove the shape, lens, and disk channels are nonblank
   and carry the expected dark occlusion / color-spread / warm-emission
   signals.
4. Reduced-motion renders the still fallback with no canvas.
5. `company:dev_update`, `company:dev_check`, `vp check`, `vp test`,
   `vp build`, and `company:node_app_nomad_artifact` pass.
