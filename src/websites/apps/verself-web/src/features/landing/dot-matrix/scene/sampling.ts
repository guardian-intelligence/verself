import { rectCenter, sampleShape, shapeBounds } from "./shapes";
import type {
  CompiledWaveFields,
  DomainRect,
  Vec2,
  WaveFieldRegion,
  WaveSceneModel,
} from "./types";

export type RandomSource = () => number;

export function createRandom(seed: number): RandomSource {
  let state = seed >>> 0;
  return () => {
    state += 0x6d2b79f5;
    let value = state;
    value = Math.imul(value ^ (value >>> 15), value | 1);
    value ^= value + Math.imul(value ^ (value >>> 7), value | 61);
    return ((value ^ (value >>> 14)) >>> 0) / 4294967296;
  };
}

export function randomRange(random: RandomSource, min: number, max: number): number {
  return min + (max - min) * random();
}

export function weightedRandomSpawnPoint(
  model: WaveSceneModel,
  fields: CompiledWaveFields,
  random: RandomSource,
): Vec2 | undefined {
  const spawnRegions = model.regions.filter(
    (region) => region.field === "spawn" && region.weight > 0,
  );
  if (spawnRegions.length === 0) return undefined;

  for (let attempt = 0; attempt < 24; attempt += 1) {
    const region = chooseWeightedRegion(spawnRegions, random);
    const bounds = shapeBounds(region.shape);
    const point = {
      x: randomRange(random, bounds.x, bounds.x + bounds.width),
      y: randomRange(random, bounds.y, bounds.y + bounds.height),
    };
    const localSpawn = sampleShape(region.shape, point, region.feather);
    if (localSpawn < random()) continue;
    if (fields.spawnAt(point) < random() * 0.55) continue;
    return point;
  }

  return undefined;
}

export function flowDirectionTowardCamera(
  point: Vec2,
  cameraRect: DomainRect,
  random: RandomSource,
): Vec2 {
  const center = rectCenter(cameraRect);
  const jitter = randomRange(random, -0.34, 0.34);
  const dx = center.x - point.x;
  const dy = center.y - point.y;
  const length = Math.max(Math.sqrt(dx * dx + dy * dy), 0.0001);
  const nx = dx / length;
  const ny = dy / length;
  return normalize({
    x: nx * Math.cos(jitter) - ny * Math.sin(jitter),
    y: nx * Math.sin(jitter) + ny * Math.cos(jitter),
  });
}

export function projectedVisibilityAlongPath(
  start: Vec2,
  direction: Vec2,
  fields: CompiledWaveFields,
): number {
  let total = 0;
  let weight = 0;

  for (let index = 1; index <= 9; index += 1) {
    const distance = index * 0.055;
    const point = {
      x: clamp01(start.x + direction.x * distance),
      y: clamp01(start.y + direction.y * distance),
    };
    const sampleWeight = index <= 5 ? 1 : 0.62;
    total += fields.visibilityAt(point) * (1 - fields.readabilityAt(point) * 0.35) * sampleWeight;
    weight += sampleWeight;
  }

  return weight > 0 ? total / weight : 0;
}

function chooseWeightedRegion(
  regions: readonly WaveFieldRegion[],
  random: RandomSource,
): WaveFieldRegion {
  let total = 0;
  const weights = regions.map((region) => {
    const bounds = shapeBounds(region.shape);
    const area = Math.max(bounds.width * bounds.height, 0.0001);
    const weight = area * Math.max(region.weight, 0.001);
    total += weight;
    return weight;
  });

  let threshold = random() * total;
  for (let index = 0; index < regions.length; index += 1) {
    threshold -= weights[index] ?? 0;
    if (threshold <= 0) {
      const region = regions[index];
      if (region !== undefined) return region;
    }
  }

  const fallback = regions[regions.length - 1];
  if (fallback === undefined) {
    throw new Error("weighted region selection requires at least one region");
  }
  return fallback;
}

function normalize(vector: Vec2): Vec2 {
  const length = Math.max(Math.sqrt(vector.x * vector.x + vector.y * vector.y), 0.0001);
  return {
    x: vector.x / length,
    y: vector.y / length,
  };
}

function clamp01(value: number): number {
  return Math.min(1, Math.max(0, value));
}
