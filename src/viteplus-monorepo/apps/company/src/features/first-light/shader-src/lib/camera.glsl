struct OpticalFrame {
  vec2 uv;
  vec2 p;
  vec2 diagonal;
  vec2 cross;
  float along;
  float across;
  float aspect;
  float drift;
};

mat2 firstlight_rotate(float a) {
  float s = sin(a);
  float c = cos(a);
  return mat2(c, -s, s, c);
}

OpticalFrame firstlight_frame(vec2 uv, float aspect, float t) {
  vec2 p = (uv - 0.5) * vec2(aspect, 1.0);
  float breath = sin(t * 0.19) * 0.011 + sin(t * 0.071 + 1.3) * 0.007;
  p += vec2(sin(t * 0.053 + 0.4), cos(t * 0.047 + 1.1)) * 0.012;
  p = firstlight_rotate(-0.19 + breath) * p;

  vec2 diagonal = normalize(vec2(0.74, -0.67));
  vec2 cross = vec2(-diagonal.y, diagonal.x);
  float along = dot(p, diagonal);
  float across = dot(p, cross);

  OpticalFrame frame;
  frame.uv = uv;
  frame.p = p;
  frame.diagonal = diagonal;
  frame.cross = cross;
  frame.along = along;
  frame.across = across;
  frame.aspect = aspect;
  frame.drift = breath;
  return frame;
}
