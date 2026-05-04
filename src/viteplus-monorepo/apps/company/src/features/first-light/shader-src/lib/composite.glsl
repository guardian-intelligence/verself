vec3 celestial_shape_debug(CelestialShapeField shape) {
  vec3 field = vec3(0.03, 0.06, 0.07) + vec3(shape.influence * 0.18);
  vec3 normalColor = vec3(shape.normal * 0.5 + 0.5, 0.28);
  vec3 rim = vec3(1.0, 0.82, 0.58) * shape.rim;
  return mix(field + normalColor * 0.22 + rim, vec3(0.0), shape.occlusion);
}

vec3 celestial_lens_debug(CelestialShapeField shape, CelestialLensField lens) {
  vec3 direction = vec3(lens.offset * 3.0 + 0.5, 0.24);
  vec3 ring = vec3(0.98, 0.78, 0.48) * lens.ring;
  vec3 strength = vec3(0.2, 0.52, 0.66) * lens.strength * 5.5;
  return mix(direction * 0.28 + strength + ring, vec3(0.0), shape.occlusion);
}

vec3 celestial_composite(
  vec3 color,
  vec3 disk,
  CelestialShapeField shape,
  CelestialLensField lens
) {
  if (uDebugMode > 0.5 && uDebugMode < 1.5) {
    return celestial_shape_debug(shape);
  }
  if (uDebugMode > 1.5 && uDebugMode < 2.5) {
    return celestial_lens_debug(shape, lens);
  }
  if (uDebugMode > 2.5 && uDebugMode < 3.5) {
    return mix(disk + vec3(0.12, 0.06, 0.025) * lens.ring, vec3(0.0), shape.occlusion);
  }

  vec3 photonRing = vec3(1.0, 0.78, 0.52) * lens.ring;
  vec3 shadow = vec3(0.0, 0.0, 0.002);
  vec3 composite = color + disk + photonRing;
  composite = mix(composite, shadow, shape.occlusion);
  composite += vec3(0.54, 0.78, 0.88) * abs(lens.shear) * 0.12;
  return composite;
}
