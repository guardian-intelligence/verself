import wordmarkFontUrl from "three/examples/fonts/helvetiker_bold.typeface.json?url";

export const WORDMARK_TEXT = "Verself";

export const wordmarkTypeface = wordmarkFontUrl;

// Text3D geometry is authored at size=1; on-screen px size comes from
// WordmarkScreenSizer + landing-hero-wordmark-spec (drei ScreenSizer).
export const WORDMARK_CANONICAL_SIZE = 1;

// Helvetiker bold reads slightly taller than Geist at the same CSS cap size.
export const wordmarkTypefaceScreenCalibration = 0.92;

export const wordmarkGeometry = {
  bevelEnabled: true,
  bevelOffset: 0,
  bevelSegments: 10,
  bevelSize: 0.032,
  bevelThickness: 0.05,
  curveSegments: 18,
  height: 0.36,
  letterSpacing: -0.08,
  lineHeight: 1,
  size: WORDMARK_CANONICAL_SIZE,
} as const;

export const wordmarkCaustics = {
  backside: true,
  backsideIOR: 1.22,
  color: "#ffd4a0",
  frames: Number.POSITIVE_INFINITY,
  intensity: 0.095,
  ior: 1.22,
  lightSource: [1.8, 2.8, 4.8] as [number, number, number],
  resolution: 1024,
  worldRadius: 0.012,
} as const;

export const wordmarkTransmission = {
  anisotropicBlur: 0.12,
  anisotropy: 0.18,
  backside: true,
  backsideEnvMapIntensity: 0.85,
  backsideResolution: 256,
  backsideThickness: 0.35,
  chromaticAberration: 0.045,
  clearcoat: 1,
  clearcoatRoughness: 0.04,
  color: "#f6f8ff",
  distortion: 0.14,
  distortionScale: 0.28,
  ior: 1.48,
  resolution: 512,
  roughness: 0.015,
  samples: 6,
  temporalDistortion: 0.08,
  thickness: 0.72,
  transmission: 1,
  transmissionSampler: true,
} as const;
