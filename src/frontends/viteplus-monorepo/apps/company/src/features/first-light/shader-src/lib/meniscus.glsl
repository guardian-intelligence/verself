float firstlight_ellipse_sdf(vec2 p, vec2 center, vec2 radius) {
  vec2 q = (p - center) / radius;
  return length(q) - 1.0;
}

float firstlight_meniscus(OpticalFrame frame, out float rim, out float body, out float bend) {
  vec2 center = vec2(0.54, -0.06);
  float sdf = firstlight_ellipse_sdf(frame.p, center, vec2(0.32, 0.79));
  body = 1.0 - smoothstep(-0.035, 0.08, sdf);
  rim = exp(-abs(sdf) * 38.0) * smoothstep(-0.92, 0.22, frame.along);
  rim += exp(-abs(sdf + 0.06) * 30.0) * 0.22;
  bend = clamp(-sdf * body + rim * 0.65, 0.0, 1.0);
  return sdf;
}
