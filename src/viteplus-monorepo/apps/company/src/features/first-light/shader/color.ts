export const glslColor = /* glsl */ `
vec3 firstlight_warm() {
  return vec3(1.0, 0.972, 0.9);
}

vec3 firstlight_cool() {
  return vec3(0.42, 0.76, 1.0);
}

vec3 firstlight_amber() {
  return vec3(1.0, 0.62, 0.34);
}

vec3 firstlight_spectral(float edge, float caustic) {
  vec3 warm = firstlight_warm();
  vec3 coolEdge = mix(warm, firstlight_cool(), 0.18 * edge);
  vec3 amberRim = mix(coolEdge, firstlight_amber(), 0.14 * caustic * edge);
  return amberRim;
}
`;
