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
