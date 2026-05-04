float firstlight_band(float across, float center, float width, float feather) {
  float d = abs(across - center);
  return 1.0 - smoothstep(width, width + feather, d);
}

float firstlight_band_edge(float across, float center, float width, float feather) {
  float d = abs(abs(across - center) - width);
  return 1.0 - smoothstep(0.0, feather, d);
}

vec3 firstlight_bands(OpticalFrame frame, float t, float bend, out float mask, out float edge) {
  float warpedAcross = frame.across + bend * 0.19 + sin(frame.along * 6.0 + t * 0.17) * 0.012;
  float main = firstlight_band(warpedAcross, -0.035, 0.105, 0.06);
  float broad = firstlight_band(warpedAcross, -0.19, 0.055, 0.075);
  float lower = firstlight_band(warpedAcross, 0.205, 0.028, 0.045);
  float hairA = firstlight_band(warpedAcross, -0.315, 0.006, 0.012);
  float hairB = firstlight_band(warpedAcross, 0.335, 0.007, 0.014);

  float taper = smoothstep(-1.08, -0.05, frame.along) * smoothstep(0.98, 0.0, frame.along);
  mask = clamp((main * 0.82 + broad * 0.48 + lower * 0.38 + hairA * 0.56 + hairB * 0.6) * taper, 0.0, 1.0);
  edge =
    firstlight_band_edge(warpedAcross, -0.035, 0.105, 0.018) * 0.7 +
    firstlight_band_edge(warpedAcross, -0.19, 0.055, 0.014) * 0.45 +
    firstlight_band_edge(warpedAcross, 0.205, 0.028, 0.01) * 0.4 +
    (hairA + hairB) * 0.8;
  edge *= taper;

  vec3 glass = vec3(0.09, 0.19, 0.21) * broad;
  glass += vec3(0.28, 0.41, 0.42) * main;
  glass += vec3(0.14, 0.25, 0.27) * lower;
  glass += vec3(0.95, 0.72, 0.58) * (hairA + hairB) * 0.32;
  return glass * mask;
}
