float firstlight_hash(vec2 p) {
  vec3 p3 = fract(vec3(p.xyx) * 0.1031);
  p3 += dot(p3, p3.yzx + 33.33);
  return fract((p3.x + p3.y) * p3.z);
}

float firstlight_noise(vec2 p) {
  vec2 i = floor(p);
  vec2 f = fract(p);
  vec2 u = f * f * (3.0 - 2.0 * f);
  return mix(
    mix(firstlight_hash(i + vec2(0.0, 0.0)), firstlight_hash(i + vec2(1.0, 0.0)), u.x),
    mix(firstlight_hash(i + vec2(0.0, 1.0)), firstlight_hash(i + vec2(1.0, 1.0)), u.x),
    u.y
  );
}

float firstlight_fbm(vec2 p) {
  float value = 0.0;
  float amp = 0.5;
  mat2 rotate = mat2(0.8, -0.6, 0.6, 0.8);
  for (int i = 0; i < 4; i++) {
    value += amp * firstlight_noise(p);
    p = rotate * p * 2.12 + 17.0;
    amp *= 0.5;
  }
  return value;
}

float firstlight_ridge(vec2 p) {
  float n = firstlight_fbm(p);
  return pow(1.0 - abs(n * 2.0 - 1.0), 3.2);
}
