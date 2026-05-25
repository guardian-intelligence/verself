import { describe, expect, it } from "vite-plus/test";

import { createCompiledWaveScene } from "../scene/compile.ts";
import { createLandingWaveScene } from "../scene/model.ts";
import { createAmbientWaveScheduler } from "./ambient.ts";

describe("ambient wave scheduler", () => {
  it("emits one cohesive bounded impulse per scheduled wave", () => {
    const scheduler = createAmbientWaveScheduler({ seed: 1234 });
    const scene = createLandingWaveScene({
      viewport: { h: 900, w: 1440 },
    });
    const compiledScene = createCompiledWaveScene(scene, { height: 16, width: 16 });

    expect(
      scheduler.collect({
        ambient: 0.8,
        cameraRect: compiledScene.cameraRect,
        fields: compiledScene.fields,
        ignition: 0.1,
        nowSeconds: 0,
        scene,
        viewport: { h: 900, w: 1440 },
      }),
    ).toEqual([]);
    const impulses = scheduler.collect({
      ambient: 0.8,
      cameraRect: compiledScene.cameraRect,
      fields: compiledScene.fields,
      ignition: 0.1,
      nowSeconds: 1.3,
      scene,
      viewport: { h: 900, w: 1440 },
    });

    expect(impulses.length).toBe(1);
    for (const impulse of impulses) {
      expect(impulse.x).toBeGreaterThanOrEqual(0);
      expect(impulse.x).toBeLessThanOrEqual(1);
      expect(impulse.y).toBeGreaterThanOrEqual(0);
      expect(impulse.y).toBeLessThanOrEqual(1);
      expect(impulse.radius).toBeGreaterThan(0);
      expect(impulse.kind).toBe("front");
      expect(impulse.frontLength).toEqual(expect.any(Number));
      expect(impulse.frontLength ?? 0).toBeGreaterThan(impulse.radius);
      expect(Math.hypot(impulse.normalX ?? 0, impulse.normalY ?? 0)).toBeCloseTo(1, 5);
      expect(Math.abs(impulse.strength)).toBeGreaterThan(0);
    }

    compiledScene.dispose();
  });

  it("does not backfill multiple waves after a long frame", () => {
    const scheduler = createAmbientWaveScheduler({ seed: 4321 });
    const scene = createLandingWaveScene({
      viewport: { h: 900, w: 1440 },
    });
    const compiledScene = createCompiledWaveScene(scene, { height: 16, width: 16 });

    const impulses = scheduler.collect({
      ambient: 0.8,
      cameraRect: compiledScene.cameraRect,
      fields: compiledScene.fields,
      ignition: 0.1,
      nowSeconds: 10,
      scene,
      viewport: { h: 900, w: 1440 },
    });

    expect(impulses.length).toBeLessThanOrEqual(1);
    compiledScene.dispose();
  });

  it("scales front size down for small screens", () => {
    const scene = createLandingWaveScene({
      viewport: { h: 900, w: 1440 },
    });
    const compiledScene = createCompiledWaveScene(scene, { height: 16, width: 16 });
    const desktopScheduler = createAmbientWaveScheduler({ seed: 2468 });
    const mobileScheduler = createAmbientWaveScheduler({ seed: 2468 });
    const baseInput = {
      ambient: 0.8,
      cameraRect: compiledScene.cameraRect,
      fields: compiledScene.fields,
      ignition: 0.1,
      scene,
    };

    desktopScheduler.collect({
      ...baseInput,
      nowSeconds: 0,
      viewport: { h: 900, w: 1440 },
    });
    mobileScheduler.collect({
      ...baseInput,
      nowSeconds: 0,
      viewport: { h: 844, w: 390 },
    });
    const desktopImpulse = desktopScheduler.collect({
      ...baseInput,
      nowSeconds: 1.3,
      viewport: { h: 900, w: 1440 },
    })[0];
    const mobileImpulse = mobileScheduler.collect({
      ...baseInput,
      nowSeconds: 1.3,
      viewport: { h: 844, w: 390 },
    })[0];

    expect(desktopImpulse?.frontLength).toEqual(expect.any(Number));
    expect(mobileImpulse?.frontLength).toEqual(expect.any(Number));
    expect(mobileImpulse?.frontLength ?? 0).toBeLessThan(desktopImpulse?.frontLength ?? 0);
    compiledScene.dispose();
  });
});
