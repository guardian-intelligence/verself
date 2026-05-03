uniform float uTime;
uniform float uActive;
uniform vec2 uResolution;
uniform vec4 uTrailRect;
uniform vec4 uWingsRect;

in vec2 vUv;
out vec4 firstLightColor;

#include "lib/noise.glsl"
#include "lib/color.glsl"
#include "lib/easing.glsl"
#include "lib/geometry.glsl"
#include "lib/motion.glsl"

void main() {
  vec2 uv = vec2(vUv.x, 1.0 - vUv.y);
  float ms = uTime * 1000.0;
  float aspect = max(uResolution.x / max(uResolution.y, 1.0), 0.2);
  vec2 source = sourcePosition(ms, uTrailRect, uWingsRect);
  float intensity = sourceIntensity(ms) * uActive;

  vec2 fromSource = uv - source;
  vec2 anisotropic = rotate2d(-0.72) * vec2(fromSource.x * aspect, fromSource.y);
  float liquidLens = exp(-dot(anisotropic / vec2(0.115, 0.34), anisotropic / vec2(0.115, 0.34)));
  float core = exp(-dot(anisotropic / vec2(0.035, 0.11), anisotropic / vec2(0.035, 0.11)));
  float bloom = exp(-length(vec2(fromSource.x * aspect, fromSource.y)) * 12.0);

  float diagonal = abs(fromSource.x * aspect * 0.72 + fromSource.y * 1.12);
  float longStreak = exp(-diagonal * 58.0) * exp(-abs(fromSource.x * aspect - fromSource.y * 0.65) * 0.72);
  float comb = pow(1.0 - abs(sin((uv.x * aspect + uv.y * 1.18 - uTime * 0.18) * 28.0)), 8.0);
  float streaks = longStreak * (0.34 + 0.44 * comb);

  vec2 causticUv = rotate2d(0.42) * (uv * vec2(aspect, 1.0) * 8.0 + vec2(uTime * 0.23, -uTime * 0.08));
  float caustic = firstlight_fbm(causticUv);
  float grain = firstlight_hash(floor(uv * uResolution.xy * 0.85) + floor(uTime * 18.0));
  float edge = smoothstep(0.2, 0.82, liquidLens) * (1.0 - core);

  float trailMask = roundedRectMask(uv, uTrailRect + vec4(-0.03, -0.04, 0.06, 0.08), 0.08);
  float wingMask = roundedRectMask(uv, uWingsRect + vec4(-0.04, -0.04, 0.08, 0.08), 0.11);

  float light = core * 0.82 + bloom * 0.18 + liquidLens * (0.2 + caustic * 0.18) + streaks * 0.46;
  light += trailMask * streaks * 0.24;
  light += wingMask * liquidLens * 0.22;
  light *= intensity;
  light *= 0.94 + grain * 0.08;

  vec3 color = firstlight_spectral(edge, caustic);
  color += firstlight_cool() * edge * 0.08 * intensity;
  color += firstlight_amber() * core * 0.18 * intensity;

  float alpha = firstlight_saturate(light * 1.28);
  firstLightColor = vec4(color * light, alpha);
}
