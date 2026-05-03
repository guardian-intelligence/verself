vec3 firstlight_tone(vec3 color) {
  color = max(color, vec3(0.0));
  color = vec3(1.0) - exp(-color * vec3(1.34, 1.22, 1.08));
  color = pow(color, vec3(0.92));
  return color;
}

vec3 firstlight_grain(vec2 uv, vec2 resolution, float t) {
  float g = firstlight_hash(floor(uv * resolution) + vec2(floor(t * 24.0), floor(t * 17.0)));
  float luma = g - 0.5;
  return vec3(luma * 0.065);
}
