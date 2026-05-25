import type { CompiledWaveFields, Vec2 } from "./types";

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

function clamp01(value: number): number {
  return Math.min(1, Math.max(0, value));
}
