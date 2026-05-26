import {
  createRandom,
  projectedVisibilityAlongPath,
  randomRange,
  type RandomSource,
} from "../scene/sampling";
import type { CompiledWaveFields, DomainRect, Vec2 } from "../scene/types";
import type { WaveImpulse } from "./types";

interface AmbientWave {
  readonly direction: Vec2;
  readonly frontLength: number;
  readonly origin: Vec2;
  readonly radius: number;
  readonly strength: number;
  readonly wavelength: number;
}

interface AmbientWaveSchedulerOptions {
  readonly seed: number;
}

interface AmbientWaveCollectInput {
  readonly ambient: number;
  readonly cameraRect: DomainRect;
  readonly fields: CompiledWaveFields;
  readonly focusPoint: Vec2;
  readonly ignition: number;
  readonly nowSeconds: number;
  readonly viewport: {
    readonly h: number;
    readonly w: number;
  };
}

const REFERENCE_SHORT_EDGE_CSS_PX = 462;
const WAVE_INTERVAL_SECONDS = {
  max: 8.8,
  min: 4.4,
};
const AMBIENT_FRONT_THICKNESS_SCALE = 0.1;
const AMBIENT_STRENGTH_SCALE = 2.6;
const FULL_SCREEN_FRONT_LENGTH_SCALE = { max: 1.24, min: 1.05 };
const HALO_FOCUS_AIM_JITTER = 0.055;
const HALO_SOURCE_MIN_OFFSET = 0.035;
const HALO_SOURCE_OFFSET_SCALE = 4.5;
const WAVE_TRAIN_FRONT_COUNT = 5;
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
        nextWaveSeconds = input.nowSeconds + randomRange(random, 0.8, 1.8);
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
      return toImpulses(wave);
    },
  };
}

function createAmbientWave(
  random: RandomSource,
  input: AmbientWaveCollectInput,
  recentOrigins: readonly Vec2[],
): AmbientWave | undefined {
  const sign = random() < 0.5 ? -1 : 1;
  const waveScale = viewportWaveScale(input.viewport);
  const radius =
    randomRange(random, 0.018, 0.032) *
    AMBIENT_FRONT_THICKNESS_SCALE *
    interpolate(0.74, 1, waveScale);
  const focusPoint = clampToRect(input.focusPoint, input.cameraRect, 0.08);
  const frontLength =
    Math.hypot(input.cameraRect.width, input.cameraRect.height) *
    randomRange(random, FULL_SCREEN_FRONT_LENGTH_SCALE.min, FULL_SCREEN_FRONT_LENGTH_SCALE.max);
  const sweep = chooseHaloSweep(random, input, recentOrigins, radius, focusPoint);
  if (sweep === undefined) return undefined;

  const projectedVisibility = projectedVisibilityAlongPath(
    sweep.origin,
    sweep.direction,
    input.fields,
  );
  const baseStrength =
    randomRange(random, 0.018, 0.04) *
    AMBIENT_STRENGTH_SCALE *
    (0.78 + input.ambient * 0.42 + input.ignition * 0.22) *
    Math.max(projectedVisibility, 0.45);

  return {
    direction: sweep.direction,
    frontLength: Math.max(frontLength, radius * 1.7),
    origin: sweep.origin,
    radius,
    strength: sign * baseStrength * randomRange(random, 0.9, 1.12),
    wavelength: Math.max(radius * 6.4, 0.015 * waveScale),
  };
}

