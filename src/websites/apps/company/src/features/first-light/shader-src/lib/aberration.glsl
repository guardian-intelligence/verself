vec3 firstlight_aberrate(vec3 color, float edge, float rim, float bandEdge) {
  float split = clamp(edge * 0.42 + rim * 0.34 + bandEdge * 0.22, 0.0, 1.0);
  vec3 cool = vec3(0.37, 0.73, 1.0) * split * 0.22;
  vec3 amber = vec3(1.0, 0.53, 0.31) * split * 0.18;
  color.r += amber.r;
  color.g += cool.g * 0.38 + amber.g * 0.18;
  color.b += cool.b;
  return color;
}
