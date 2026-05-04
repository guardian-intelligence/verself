float firstlight_caustics(OpticalFrame frame, float bend, float t) {
  vec2 q = vec2(frame.along * 3.4 + t * 0.035, frame.across * 18.0 - t * 0.12);
  q += vec2(firstlight_fbm(q * 0.35 + 7.0), firstlight_fbm(q * 0.45 - 2.0)) * 0.72;
  float ridge = firstlight_ridge(q);
  float fine = firstlight_ridge(q * vec2(1.7, 1.2) + 4.3);
  return (ridge * 0.72 + fine * 0.28) * (0.18 + bend * 0.82);
}
