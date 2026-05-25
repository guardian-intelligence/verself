import { describe, expect, it } from "vite-plus/test";

import { createCompiledWaveScene } from "../scene/compile.ts";
import { createLandingWaveScene } from "../scene/model.ts";
import { createAmbientWaveScheduler } from "./ambient.ts";

describe("ambient wave scheduler", () => {
  const focusPoint = { x: 0.5, y: 0.62 };

  it("emits one cohesive bounded logo-bound wave train per scheduled wave", () => {
    const scheduler = createAmbientWaveScheduler({ seed: 1234 });
    const scene = createLandingWaveScene({ h: 900, w: 1440 });
    const compiledScene = createCompiledWaveScene(scene, { height: 16, width: 16 });

    expect(
      scheduler.collect({
        ambient: 0.8,
        cameraRect: compiledScene.cameraRect,
        fields: compiledScene.fields,
        focusPoint,
        ignition: 0.1,
        nowSeconds: 0,
        viewport: { h: 900, w: 1440 },
      }),
    ).toEqual([]);
    const impulses = scheduler.collect({
      ambient: 0.8,
      cameraRect: compiledScene.cameraRect,
      fields: compiledScene.fields,
      focusPoint,
      ignition: 0.1,
      nowSeconds: 1.3,
      viewport: { h: 900, w: 1440 },
    });

    expect(impulses.length).toBeGreaterThan(1);
    expect(impulses.length).toBeLessThanOrEqual(6);
    for (const impulse of impulses) {
      expect(impulse.x).toBeGreaterThanOrEqual(-0.75);
      expect(impulse.x).toBeLessThanOrEqual(1.75);
      expect(impulse.y).toBeGreaterThanOrEqual(-0.75);
      expect(impulse.y).toBeLessThanOrEqual(1.75);
      expect(impulse.x < 0 || impulse.x > 1 || impulse.y < 0 || impulse.y > 1).toBe(true);
      expect(impulse.radius).toBeGreaterThan(0);
      expect(impulse.kind).toBe("front");
      expect(impulse.frontLength).toEqual(expect.any(Number));
      expect(impulse.frontLength ?? 0).toBeGreaterThan(Math.hypot(1, 1));
      expect(Math.hypot(impulse.normalX ?? 0, impulse.normalY ?? 0)).toBeCloseTo(1, 5);
      expect(
        (impulse.normalX ?? 0) * (focusPoint.x - impulse.x) +
          (impulse.normalY ?? 0) * (focusPoint.y - impulse.y),
      ).toBeGreaterThan(0);
      expect(Math.abs(impulse.strength)).toBeGreaterThan(0);
    }

    compiledScene.dispose();
  });

  it("does not backfill multiple waves after a long frame", () => {
    const scheduler = createAmbientWaveScheduler({ seed: 4321 });
    const scene = createLandingWaveScene({ h: 900, w: 1440 });
    const compiledScene = createCompiledWaveScene(scene, { height: 16, width: 16 });

    const impulses = scheduler.collect({
      ambient: 0.8,
      cameraRect: compiledScene.cameraRect,
      fields: compiledScene.fields,
      focusPoint,
      ignition: 0.1,
      nowSeconds: 10,
      viewport: { h: 900, w: 1440 },
    });

    expect(impulses.length).toBeLessThanOrEqual(1);
    compiledScene.dispose();
  });

  it("keeps fronts screen-spanning while scaling wave thickness down for small screens", () => {
    const scene = createLandingWaveScene({ h: 900, w: 1440 });
    const compiledScene = createCompiledWaveScene(scene, { height: 16, width: 16 });
    const desktopScheduler = createAmbientWaveScheduler({ seed: 2468 });
    const mobileScheduler = createAmbientWaveScheduler({ seed: 2468 });
    const baseInput = {
      ambient: 0.8,
      cameraRect: compiledScene.cameraRect,
      fields: compiledScene.fields,
      focusPoint,
      ignition: 0.1,
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
    expect(desktopImpulse?.frontLength ?? 0).toBeGreaterThan(Math.hypot(1, 1));
    expect(mobileImpulse?.frontLength ?? 0).toBeGreaterThan(Math.hypot(1, 1));
    expect(mobileImpulse?.radius ?? 0).toBeLessThan(desktopImpulse?.radius ?? 0);
    compiledScene.dispose();
  });

  it("varies approach angles around the logo halo", () => {
    const scheduler = createAmbientWaveScheduler({ seed: 97531 });
    const scene = createLandingWaveScene({ h: 900, w: 1440 });
    const compiledScene = createCompiledWaveScene(scene, { height: 16, width: 16 });
    const baseInput = {
      ambient: 0.8,
      cameraRect: compiledScene.cameraRect,
      fields: compiledScene.fields,
      focusPoint,
      ignition: 0.1,
      viewport: { h: 900, w: 1440 },
    };
    const directions: number[] = [];

    scheduler.collect({ ...baseInput, nowSeconds: 0 });
    for (let index = 0; index < 7; index += 1) {
      const impulse = scheduler.collect({ ...baseInput, nowSeconds: 2 + index * 8 })[0];
      if (impulse !== undefined) {
        directions.push(Math.atan2(impulse.normalY ?? 0, impulse.normalX ?? 0));
      }
    }

    const occupiedQuadrants = new Set(
      directions.map((direction) => Math.floor(((direction + Math.PI) / (Math.PI * 2)) * 4)),
    );
    expect(occupiedQuadrants.size).toBeGreaterThanOrEqual(3);
    compiledScene.dispose();
  });
});
