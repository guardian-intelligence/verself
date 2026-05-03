mat2 rotate2d(float a) {
  float s = sin(a);
  float c = cos(a);
  return mat2(c, -s, s, c);
}

vec2 rectCenter(vec4 rect) {
  return rect.xy + rect.zw * 0.5;
}

float roundedRectMask(vec2 uv, vec4 rect, float feather) {
  vec2 center = rectCenter(rect);
  vec2 halfSize = rect.zw * 0.5;
  vec2 q = abs(uv - center) - halfSize;
  float d = length(max(q, 0.0)) + min(max(q.x, q.y), 0.0);
  return 1.0 - smoothstep(0.0, feather, d);
}
