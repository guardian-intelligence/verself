import { describe, expect, it } from "vite-plus/test";

import { sampleDotMatrixTimeline } from "./timeline.ts";

describe("dot matrix timeline", () => {
  it("keeps phase monotonic across startup and ambient playback", () => {
    let previous = sampleDotMatrixTimeline(0).phase;

    for (let time = 0.05; time <= 10; time += 0.05) {
      const sample = sampleDotMatrixTimeline(time);
      expect(sample.phase).toBeGreaterThanOrEqual(previous);
      previous = sample.phase;
    }
  });

  it("settles into a steady ambient phase velocity after the authored knots", () => {
    const start = sampleDotMatrixTimeline(6).phase;
    const end = sampleDotMatrixTimeline(7).phase;

    expect(end - start).toBeCloseTo(0.504, 5);
  });

  it("slows the opening acceleration by the same phase scale", () => {
    const firstAccelerationKnot = sampleDotMatrixTimeline(0.35).phase;

    expect(firstAccelerationKnot).toBeCloseTo(0.322, 5);
  });

  it("separates startup ignition from ongoing ambient intensity", () => {
    const startup = sampleDotMatrixTimeline(0.3);
    const ambient = sampleDotMatrixTimeline(6);

    expect(startup.ignition).toBeGreaterThan(ambient.ignition);
    expect(ambient.ignition).toBe(0);
    expect(ambient.ambient).toBeGreaterThan(0);
  });
});
