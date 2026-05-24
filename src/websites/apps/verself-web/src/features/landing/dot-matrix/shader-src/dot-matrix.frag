precision highp float;

uniform float uActive;
uniform float uDpr;
uniform float uLoadProgress;
uniform float uSeed;
uniform float uTime;
uniform vec2 uResolution;
uniform vec4 uPointer;
uniform vec4 uScroll;

in vec2 vUv;

out vec4 fragColor;

float hash12(vec2 p) {
  vec3 p3 = fract(vec3(p.xyx) * 0.1031);
  p3 += dot(p3, p3.yzx + 33.33);
  return fract((p3.x + p3.y) * p3.z);
}

float pulseTrain(float phase, float width, float power) {
  float d = abs(fract(phase) - 0.5) * 2.0;
  return pow(max(0.0, 1.0 - smoothstep(0.0, width, d)), power);
}

float dotMask(vec2 localPixel, float radius) {
  float d = length(localPixel);
  return smoothstep(radius + 0.85, radius - 0.45, d);
}

float taperedWindow(vec2 uv) {
  vec2 centered = uv - 0.5;
  float vertical = smoothstep(0.02, 0.18, uv.y) * smoothstep(1.0, 0.82, uv.y);
  float horizontal = smoothstep(0.0, 0.34, 0.5 - abs(centered.x));
  return vertical * horizontal;
}

void main() {
  vec2 fragCoord = vUv * uResolution;
  float dpr = max(uDpr, 1.0);
  float stepPx = 14.0 * dpr;
  vec2 cell = floor(fragCoord / stepPx);
  vec2 cellCount = max(floor(uResolution / stepPx), vec2(1.0));
  vec2 cellUv = (cell + 0.5) / cellCount;
  vec2 localPixel = fract(fragCoord / stepPx) * stepPx - stepPx * 0.5;

  float mask = dotMask(localPixel, 1.24 * dpr);
  float field = taperedWindow(cellUv);
  float edge = smoothstep(0.98, 0.22, length((cellUv - 0.5) * vec2(0.82, 1.14)));
  float base = 0.18 + 0.22 * field + 0.06 * edge;

  float jitter = hash12(cell + vec2(uSeed * 97.0, 11.0));
  float slowTwinkle = 0.5 + 0.5 * sin(uTime * 1.7 + jitter * 6.28318);
  float dormant = base * mix(0.76, 1.18, slowTwinkle * 0.22);

  float diagonalA = cellUv.x * 0.78 + cellUv.y * 1.42 + 0.08 * sin(cellUv.y * 7.0);
  float diagonalB = (1.0 - cellUv.x) * 1.18 + cellUv.y * 0.82 + 0.06 * sin(cellUv.x * 6.0);
  float vertical = cellUv.y + 0.12 * sin(cellUv.x * 9.0 + uTime * 0.38);

  float waveA = pulseTrain(diagonalA - uTime * 0.165 - uSeed, 0.085, 1.6);
  float waveB = pulseTrain(diagonalB - uTime * 0.118 + uSeed * 0.41, 0.07, 1.8);
  float waveC = pulseTrain(vertical - uTime * 0.09, 0.052, 2.2);

  float cluster = smoothstep(0.18, 0.92, hash12(cell * 0.73 + floor(uTime * 9.0)));
  float loadGate = smoothstep(0.0, 0.72, uLoadProgress);
  float ignition = pulseTrain(diagonalA + diagonalB - uLoadProgress * 1.85, 0.19, 1.35);
  float waveEnergy = (waveA * 1.05 + waveB * 0.82 + waveC * 0.42 + ignition * 1.35) * loadGate;
  waveEnergy *= field * (0.58 + 0.42 * cluster);

  vec2 pointerUv = uPointer.xy;
  float pointerActive = uPointer.z;
  float pointerDist = distance(cellUv, pointerUv);
  float pointerWake = pointerActive * smoothstep(0.34, 0.0, pointerDist);
  pointerWake *= 0.35 + 0.65 * pulseTrain(pointerDist * 2.4 - uTime * 0.72, 0.18, 1.5);

  float scrollWake = clamp(abs(uScroll.y) * 0.28, 0.0, 0.42);
  float luminance = dormant + waveEnergy + pointerWake + scrollWake;
  luminance = clamp(luminance, 0.0, 1.45);

  vec3 baseTone = vec3(0.42);
  vec3 hotTone = mix(vec3(0.86, 0.94, 1.0), vec3(0.58, 0.95, 0.86), waveB);
  vec3 color = mix(baseTone, hotTone, smoothstep(0.32, 1.0, luminance));
  color += vec3(0.18, 0.30, 0.34) * waveEnergy * 0.32;

  float alpha = mask * (0.2 + 0.72 * clamp(luminance, 0.0, 1.0));
  alpha *= mix(0.72, 1.0, uActive);

  fragColor = vec4(color * luminance, alpha);
}
