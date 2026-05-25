import type { DomainRect, WaveFieldRegion, WaveSceneModel } from "./types";

interface LandingWaveSceneInput {
  readonly viewport: {
    readonly h: number;
    readonly w: number;
  };
}

const simulationDomain: DomainRect = {
  height: 1,
  width: 1,
  x: 0,
  y: 0,
};

export function createLandingWaveScene(input: LandingWaveSceneInput): WaveSceneModel {
  const cameraRect = resolveLandingWaveCameraRect(input.viewport);
  return {
    regions: [
      sceneRect("simulation", "simulation-domain", simulationDomain),
      sceneRect("camera", "primary-camera-rect", cameraRect),
      ...baseRegions(cameraRect),
    ],
  };
}

export function resolveLandingWaveCameraRect(
  _viewport: LandingWaveSceneInput["viewport"],
): DomainRect {
  return simulationDomain;
}

function baseRegions(cameraRect: DomainRect): readonly WaveFieldRegion[] {
  const spawnBand = 0.064;

  return [
    ...visibilityRegions(cameraRect),
    spawnRect("left", {
      height: 1,
      width: spawnBand,
      x: 0,
      y: 0,
    }),
    spawnRect("right", { height: 1, width: spawnBand, x: 1 - spawnBand, y: 0 }),
    spawnRect("bottom", {
      height: spawnBand,
      width: 1,
      x: 0,
      y: 0,
    }),
    spawnRect("top", { height: spawnBand, width: 1, x: 0, y: 1 - spawnBand }),
  ];
}

function visibilityRegions(cameraRect: DomainRect): readonly WaveFieldRegion[] {
  return [
    {
      combine: "max",
      feather: 0,
      field: "visibility",
      id: "full-screen-dot-visibility",
      shape: {
        kind: "rect",
        rect: cameraRect,
      },
      weight: 1,
    },
  ];
}

function spawnRect(id: string, rect: DomainRect): WaveFieldRegion {
  return {
    combine: "max",
    feather: 0.02,
    field: "spawn",
    id: `hero-spawn-${id}-halo`,
    shape: { kind: "rect", rect },
    weight: 1,
  };
}

function sceneRect(field: "camera" | "simulation", id: string, rect: DomainRect): WaveFieldRegion {
  return {
    combine: "max",
    feather: 0,
    field,
    id,
    shape: { kind: "rect", rect },
    weight: 1,
  };
}
