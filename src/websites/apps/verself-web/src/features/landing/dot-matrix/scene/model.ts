import type { CompiledWaveFields, DomainRect, LandingWaveScene, Vec2 } from "./types";

interface LandingWaveSceneInput {
  readonly h: number;
  readonly w: number;
}

const UNIT_DOMAIN: DomainRect = {
  height: 1,
  width: 1,
  x: 0,
  y: 0,
};

const SPAWN_BAND = 0.064;
const SPAWN_FEATHER = 0.02;
const EDGE_ABSORPTION_BAND = 0.018;
const EDGE_ABSORPTION_FEATHER = 0.055;

export function createLandingWaveScene(_viewport: LandingWaveSceneInput): LandingWaveScene {
  return {
    cameraRect: UNIT_DOMAIN,
    fields: createLandingWaveFields(),
  };
}

function createLandingWaveFields(): CompiledWaveFields {
  const absorptionAt = (point: Vec2) => sampleAbsorption(point);
  const spawnAt = (point: Vec2) => sampleSpawn(point);
  const visibilityAt = (point: Vec2) => (containsUnitPoint(point) ? 1 : 0);
  const zero = () => 0;

  return {
    absorptionAt,
    at(point) {
      return {
        absorption: absorptionAt(point),
        readability: 0,
        spawn: spawnAt(point),
        visibility: visibilityAt(point),
      };
    },
    readabilityAt: zero,
    spawnAt,
    visibilityAt,
  };
}

function sampleAbsorption(point: Vec2): number {
  if (!containsUnitPoint(point)) return 1;

  const edgeDistance = Math.min(point.x, 1 - point.x, point.y, 1 - point.y);
  if (edgeDistance <= EDGE_ABSORPTION_BAND) return 1;
  if (edgeDistance >= EDGE_ABSORPTION_BAND + EDGE_ABSORPTION_FEATHER) return 0;
  return (
    1 -
    smoothstep(EDGE_ABSORPTION_BAND, EDGE_ABSORPTION_BAND + EDGE_ABSORPTION_FEATHER, edgeDistance)
  );
}

function sampleSpawn(point: Vec2): number {
  if (!containsUnitPoint(point)) return 0;

  const edgeDistance = Math.min(point.x, 1 - point.x, point.y, 1 - point.y);
  if (edgeDistance <= SPAWN_BAND) return 1;
  if (edgeDistance >= SPAWN_BAND + SPAWN_FEATHER) return 0;
  return 1 - smoothstep(SPAWN_BAND, SPAWN_BAND + SPAWN_FEATHER, edgeDistance);
}

function containsUnitPoint(point: Vec2): boolean {
  return point.x >= 0 && point.x <= 1 && point.y >= 0 && point.y <= 1;
}

function smoothstep(edge0: number, edge1: number, value: number): number {
  const t = Math.min(1, Math.max(0, (value - edge0) / (edge1 - edge0)));
  return t * t * (3 - 2 * t);
}
