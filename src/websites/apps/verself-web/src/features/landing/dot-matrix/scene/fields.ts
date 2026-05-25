import { expandRect, sampleShape, shapeBounds } from "./shapes";
import type {
  CompiledWaveFields,
  DomainRect,
  Vec2,
  WaveFieldKind,
  WaveRegionKind,
  WaveFieldRegion,
  WaveFieldSample,
} from "./types";

interface CompiledWaveFieldRegion {
  readonly bounds: DomainRect;
  readonly region: WaveFieldRegion;
}

type WaveFieldBuckets = Record<WaveFieldKind, readonly CompiledWaveFieldRegion[]>;

export function compileWaveFields(regions: readonly WaveFieldRegion[]): CompiledWaveFields {
  const buckets = bucketWaveFieldRegions(regions);
  const fieldAt = (field: WaveFieldKind, point: Vec2) => sampleField(buckets[field], point);

  return {
    absorptionAt: (point) => fieldAt("absorption", point),
    at(point): WaveFieldSample {
      return {
        absorption: fieldAt("absorption", point),
        readability: fieldAt("readability", point),
        spawn: fieldAt("spawn", point),
        visibility: fieldAt("visibility", point),
      };
    },
    readabilityAt: (point) => fieldAt("readability", point),
    spawnAt: (point) => fieldAt("spawn", point),
    visibilityAt: (point) => fieldAt("visibility", point),
  };
}

function bucketWaveFieldRegions(regions: readonly WaveFieldRegion[]): WaveFieldBuckets {
  const buckets: Record<WaveFieldKind, CompiledWaveFieldRegion[]> = {
    absorption: [],
    readability: [],
    spawn: [],
    visibility: [],
  };

  for (const region of regions) {
    if (!isWaveFieldKind(region.field)) continue;
    buckets[region.field].push({
      bounds: expandRect(shapeBounds(region.shape), {
        x: Math.max(region.feather, 0),
        y: Math.max(region.feather, 0),
      }),
      region,
    });
  }

  return buckets;
}

function sampleField(regions: readonly CompiledWaveFieldRegion[], point: Vec2): number {
  let value = 0;

  for (const { bounds, region } of regions) {
    if (!containsPoint(bounds, point)) {
      if (region.combine === "multiply") {
        value = 0;
      }
      continue;
    }
    const contribution = clamp01(sampleShape(region.shape, point, region.feather) * region.weight);
    switch (region.combine) {
      case "add":
        value += contribution;
        break;
      case "max":
        value = Math.max(value, contribution);
        break;
      case "multiply":
        value *= contribution;
        break;
      case "subtract":
        value -= contribution;
        break;
    }
  }

  return clamp01(value);
}

function isWaveFieldKind(field: WaveRegionKind): field is WaveFieldKind {
  return (
    field === "absorption" || field === "readability" || field === "spawn" || field === "visibility"
  );
}

function containsPoint(rect: DomainRect, point: Vec2): boolean {
  return (
    point.x >= rect.x &&
    point.x <= rect.x + rect.width &&
    point.y >= rect.y &&
    point.y <= rect.y + rect.height
  );
}

function clamp01(value: number): number {
  return Math.min(1, Math.max(0, value));
}
