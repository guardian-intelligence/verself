import { describe, expect, it } from "vite-plus/test";

import { createCompiledWaveScene } from "./compile.ts";
import { compileWaveFields } from "./fields.ts";
import type { WaveFieldRegion } from "./types.ts";

describe("wave scene fields", () => {
  it("composes plural intersecting regions by field kind", () => {
    const regions: WaveFieldRegion[] = [
      {
        combine: "max",
        feather: 0,
        field: "visibility",
        id: "visible-a",
        shape: { kind: "rect", rect: { height: 0.5, width: 0.5, x: 0.1, y: 0.1 } },
        weight: 0.6,
      },
      {
        combine: "max",
        feather: 0,
        field: "visibility",
        id: "visible-b",
        shape: { kind: "rect", rect: { height: 0.5, width: 0.5, x: 0.3, y: 0.3 } },
        weight: 0.9,
      },
      {
        combine: "max",
        feather: 0,
        field: "absorption",
        id: "readable-absorption",
        shape: { kind: "rect", rect: { height: 0.2, width: 0.2, x: 0.35, y: 0.35 } },
        weight: 0.7,
      },
    ];

    const fields = compileWaveFields(regions);
    const sample = fields.at({ x: 0.4, y: 0.4 });

    expect(sample.visibility).toBe(0.9);
    expect(sample.absorption).toBe(0.7);
    expect(sample.spawn).toBe(0);
  });

  it("feathers outside shape boundaries", () => {
    const fields = compileWaveFields([
      {
        combine: "max",
        feather: 0.1,
        field: "spawn",
        id: "spawn-halo",
        shape: { kind: "rect", rect: { height: 0.2, width: 0.2, x: 0.4, y: 0.4 } },
        weight: 1,
      },
    ]);

    expect(fields.spawnAt({ x: 0.5, y: 0.5 })).toBe(1);
    expect(fields.spawnAt({ x: 0.65, y: 0.5 })).toBeGreaterThan(0);
    expect(fields.spawnAt({ x: 0.8, y: 0.5 })).toBe(0);
  });

  it("preserves multiply semantics when a region is out of range", () => {
    const fields = compileWaveFields([
      {
        combine: "add",
        feather: 0,
        field: "visibility",
        id: "base",
        shape: { kind: "rect", rect: { height: 1, width: 1, x: 0, y: 0 } },
        weight: 1,
      },
      {
        combine: "multiply",
        feather: 0,
        field: "visibility",
        id: "gate",
        shape: { kind: "rect", rect: { height: 0.2, width: 0.2, x: 0.4, y: 0.4 } },
        weight: 1,
      },
    ]);

    expect(fields.visibilityAt({ x: 0.5, y: 0.5 })).toBe(1);
    expect(fields.visibilityAt({ x: 0.2, y: 0.2 })).toBe(0);
  });

  it("derives camera and simulation rects from plural scene regions", () => {
    const compiledScene = createCompiledWaveScene(
      {
        regions: [
          {
            combine: "max",
            feather: 0,
            field: "simulation",
            id: "simulation-a",
            shape: { kind: "rect", rect: { height: 1, width: 0.7, x: 0, y: 0 } },
            weight: 1,
          },
          {
            combine: "max",
            feather: 0,
            field: "simulation",
            id: "simulation-b",
            shape: { kind: "rect", rect: { height: 1, width: 0.3, x: 0.7, y: 0 } },
            weight: 1,
          },
          {
            combine: "max",
            feather: 0,
            field: "camera",
            id: "camera-left",
            shape: { kind: "rect", rect: { height: 0.6, width: 0.25, x: 0.2, y: 0.2 } },
            weight: 1,
          },
          {
            combine: "max",
            feather: 0,
            field: "camera",
            id: "camera-right",
            shape: { kind: "rect", rect: { height: 0.6, width: 0.25, x: 0.5, y: 0.2 } },
            weight: 1,
          },
        ],
      },
      { height: 8, width: 8 },
    );

    expect(compiledScene.simulationDomain).toEqual({ height: 1, width: 1, x: 0, y: 0 });
    expect(compiledScene.cameraRect.x).toBeCloseTo(0.2);
    expect(compiledScene.cameraRect.y).toBeCloseTo(0.2);
    expect(compiledScene.cameraRect.width).toBeCloseTo(0.55);
    expect(compiledScene.cameraRect.height).toBeCloseTo(0.6);

    compiledScene.dispose();
  });
});
