// Base wash tuned above the legacy CSS overlay (0.06 / 0.05 peaks).

export type LandingSpotlightSource = {
  readonly decay: number;
  readonly driftPhase: number;
  readonly peak: number;
  readonly radius: number;
  readonly x: number;
  readonly y: number;
  readonly z: number;
};

export const landingSpotlightIntensityScale = 1.72;

export const landingSpotlightMotion = {
  driftX: 0.038,
  driftY: 0.031,
  peakPulse: 0.14,
  peakPulseSpeed: 0.38,
  speed: 0.19,
} as const;

export const landingSpotlightSources: readonly LandingSpotlightSource[] = [
  {
    decay: 2,
    driftPhase: 0,
    peak: 0.085,
    radius: 0.27,
    x: 0.5,
    y: 0.45,
    z: 1.35,
  },
  {
    decay: 2,
    driftPhase: 2.05,
    peak: 0.072,
    radius: 0.2,
    x: 0.66,
    y: 0.4,
    z: 1.2,
  },
] as const;

export type LandingSpotlightState = {
  readonly centerUv: [number, number];
  readonly peak: number;
  readonly position: [number, number, number];
  readonly radius: number;
};

export function resolveSpotlightPosition(
  viewportWidth: number,
  viewportHeight: number,
  centerUv: [number, number],
  z: number,
): [number, number, number] {
  const x = viewportWidth * (centerUv[0] - 0.5);
  const y = viewportHeight * (0.5 - centerUv[1]);
  return [x, y, z];
}

export function resolveSpotlightState(
  source: LandingSpotlightSource,
  elapsedSeconds: number,
  animate: boolean,
): LandingSpotlightState {
  const motion = landingSpotlightMotion;
  const driftX = animate
    ? Math.sin(elapsedSeconds * motion.speed + source.driftPhase) * motion.driftX
    : 0;
  const driftY = animate
    ? Math.cos(elapsedSeconds * motion.speed * 0.86 + source.driftPhase) * motion.driftY
    : 0;
  const peakPulse = animate
    ? 1 +
      Math.sin(elapsedSeconds * motion.peakPulseSpeed + source.driftPhase * 1.3) * motion.peakPulse
    : 1;

  const centerUv: [number, number] = [source.x + driftX, source.y + driftY];
  const peak = source.peak * landingSpotlightIntensityScale * peakPulse;

  return {
    centerUv,
    peak,
    position: [0, 0, source.z],
    radius: source.radius,
  };
}

export function resolveSpotlightWorldPosition(
  viewportWidth: number,
  viewportHeight: number,
  state: LandingSpotlightState,
  z: number,
): [number, number, number] {
  return resolveSpotlightPosition(viewportWidth, viewportHeight, state.centerUv, z);
}
