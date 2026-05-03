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
