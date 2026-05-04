vec3 celestial_disk_color(float heat, float warmth) {
  vec3 cool = vec3(0.42, 0.72, 0.88);
  vec3 amber = vec3(1.0, 0.68, 0.36);
  vec3 white = vec3(1.0, 0.93, 0.72);
  return mix(mix(cool, amber, warmth), white, heat * 0.42);
}

vec3 celestial_disk(OpticalFrame frame, CelestialShapeField shape, CelestialLensField lens, float t) {
  vec2 local = celestial_rotate(frame.p - uShapeCenter + lens.offset * 0.44, -uDiskParams.x);
  local.y /= max(0.14, 0.32 + uDiskParams.x * 0.18);

  float r = length(local);
  float radial = smoothstep(uDiskParams.y, uDiskParams.y + 0.035, r) *
    (1.0 - smoothstep(uDiskParams.z - 0.05, uDiskParams.z, r));
  float plane = exp(-abs(local.y) * 13.5);
  float turbulence = firstlight_fbm(local * 4.2 + vec2(t * 0.08, -t * 0.035));
  float clumps = 0.64 + turbulence * 0.52;
  float doppler = 1.0 + dot(normalize(local + vec2(0.001)), vec2(1.0, 0.0)) * uDiskTone.z;
  float shadowCut = 1.0 - smoothstep(-0.03, 0.055, -shape.sdf);
  float intensity = radial * plane * clumps * doppler * uDiskParams.w * shadowCut;

  float heat = smoothstep(uDiskParams.y, uDiskParams.y + 0.12, r);
  return celestial_disk_color(heat, uDiskTone.x) * max(intensity, 0.0);
}
