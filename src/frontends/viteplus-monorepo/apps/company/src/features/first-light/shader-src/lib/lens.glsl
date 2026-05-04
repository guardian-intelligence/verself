struct CelestialLensField {
  vec2 offset;
  float strength;
  float ring;
  float shear;
};

OpticalFrame celestial_offset_frame(OpticalFrame frame, vec2 offset) {
  frame.p += offset;
  frame.along = dot(frame.p, frame.diagonal);
  frame.across = dot(frame.p, frame.cross);
  return frame;
}

CelestialLensField celestial_lens(CelestialShapeField shape) {
  float distance = max(shape.sdf, 0.0);
  float nearField = shape.influence / (0.18 + distance * 3.6);
  float strength = uLensParams.x * nearField * (1.0 - shape.occlusion);
  vec2 tangent = vec2(-shape.normal.y, shape.normal.x);
  vec2 swirl = tangent * sin(shape.angle * 2.0 + uDiskTone.y) * strength * 0.28;
  vec2 pull = -shape.normal * strength;
  float ringCore = exp(-abs(shape.sdf - uLensParams.w * 1.25) / max(uLensParams.w, 0.001));

  CelestialLensField lens;
  lens.offset = pull + swirl;
  lens.strength = strength;
  lens.ring = (shape.rim * 0.18 + shape.photon * 0.22 + ringCore) * uDiskTone.w *
    (1.0 - shape.occlusion);
  lens.shear = dot(shape.normal, normalize(vec2(0.88, -0.48))) * strength;
  return lens;
}