function chooseHaloSweep(
  random: RandomSource,
  input: AmbientWaveCollectInput,
  recentOrigins: readonly Vec2[],
  radius: number,
  focusPoint: Vec2,
): { readonly direction: Vec2; readonly origin: Vec2 } | undefined {
  const waveScale = viewportWaveScale(input.viewport);
  const minOriginDistance = 0.28 * interpolate(0.72, 1, waveScale);
  let bestCandidate: { readonly direction: Vec2; readonly origin: Vec2 } | undefined;
  let bestScore = 0;

  for (let attempt = 0; attempt < 24; attempt += 1) {
    const candidate = haloSweepCandidate(random, input.cameraRect, radius, focusPoint, waveScale);
    const projectedVisibility = projectedVisibilityAlongPath(
      candidate.origin,
      candidate.direction,
      input.fields,
    );
    const entryPoint = projectToCamera(candidate.origin, candidate.direction, input.cameraRect);
    const absorption = input.fields.absorptionAt(entryPoint);
    const score =
      projectedVisibility *
      (1 - Math.min(absorption, 0.84)) *
      recentOriginPenalty(candidate.origin, recentOrigins, minOriginDistance);
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

function haloSweepCandidate(
  random: RandomSource,
  cameraRect: DomainRect,
  radius: number,
  focusPoint: Vec2,
  waveScale: number,
): { readonly direction: Vec2; readonly origin: Vec2 } {
  const aimPoint = jitterFocusPoint(random, cameraRect, focusPoint, waveScale);
  const angle = randomRange(random, 0, Math.PI * 2);
  const outward = {
    x: Math.cos(angle),
    y: Math.sin(angle),
  };
  const boundaryDistance = distanceToRectBoundary(aimPoint, outward, cameraRect);
  const offset =
    Math.max(radius * HALO_SOURCE_OFFSET_SCALE, HALO_SOURCE_MIN_OFFSET * waveScale) +
    randomRange(random, 0.012, 0.052) * waveScale;
  const origin = {
    x: aimPoint.x + outward.x * (boundaryDistance + offset),
    y: aimPoint.y + outward.y * (boundaryDistance + offset),
  };

  return {
    direction: normalize({
      x: aimPoint.x - origin.x,
      y: aimPoint.y - origin.y,
    }),
    origin,
  };
}

function jitterFocusPoint(
  random: RandomSource,
  cameraRect: DomainRect,
  focusPoint: Vec2,
  waveScale: number,
): Vec2 {
  const angle = randomRange(random, 0, Math.PI * 2);
  const radius = randomRange(random, 0, HALO_FOCUS_AIM_JITTER * waveScale);
  return clampToRect(
    {
      x: focusPoint.x + Math.cos(angle) * radius,
      y: focusPoint.y + Math.sin(angle) * radius * 0.72,
    },
    cameraRect,
    0.05,
  );
}

function distanceToRectBoundary(point: Vec2, direction: Vec2, rect: DomainRect): number {
  const distances: number[] = [];
  if (direction.x > 0) {
    distances.push((rect.x + rect.width - point.x) / direction.x);
  } else if (direction.x < 0) {
    distances.push((rect.x - point.x) / direction.x);
  }
  if (direction.y > 0) {
    distances.push((rect.y + rect.height - point.y) / direction.y);
  } else if (direction.y < 0) {
    distances.push((rect.y - point.y) / direction.y);
  }

  const positiveDistances = distances.filter((distance) => distance >= 0);
  if (positiveDistances.length === 0) {
    return Math.hypot(rect.width, rect.height) * 0.5;
  }

  return Math.min(...positiveDistances);
}

function projectToCamera(origin: Vec2, direction: Vec2, cameraRect: DomainRect): Vec2 {
  if (containsPoint(cameraRect, origin)) return origin;

  const distances: number[] = [];
  if (origin.x < cameraRect.x && direction.x > 0) {
    distances.push((cameraRect.x - origin.x) / direction.x);
  }
  if (origin.x > cameraRect.x + cameraRect.width && direction.x < 0) {
    distances.push((cameraRect.x + cameraRect.width - origin.x) / direction.x);
  }
  if (origin.y < cameraRect.y && direction.y > 0) {
    distances.push((cameraRect.y - origin.y) / direction.y);
  }
  if (origin.y > cameraRect.y + cameraRect.height && direction.y < 0) {
    distances.push((cameraRect.y + cameraRect.height - origin.y) / direction.y);
  }

  const distance = Math.min(...distances.filter((candidate) => candidate >= 0));
  if (!Number.isFinite(distance)) {
    return clampPointToRect(origin, cameraRect);
  }

  return {
    x: clampRange(origin.x + direction.x * distance, cameraRect.x, cameraRect.x + cameraRect.width),
    y: clampRange(
      origin.y + direction.y * distance,
      cameraRect.y,
      cameraRect.y + cameraRect.height,
    ),
  };
}

function toImpulses(wave: AmbientWave): ReadonlyArray<WaveImpulse> {
  return Array.from({ length: WAVE_TRAIN_FRONT_COUNT }, (_, index) => {
    const trailingDistance = index * wave.wavelength;
    const decay = Math.pow(0.76, index);
    return {
      frontLength: wave.frontLength,
      kind: "front",
      normalX: wave.direction.x,
      normalY: wave.direction.y,
      radius: wave.radius,
      strength: wave.strength * decay,
      x: wave.origin.x - wave.direction.x * trailingDistance,
      y: wave.origin.y - wave.direction.y * trailingDistance,
    };
  });
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

function containsPoint(rect: DomainRect, point: Vec2): boolean {
  return (
    point.x >= rect.x &&
    point.x <= rect.x + rect.width &&
    point.y >= rect.y &&
    point.y <= rect.y + rect.height
  );
}

function clampPointToRect(point: Vec2, rect: DomainRect): Vec2 {
  return {
    x: clampRange(point.x, rect.x, rect.x + rect.width),
    y: clampRange(point.y, rect.y, rect.y + rect.height),
  };
}

function clampToRect(point: Vec2, rect: DomainRect, marginScale: number): Vec2 {
  const marginX = rect.width * marginScale;
  const marginY = rect.height * marginScale;
  return {
    x: clampRange(point.x, rect.x + marginX, rect.x + rect.width - marginX),
    y: clampRange(point.y, rect.y + marginY, rect.y + rect.height - marginY),
  };
}

function clampRange(value: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, value));
}

function normalize(vector: Vec2): Vec2 {
  const length = Math.max(Math.hypot(vector.x, vector.y), 0.0001);
  return {
    x: vector.x / length,
    y: vector.y / length,
  };
}
