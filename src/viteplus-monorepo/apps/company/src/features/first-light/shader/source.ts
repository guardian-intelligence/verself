import { glslColor } from "./color";
import { glslNoise } from "./noise";

export const firstLightVertexShader = /* glsl */ `
varying vec2 vUv;

void main() {
  vUv = uv;
  gl_Position = vec4(position.xy, 0.0, 1.0);
}
`;

export const firstLightFragmentShader = /* glsl */ `
precision highp float;

uniform float uTime;
uniform float uActive;
uniform vec2 uResolution;
uniform vec4 uTrailRect;
uniform vec4 uWingsRect;

varying vec2 vUv;

${glslNoise}
${glslColor}

float firstlight_saturate(float v) {
  return clamp(v, 0.0, 1.0);
}

float easeOutQuint(float t) {
  float p = 1.0 - firstlight_saturate(t);
  return 1.0 - p * p * p * p * p;
}

float easeInOutCubic(float t) {
  t = firstlight_saturate(t);
  return t < 0.5 ? 4.0 * t * t * t : 1.0 - pow(-2.0 * t + 2.0, 3.0) * 0.5;
}

mat2 rotate2d(float a) {
  float s = sin(a);
  float c = cos(a);
  return mat2(c, -s, s, c);
}

vec2 rectCenter(vec4 rect) {
  return rect.xy + rect.zw * 0.5;
}

vec2 sourcePosition(float ms, vec4 trailRect, vec4 wingsRect) {
  vec2 trailStart = vec2(trailRect.x - 0.14, trailRect.y + trailRect.w * 0.08);
  vec2 trailEnd = vec2(trailRect.x + trailRect.z + 0.13, trailRect.y + trailRect.w * 0.54);
  vec2 wingAnchor = rectCenter(wingsRect) + vec2(wingsRect.z * 0.62, wingsRect.w * 0.03);
  vec2 offCanvas = vec2(1.12, -0.18);

  if (ms < 700.0) {
    return offCanvas;
  }
  if (ms < 1300.0) {
    float t = easeOutQuint((ms - 700.0) / 600.0);
    return mix(offCanvas, trailStart, t);
  }
  if (ms < 2100.0) {
    float t = (ms - 1300.0) / 800.0;
    return mix(trailStart, trailEnd, t);
  }
  if (ms < 2800.0) {
    float t = easeInOutCubic((ms - 2100.0) / 700.0);
    return mix(trailEnd, wingAnchor, t);
  }

  float settled = (ms - 2800.0) / 1000.0;
  vec2 orbit = vec2(sin(settled * 0.44), sin(settled * 0.29 + 1.7)) * 0.018;
  return wingAnchor + orbit;
}

float sourceIntensity(float ms) {
  if (ms < 700.0) {
    return 0.0;
  }
  if (ms < 1300.0) {
    return 0.82 * easeOutQuint((ms - 700.0) / 600.0);
  }
  if (ms < 2100.0) {
    float t = (ms - 1300.0) / 800.0;
    return 0.48 + 0.34 * sin(3.14159265 * t);
  }
  if (ms < 2800.0) {
    float t = easeInOutCubic((ms - 2100.0) / 700.0);
    return mix(0.5, 0.24, t);
  }
  return 0.18;
}

float roundedRectMask(vec2 uv, vec4 rect, float feather) {
  vec2 center = rectCenter(rect);
  vec2 halfSize = rect.zw * 0.5;
  vec2 q = abs(uv - center) - halfSize;
  float d = length(max(q, 0.0)) + min(max(q.x, q.y), 0.0);
  return 1.0 - smoothstep(0.0, feather, d);
}

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
  gl_FragColor = vec4(color * light, alpha);
}
`;
