precision highp float;

const int MAX_SHADOW_MASKS = 32;

uniform float uActive;
uniform float uAmbient;
uniform float uContrast;
uniform float uDpr;
uniform float uDotSpacingPx;
uniform float uLightAmbient;
uniform float uPhase;
uniform float uPostExposure;
uniform float uSeed;
uniform float uSurfaceDepth;
uniform int uShadowMaskCount;
uniform sampler2D uFieldState;
uniform sampler2D uWaveState;
uniform vec4 uCameraRect;
uniform vec2 uResolution;
uniform vec3 uKeyLightColor;
uniform vec3 uKeyLightDirection;
uniform vec3 uRimLightColor;
uniform vec3 uRimLightDirection;
uniform vec4 uShadowMaskFalloff[MAX_SHADOW_MASKS];
uniform vec4 uShadowMaskParams[MAX_SHADOW_MASKS];
uniform vec4 uShadowMaskRects[MAX_SHADOW_MASKS];

in vec2 vUv;

out vec4 fragColor;

float hash12(vec2 p) {
  vec3 p3 = fract(vec3(p.xyx) * 0.1031);
  p3 += dot(p3, p3.yzx + 33.33);
  return fract((p3.x + p3.y) * p3.z);
}

float dotMask(vec2 localPixel, float radius) {
  float d = length(localPixel);
  return smoothstep(radius + 0.85, radius - 0.45, d);
}

float roundedRectSignedDistancePx(vec2 pointPx, vec4 rectPx, float radiusPx) {
  vec2 halfSize = max(rectPx.zw * 0.5, vec2(0.0001));
  vec2 center = rectPx.xy + halfSize;
  float radius = min(max(radiusPx, 0.0), min(halfSize.x, halfSize.y));
  vec2 q = abs(pointPx - center) - (halfSize - vec2(radius));
  return length(max(q, 0.0)) + min(max(q.x, q.y), 0.0) - radius;
}

float dotShadowAt(vec2 dotCenterPx) {
  float shadow = 0.0;

  for (int index = 0; index < MAX_SHADOW_MASKS; index += 1) {
    if (index >= uShadowMaskCount) {
      break;
    }

    vec4 rectPx = uShadowMaskRects[index];
    vec4 falloff = uShadowMaskFalloff[index];
    vec4 params = uShadowMaskParams[index];
    float distancePx = roundedRectSignedDistancePx(dotCenterPx, rectPx, params.x);
    float featherPx = max(params.y, 0.001);
    float insideCoverage = 1.0 - smoothstep(0.0, featherPx, distancePx);
    float outsideCoverage = smoothstep(-featherPx, 0.0, distancePx);
    float coverage = params.w > 0.5 ? outsideCoverage : insideCoverage;
    coverage = pow(clamp(coverage, 0.0, 1.0), max(falloff.x, 0.001));
    shadow = max(shadow, coverage * params.z);
  }

  return clamp(shadow, 0.0, 1.0);
}

