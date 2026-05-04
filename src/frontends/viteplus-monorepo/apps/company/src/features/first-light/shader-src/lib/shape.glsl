struct CelestialShapeField {
  float sdf;
  float occlusion;
  float rim;
  float photon;
  float influence;
  vec2 normal;
  vec2 local;
  float angle;
  float radius;
};

vec2 celestial_rotate(vec2 p, float a) {
  float s = sin(a);
  float c = cos(a);
  return mat2(c, -s, s, c) * p;
}

vec2 celestial_local_at(vec2 p) {
  return celestial_rotate(p - uShapeCenter, -uShapeParams.z);
}

float celestial_shape_radius(float angle, float t) {
  float pinch = uShapeWarp.x * cos(angle * 2.0 - 0.35);
  float ripple = uShapeWarp.y * sin(angle * uShapeWarp.z + t * 0.18);
  return max(0.02, uShapeParams.x * (1.0 + pinch + ripple));
}

float celestial_sdf_at(vec2 p, float t) {
  vec2 local = celestial_local_at(p);
  float angle = atan(local.y, local.x);
  float radius = celestial_shape_radius(angle, t);
  vec2 q = local / vec2(radius, radius * max(uShapeParams.y, 0.05));
  float exponent = max(uShapeParams.w, 0.8);
  float superellipse = pow(
    pow(abs(q.x), exponent) + pow(abs(q.y), exponent),
    1.0 / exponent
  ) - 1.0;
  return superellipse * radius;
}

CelestialShapeField celestial_shape(OpticalFrame frame, float t) {
  vec2 local = celestial_local_at(frame.p);
  float angle = atan(local.y, local.x);
  float sdf = celestial_sdf_at(frame.p, t);
  float softness = max(uShapeWarp.w, 0.001);
  vec2 eps = vec2(0.002, 0.0);
  vec2 gradient = vec2(
    celestial_sdf_at(frame.p + eps.xy, t) - celestial_sdf_at(frame.p - eps.xy, t),
    celestial_sdf_at(frame.p + eps.yx, t) - celestial_sdf_at(frame.p - eps.yx, t)
  );
  vec2 normal = normalize(gradient + vec2(0.0001, 0.0));
  float occlusion = 1.0 - smoothstep(-softness, softness, sdf);
  float rim = exp(-abs(sdf) / max(softness * 1.9, 0.001));
  float photon = exp(-abs(sdf - softness * 2.5) / max(softness * 2.0, 0.001));
  float influence = exp(-pow(max(sdf, 0.0) / max(uLensParams.y, 0.01), max(uLensParams.z, 0.1)));

  CelestialShapeField field;
  field.sdf = sdf;
  field.occlusion = occlusion;
  field.rim = rim;
  field.photon = photon;
  field.influence = influence;
  field.normal = normal;
  field.local = local;
  field.angle = angle;
  field.radius = length(local);
  return field;
}
