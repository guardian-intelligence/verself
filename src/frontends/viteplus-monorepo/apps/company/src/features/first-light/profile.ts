import type { CelestialRenderProfile, FirstLightDebugMode } from "./types";

export const firstLightCelestialProfile: CelestialRenderProfile = {
  seed: 0.61803398875,
  motionScale: 1,
  shape: {
    center: [0.42, -0.04],
    radius: 0.255,
    aspect: 1.12,
    rotation: -0.18,
    exponent: 2.55,
    pinch: 0.08,
    ripple: 0.032,
    rippleFrequency: 3.0,
    softness: 0.018,
  },
  lens: {
    strength: 0.18,
    radius: 0.52,
    falloff: 1.55,
    ringWidth: 0.036,
    ringIntensity: 0.76,
  },
  disk: {
    tilt: -0.42,
    innerRadius: 0.3,
    outerRadius: 0.94,
    intensity: 0.58,
    warmth: 0.66,
    spin: 0.34,
    anisotropy: 0.46,
  },
};

const DEBUG_MODE_CODE: Record<FirstLightDebugMode, number> = {
  composite: 0,
  shape: 1,
  lens: 2,
  disk: 3,
};

export function firstLightDebugMode(search: string): FirstLightDebugMode {
  const visualTest = new URLSearchParams(search).get("visual-test");
  if (visualTest === "shape" || visualTest === "lens" || visualTest === "disk") {
    return visualTest;
  }
  return "composite";
}

export function firstLightDebugModeCode(mode: FirstLightDebugMode): number {
  return DEBUG_MODE_CODE[mode];
}