void main() {
  vec2 fragCoord = vUv * uResolution;
  float dpr = max(uDpr, 1.0);
  float stepPx = max(uDotSpacingPx, 1.0) * dpr;
  vec2 cell = floor(fragCoord / stepPx);
  vec2 cellCount = max(floor(uResolution / stepPx), vec2(1.0));
  vec2 cellUv = (cell + 0.5) / cellCount;
  vec2 worldUv = uCameraRect.xy + cellUv * uCameraRect.zw;
  vec2 dotCenterPx = (cell + 0.5) * stepPx;
  vec2 localPixel = fract(fragCoord / stepPx) * stepPx - stepPx * 0.5;

  vec4 fields = texture(uFieldState, worldUv);
  float shadow = dotShadowAt(dotCenterPx);
  float sceneVisibility = fields.r;
  float readableProtection = 1.0 - max(shadow, fields.a);
  float dotFade = sceneVisibility * readableProtection;

  float mask = dotMask(localPixel, stepPx * 0.225);
  float base = 0.34;

  float jitter = hash12(cell + vec2(uSeed * 97.0, 11.0));
  float slowTwinkle = 0.5 + 0.5 * sin(uPhase * 1.7 + jitter * 6.28318);
  float dormant = base * mix(0.82, 1.08, slowTwinkle * 0.18);

  float cluster = smoothstep(0.18, 0.92, hash12(cell * 0.73 + vec2(uSeed * 13.0, 4.0)));
  vec4 waveState = texture(uWaveState, worldUv);
  float height = waveState.r;
  float velocity = height - waveState.g;
  float energy = clamp(abs(height) * 5.4 + abs(velocity) * 10.5, 0.0, 0.95);
  float waveEnergy =
    energy * dotFade * mix(0.78, 1.04, cluster) * (0.76 + 0.28 * uAmbient);

  vec2 waveTexel = 1.0 / max(cellCount, vec2(1.0));
  float heightWest = texture(uWaveState, worldUv - vec2(waveTexel.x, 0.0)).r;
  float heightEast = texture(uWaveState, worldUv + vec2(waveTexel.x, 0.0)).r;
  float heightSouth = texture(uWaveState, worldUv - vec2(0.0, waveTexel.y)).r;
  float heightNorth = texture(uWaveState, worldUv + vec2(0.0, waveTexel.y)).r;
  vec2 waveSlope =
    vec2(heightEast - heightWest, heightNorth - heightSouth) * uSurfaceDepth;

  mask = dotMask(localPixel, stepPx * (0.212 + waveEnergy * 0.082));
  if (mask <= 0.001) {
    discard;
  }

  float waveHighlight = smoothstep(0.08, 0.52, waveEnergy);
  vec2 dotUnit = localPixel / max(stepPx * 0.5, 0.001);
  float capZ = sqrt(max(0.0, 1.0 - dot(dotUnit, dotUnit)));
  vec3 capNormal = normalize(vec3(dotUnit * 0.42, capZ));
  vec3 fieldNormal = normalize(vec3(-waveSlope.x, -waveSlope.y, 1.0));
  vec3 normal = normalize(mix(capNormal, fieldNormal, clamp(0.32 + waveEnergy * 0.22, 0.0, 0.68)));
  vec3 keyDirection = normalize(uKeyLightDirection);
  vec3 rimDirection = normalize(uRimLightDirection);
  float keyLight = max(dot(normal, keyDirection), 0.0);
  float rimLight = pow(max(dot(normal, rimDirection), 0.0), 2.35);
  float grazingLight = pow(1.0 - clamp(normal.z, 0.0, 1.0), 1.8);

  float luminance = dormant * mix(0.78, 0.92, uAmbient) + waveEnergy * 1.22;
  luminance *= dotFade;
  luminance *= uContrast;
  luminance = clamp(luminance, 0.0, 1.42);

  vec3 baseTone = vec3(0.42);
  vec3 troughTone = vec3(0.50, 0.78, 0.72);
  vec3 crestTone = vec3(1.0);
  vec3 hotTone = mix(troughTone, crestTone, smoothstep(-0.14, 0.14, height));
  vec3 color = mix(baseTone, hotTone, smoothstep(0.06, 0.44, waveEnergy));
  color = mix(color, vec3(1.0), waveHighlight * 0.42);
  color += vec3(0.08, 0.14, 0.16) * waveEnergy * 0.1;
  vec3 light =
    vec3(uLightAmbient) +
    uKeyLightColor * keyLight * 0.72 +
    uRimLightColor * rimLight * (0.14 + waveHighlight * 0.26) +
    uRimLightColor * grazingLight * waveEnergy * 0.1;

  float alpha =
    mask * mix(0.34 + 0.44 * clamp(luminance, 0.0, 1.0), 0.84, waveHighlight * 0.48);
  alpha *= dotFade;
  alpha *= mix(0.72, 1.0, uActive);

  fragColor = vec4(min(color * light * luminance * uPostExposure, vec3(1.0)), alpha);
}
