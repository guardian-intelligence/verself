import { describe, expect, it } from "vite-plus/test";

import { createFixedTimestepLoop } from "./fixed-timestep.ts";

describe("fixed timestep loop", () => {
  it("advances simulation steps from elapsed wall time", () => {
    const loop = createFixedTimestepLoop({
      maxFrameSeconds: 0.1,
      maxSubSteps: 6,
      stepSeconds: 1 / 60,
    });

    expect(loop.advance(1 / 120, true).steps).toBe(0);
    expect(loop.advance(1 / 120, true).steps).toBe(1);
    expect(loop.advance(2 / 60, true).steps).toBe(2);
  });

  it("drops oversized catch-up work instead of tying simulation cost to a slow frame", () => {
    const loop = createFixedTimestepLoop({
      maxFrameSeconds: 0.4,
      maxSubSteps: 2,
      stepSeconds: 1 / 60,
    });
    const result = loop.advance(0.4, true);

    expect(result.steps).toBe(2);
    expect(result.droppedSeconds).toBeGreaterThan(0);
  });

  it("resets accumulated time while inactive", () => {
    const loop = createFixedTimestepLoop({
      maxFrameSeconds: 0.1,
      maxSubSteps: 6,
      stepSeconds: 1 / 60,
    });

    expect(loop.advance(1 / 120, true).steps).toBe(0);
    expect(loop.advance(1 / 120, false).steps).toBe(0);
    expect(loop.advance(1 / 120, true).steps).toBe(0);
  });
});
