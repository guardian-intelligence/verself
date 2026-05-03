uniform float uTime;
uniform float uActive;
uniform vec2 uResolution;
uniform float uDpr;
uniform float uAspect;
uniform float uMotionScale;
uniform float uSeed;

in vec2 vUv;
out vec4 firstLightColor;

#include "lib/noise.glsl"
#include "lib/camera.glsl"
#include "lib/plate.glsl"
#include "lib/meniscus.glsl"
#include "lib/bands.glsl"
#include "lib/caustics.glsl"
#include "lib/flare.glsl"
#include "lib/aberration.glsl"
#include "lib/tone.glsl"

void main() {
  vec2 uv = vec2(vUv.x, 1.0 - vUv.y);
  float t = uTime * uMotionScale + uSeed * 19.0;
  float aspect = max(uAspect, 0.2);
  OpticalFrame frame = firstlight_frame(uv, aspect, t);

  float rim = 0.0;
  float body = 0.0;
  float bend = 0.0;
  firstlight_meniscus(frame, rim, body, bend);

  float bandMask = 0.0;
  float bandEdge = 0.0;
  vec3 color = firstlight_plate(frame, t);
  vec3 bands = firstlight_bands(frame, t, bend, bandMask, bandEdge);
  float caustic = firstlight_caustics(frame, bend + bandMask, t);
  vec3 flare = firstlight_flare(frame, rim, bandEdge, caustic, t);

  vec3 refracted = bands * (0.75 + caustic * 0.55);
  refracted += vec3(0.055, 0.13, 0.155) * body * (0.36 + caustic * 0.5);
  refracted += vec3(0.45, 0.76, 0.9) * rim * 0.34;
  color += refracted;
  color += flare;
  color = firstlight_aberrate(color, bandEdge, rim, bandMask);
  color = firstlight_tone(color);
  color += firstlight_grain(uv, uResolution.xy / max(uDpr, 1.0), t);

  float alpha = clamp(uActive, 0.0, 1.0);
  firstLightColor = vec4(color * alpha, alpha);
}
