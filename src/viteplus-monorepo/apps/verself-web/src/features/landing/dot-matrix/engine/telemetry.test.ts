import { describe, expect, it } from "vite-plus/test";

import { summarizeFrameDeltas } from "./telemetry.ts";

describe("dot matrix telemetry", () => {
  it("summarizes active frame windows without treating inactive time as FPS", () => {
    const summary = summarizeFrameDeltas([16, 17, 33, 51], 117, 234);

    expect(summary.frameCount).toBe(4);
    expect(summary.fpsMean).toBeCloseTo(34.19, 2);
    expect(summary.activeRatio).toBeCloseTo(0.5, 4);
    expect(summary.budget16Misses).toBe(3);
    expect(summary.budget33Misses).toBe(1);
    expect(summary.longFrames50).toBe(1);
  });

  it("returns an explicit zero summary for empty windows", () => {
    const summary = summarizeFrameDeltas([], 0, 0);

    expect(summary.frameCount).toBe(0);
    expect(summary.fpsMean).toBe(0);
    expect(summary.frameMsP95).toBe(0);
    expect(summary.activeRatio).toBe(0);
  });
});
