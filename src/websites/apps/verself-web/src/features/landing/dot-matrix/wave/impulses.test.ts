import { describe, expect, it } from "vite-plus/test";

import { createClickWaveImpulse, createWaveImpulseQueue } from "./impulses.ts";

describe("wave impulse queue", () => {
  it("stores bounded one-shot impulses", () => {
    const queue = createWaveImpulseQueue(2);

    queue.push({ radius: 0.02, strength: 0.04, x: 0.1, y: 0.2 });
    queue.push({ radius: 0.03, strength: 0.05, x: 0.2, y: 0.3 });
    queue.push({ radius: 0.04, strength: 0.06, x: 0.3, y: 0.4 });

    expect(queue.size()).toBe(2);
    expect(queue.drain(6)).toEqual([
      { radius: 0.03, strength: 0.05, x: 0.2, y: 0.3 },
      { radius: 0.04, strength: 0.06, x: 0.3, y: 0.4 },
    ]);
    expect(queue.size()).toBe(0);
  });

  it("maps a click into a localized visible impulse", () => {
    const impulse = createClickWaveImpulse(
      { x: 0.4, y: 0.6 },
      { height: 0.5, width: 0.6, x: 0.1, y: 0.2 },
    );

    expect(impulse.radius).toEqual(expect.any(Number));
    expect(impulse.strength).toEqual(expect.any(Number));
    expect(impulse.x).toBeCloseTo(0.34, 8);
    expect(impulse.y).toBeCloseTo(0.5, 8);
  });
});
