# Dot-matrix wave field design

The dot matrix is a display surface over a 2.5-D height-field simulation. The
wave system does not author visible wave animations directly. It stores scalar
wave state in an offscreen texture, updates that state with a finite-difference
wave equation, then lets the display shader sample the result once per dot.

Conceptually there are two texture layers:

```text
click input + ambient wave emitters
          |
          v
dynamic wave state texture
R current height, G previous height, B velocity/energy, A reserved
          |
          v
dot-matrix display shader
samples height and velocity at each dot

logical scene model
          |
          v
scene field texture
R visibility, G spawn, B absorption, A readability
          |
          v
modulates impulse placement, propagation, damping, and final dot visibility
```

The wave texture is temporal state. The scene field texture is spatial intent.
The simulation moves energy through the field; the renderer decides how that
energy is visible through the dot matrix.

## Frame pipeline

```text
1. Collect sources
   click impulses + ambient wave fronts

2. Step simulation
   previous wave state + impulses + absorption field
        -> fullscreen compute pass
        -> next wave state texture

3. Render display
   wave state + scene fields + dot grid
        -> per-dot radius, luminance, alpha, color
```

The simulation pass is a fullscreen shader over a low-resolution floating-point
render target. Each texel reads its own previous state and its nearest
neighbors:

```glsl
float h = state.r;
float hp = state.g;

float lap =
  north.r + south.r + east.r + west.r - 4.0 * h;

float hNext =
  2.0 * h
  - hp
  + waveSpeed2 * lap * dt * dt
  + impulse;

hNext *= damping;

outState = vec4(hNext, h, abs(hNext - h), 1.0);
```

The display shader does not know the simulation math. It samples the resulting
height field at the center of each dot cell:

```glsl
vec4 wave = texture(uWaveState, worldUv);
float height = wave.r;
float velocity = wave.r - wave.g;
float energy = abs(height) * heightGain + abs(velocity) * velocityGain;
```

That energy controls the dot radius, luminance, color temperature, and alpha.

## Scene fields

Scene fields are normalized scalar masks over the simulation domain. They are
continuous values in `[0, 1]`, not hard booleans. Feathered boundaries are part
of the model because discontinuous masks create visual popping and sharp
simulation artifacts.

| Field         | Meaning                                                                                   | Primary consumer                  | Effect                                                                                                                     |
| ------------- | ----------------------------------------------------------------------------------------- | --------------------------------- | -------------------------------------------------------------------------------------------------------------------------- |
| `visibility`  | Where the dot field should visually exist. This is the stage or lens for the composition. | Display shader, ambient scheduler | Raises baseline dot presence and biases ambient waves toward visible paths.                                                |
| `spawn`       | Where autonomous ambient waves are allowed to originate.                                  | Ambient scheduler                 | Keeps waves entering from intentional zones, usually edge halos, instead of appearing uniformly across the screen.         |
| `absorption`  | Where wave energy should decay faster.                                                    | Simulation shader                 | Creates open edges and quiet zones by increasing damping.                                                                  |
| `readability` | Where foreground content must remain legible.                                             | Display shader                    | Attenuates dot radius, luminance, and alpha around text/navigation without necessarily deleting the underlying wave state. |

These masks layer rather than replace one another. For example, a point can be
high visibility, high readability, and low absorption: the wave may pass through
the area, but the final dots are subdued so text remains clear. A boundary band
can be high spawn and high absorption: waves can originate near the edge while
old energy is also drained before it reflects.

The usual display combination is:

```glsl
vec4 fields = texture(uFieldState, worldUv);

float visibility = fields.r;
float readability = fields.a;
float readableProtection = 1.0 - readability;

float visibleEnergy = energy * visibility * readableProtection;
float radius = baseRadius + visibleEnergy * radiusGain;
float alpha = baseAlpha * visibility * readableProtection + visibleEnergy * alphaGain;
```

Absorption is applied in the simulation pass instead:

