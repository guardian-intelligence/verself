vec3 firstlight_plate(OpticalFrame frame, float t) {
  float vignette = smoothstep(1.24, 0.12, length(frame.p * vec2(0.92, 1.16)));
  float hazeA = firstlight_fbm(frame.p * 1.25 + vec2(t * 0.018, -t * 0.011));
  float hazeB = firstlight_fbm(frame.p * 2.2 + vec2(-t * 0.013, t * 0.017) + 9.7);
  float coolWash = smoothstep(-0.62, 0.52, frame.across + frame.along * 0.18);

  vec3 ink = vec3(0.006, 0.007, 0.0075);
  vec3 blueBlack = vec3(0.012, 0.025, 0.029);
  vec3 plate = mix(ink, blueBlack, 0.34 * coolWash + 0.18 * hazeA);
  plate += vec3(0.012, 0.018, 0.019) * hazeB * vignette;
  plate *= 0.62 + vignette * 0.66;
  return plate;
}
