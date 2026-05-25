import {
  createRandom,
  flowDirectionTowardCamera,
  projectedVisibilityAlongPath,
  randomRange,
  weightedRandomSpawnPoint,
  type RandomSource,
} from "../scene/sampling";
import type { CompiledWaveFields, DomainRect, Vec2, WaveSceneModel } from "../scene/types";
import type { WaveImpulse } from "./types";

interface AmbientWave {
  readonly direction: Vec2;
  readonly frontLength: number;
  readonly origin: Vec2;
  readonly radius: number;
  readonly strength: number;
}

interface AmbientWaveSchedulerOptions {
  readonly seed: number;
}

interface AmbientWaveCollectInput {
  readonly ambient: number;
  readonly cameraRect: DomainRect;
  readonly fields: CompiledWaveFields;
  readonly ignition: number;
  readonly scene: WaveSceneModel;
  readonly nowSeconds: number;
  readonly viewport: {
    readonly h: number;
    readonly w: number;
  };
}

const REFERENCE_SHORT_EDGE_CSS_PX = 462;
const WAVE_INTERVAL_SECONDS = {
  max: 2.2,
  min: 0.55,
};
const RECENT_ORIGIN_LIMIT = 6;

export function createAmbientWaveScheduler(options: AmbientWaveSchedulerOptions) {
  const random = createRandom(options.seed);
  const recentOrigins: Vec2[] = [];
  let initialized = false;
  let nextWaveSeconds = 0;

  return {
    collect(input: AmbientWaveCollectInput): ReadonlyArray<WaveImpulse> {
      if (!initialized) {
        initialized = true;
        nextWaveSeconds = input.nowSeconds + randomRange(random, 0.45, 1.2);
      }

      if (input.nowSeconds < nextWaveSeconds) {
        return [];
      }

      nextWaveSeconds =
        input.nowSeconds +
        randomRange(random, WAVE_INTERVAL_SECONDS.min, WAVE_INTERVAL_SECONDS.max);
      const wave = createAmbientWave(random, input, recentOrigins);
      if (wave === undefined) {
        return [];
      }

      rememberOrigin(recentOrigins, wave.origin);
      return [toImpulse(wave)];
    },
  };
}

function createAmbientWave(
  random: RandomSource,
  input: AmbientWaveCollectInput,
  recentOrigins: readonly Vec2[],
): AmbientWave | undefined {
  const origin = chooseVisibleSpawnOrigin(random, input, recentOrigins);
  if (origin === undefined) return undefined;

  const direction = flowDirectionTowardCamera(origin, input.cameraRect, random);
  const sign = random() < 0.5 ? -1 : 1;
  const projectedVisibility = projectedVisibilityAlongPath(origin, direction, input.fields);
  const waveScale = viewportWaveScale(input.viewport);
  const baseStrength =
    randomRange(random, 0.026, 0.052) *
    (0.82 + input.ambient * 0.54 + input.ignition * 0.34) *
    Math.max(projectedVisibility, 0.45);

  return {
    direction,
    frontLength: randomRange(random, 0.24, 0.46) * waveScale,
    origin,
    radius: randomRange(random, 0.018, 0.032) * interpolate(0.74, 1, waveScale),
    strength: sign * baseStrength * randomRange(random, 0.9, 1.12),
  };
}

function chooseVisibleSpawnOrigin(
  random: RandomSource,
  input: AmbientWaveCollectInput,
  recentOrigins: readonly Vec2[],
): Vec2 | undefined {
  const minOriginDistance = 0.22 * interpolate(0.72, 1, viewportWaveScale(input.viewport));
  let bestCandidate: Vec2 | undefined;
  let bestScore = 0;

  for (let attempt = 0; attempt < 24; attempt += 1) {
    const candidate = weightedRandomSpawnPoint(input.scene, input.fields, random);
    if (candidate === undefined) return undefined;

    const absorption = input.fields.absorptionAt(candidate);
    const direction = flowDirectionTowardCamera(candidate, input.cameraRect, random);
    const projectedVisibility = projectedVisibilityAlongPath(candidate, direction, input.fields);
    const score =
      input.fields.spawnAt(candidate) *
      projectedVisibility *
      (1 - Math.min(absorption, 0.84)) *
      recentOriginPenalty(candidate, recentOrigins, minOriginDistance);
    if (score > bestScore) {
      bestCandidate = candidate;
      bestScore = score;
    }
    if (score > randomRange(random, 0.16, 0.64)) {
      return candidate;
    }
  }

  return bestCandidate;
}

function toImpulse(wave: AmbientWave): WaveImpulse {
  return {
    frontLength: wave.frontLength,
    kind: "front",
    normalX: wave.direction.x,
    normalY: wave.direction.y,
    radius: wave.radius,
    strength: wave.strength,
    x: wave.origin.x,
    y: wave.origin.y,
  };
}

function viewportWaveScale(viewport: { readonly h: number; readonly w: number }): number {
  const shortEdgeScale = Math.min(viewport.w, viewport.h) / REFERENCE_SHORT_EDGE_CSS_PX;
  return Math.min(1, Math.max(0.52, Math.sqrt(shortEdgeScale)));
}

function recentOriginPenalty(
  candidate: Vec2,
  recentOrigins: readonly Vec2[],
  minOriginDistance: number,
): number {
  let penalty = 1;
  for (const origin of recentOrigins) {
    const distance = Math.hypot(candidate.x - origin.x, candidate.y - origin.y);
    if (distance < minOriginDistance) {
      return 0;
    }
    penalty *= smoothstep(minOriginDistance, minOriginDistance * 1.75, distance);
  }
  return penalty;
}

function rememberOrigin(recentOrigins: Vec2[], origin: Vec2) {
  recentOrigins.push(origin);
  while (recentOrigins.length > RECENT_ORIGIN_LIMIT) {
    recentOrigins.shift();
  }
}

function interpolate(start: number, end: number, amount: number): number {
  return start + (end - start) * amount;
}

function smoothstep(edge0: number, edge1: number, value: number): number {
  const amount = Math.min(1, Math.max(0, (value - edge0) / (edge1 - edge0)));
  return amount * amount * (3 - 2 * amount);
}