```glsl
float absorption = texture(uFieldState, uv).b;
float localDamping = mix(globalDamping, absorbingDamping, absorption);
hNext *= localDamping;
```

Spawn is normally consumed before the GPU step. The ambient scheduler samples
candidate origins from the spawn field, scores the projected path through
visibility/readability, then emits a front-shaped impulse into the simulation.

## Sources

User interaction is click-only. A click becomes a localized point impulse in
the simulation domain:

```text
screen click -> camera-space uv -> wave impulse { x, y, radius, strength }
```

Ambient motion is generated by scheduled impulses. These should be cohesive
fronts, not random pixel noise:

```text
seeded random scheduler
  -> choose spawn-field origin
  -> point direction toward the visible camera area
  -> emit elongated front impulse
```

Randomness belongs in source scheduling, source placement, source strength, and
front length. The propagation equation should stay deterministic once the
impulses are injected. That gives the scene non-repeating motion while keeping
the waves coherent.

## Timing

Simulation speed is expressed in simulation seconds, not rendered frames. The
render loop measures wall-clock frame delta, advances a fixed-timestep
accumulator, and may execute zero or more simulation steps before drawing.

```text
requestAnimationFrame delta
        |
        v
fixed timestep accumulator
        |
        v
N simulation steps at constant dt
        |
        v
render latest wave texture
```

This keeps propagation speed stable across 60 Hz, 120 Hz, background throttling,
and brief frame stalls. Long catch-up windows are capped; the simulation should
drop excess accumulated time rather than perform unbounded work on a slow
frame.

The important speed levers are:

- `fixedStepSeconds`: the numerical time quantum used by the simulation.
- `travelSpeedScale`: maps wall-clock seconds to simulation seconds.
- `waveSpeed`: the finite-difference propagation coefficient.
- `damping`: global energy decay.
- `absorption`: spatially varying extra damping.

Changing display brightness, radius gain, or contrast should not change wave
travel speed. Changing frame rate should not change wave travel speed.

## Stability

The simulation grid is intentionally smaller than the canvas and independent of
device pixels. Typical useful sizes are around `128x72`, `160x90`, or
`192x108`, adjusted for viewport aspect ratio. The dot grid samples this field
smoothly; the simulation should not run at full canvas resolution.

The wave-speed coefficient must remain conservative for the chosen timestep and
grid size. If speed is raised, damping and timestep should be reviewed together.
Symptoms of instability include checkerboard artifacts, rapidly growing height
values, and full-screen flashing after an impulse.

Edges should absorb energy. Reflective boundaries make the hero feel like a
tank, and wrapping makes wave fronts reappear artificially. Absorption masks
near the simulation boundary create the intended open-field behavior.

## Design invariants

- Wave state stores simulation quantities, not colors or rendered pixels.
- Scene fields describe spatial intent, not animation curves.
- The simulation shader owns propagation and damping.
- The display shader owns dot radius, alpha, luminance, and color.
- Clicks create one-shot point impulses.
- Ambient waves spawn from masked regions and enter as coherent fronts.
- Readability masks protect foreground content in the final display layer.
- Simulation resolution is decoupled from canvas resolution and dot spacing.
- Propagation speed is controlled by simulation time and coefficients, not by
  `requestAnimationFrame` cadence.

## Reference architecture

This follows the standard GPU height-field pattern used by Three.js GPGPU
examples and older GPU water simulations: ping-pong floating-point render
targets hold current and previous height state, a fullscreen compute pass
updates every texel from neighboring texels, and the visible material samples
the current state texture.

Relevant references:

- Three.js `GPUComputationRenderer` documentation:
  https://threejs.org/docs/pages/GPUComputationRenderer.html
- Three.js GPGPU water example:
  https://github.com/mrdoob/three.js/blob/dev/examples/webgl_gpgpu_water.html
- NVIDIA GPU water simulation note:
  https://developer.download.nvidia.com/assets/gamedev/docs/HLSL_WaterVTF.pdf
