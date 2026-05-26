float firstlight_aniso(vec2 p, vec2 axis, vec2 center, float longRadius, float shortRadius) {
  vec2 cross = vec2(-axis.y, axis.x);
  vec2 q = p - center;
  float a = dot(q, axis) / longRadius;
  float b = dot(q, cross) / shortRadius;
  return exp(-(a * a + b * b));
}

vec3 firstlight_flare(OpticalFrame frame, float rim, float bandEdge, float caustic, float t) {
  vec2 coreCenter = vec2(0.41, -0.42) + vec2(sin(t * 0.11), cos(t * 0.08)) * 0.012;
  float core = firstlight_aniso(frame.p, frame.diagonal, coreCenter, 0.28, 0.055);
  float hot = firstlight_aniso(frame.p, frame.diagonal, coreCenter + frame.cross * 0.015, 0.15, 0.026);
  float diagonalStreak = exp(-abs(dot(frame.p - coreCenter, frame.cross)) * 17.0);
  diagonalStreak *= smoothstep(-0.9, 0.1, frame.along) * smoothstep(1.1, -0.15, frame.along);
  float counter = exp(-abs(dot(frame.p - coreCenter, vec2(0.86, 0.51))) * 31.0);
  counter *= exp(-length(frame.p - coreCenter) * 1.8);
  float star = pow(max(0.0, diagonalStreak), 2.2) * 0.34 + counter * 0.18;
  float pulse = 0.93 + sin(t * 0.43 + caustic * 1.7) * 0.055;

  vec3 warm = vec3(1.0, 0.78, 0.54);
  vec3 whiteHot = vec3(1.0, 0.965, 0.84);
  vec3 flare = whiteHot * hot * 2.8;
  flare += warm * core * 1.2;
  flare += vec3(0.95, 0.58, 0.36) * rim * 0.34;
  flare += vec3(0.74, 0.9, 1.0) * bandEdge * 0.22;
  flare += warm * star;
  return flare * pulse;
}
