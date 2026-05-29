import type { WaveImpulse } from "./types";
import type { DomainRect } from "../scene/types";

export interface WaveImpulseQueue {
  readonly drain: (limit: number) => ReadonlyArray<WaveImpulse>;
  readonly push: (impulse: WaveImpulse) => void;
  readonly pushMany: (impulses: ReadonlyArray<WaveImpulse>) => void;
  readonly size: () => number;
}

export function createWaveImpulseQueue(capacity: number): WaveImpulseQueue {
  const impulses: WaveImpulse[] = [];

  const push = (impulse: WaveImpulse) => {
    impulses.push(clampImpulse(impulse));
    while (impulses.length > capacity) {
      impulses.shift();
    }
  };

  return {
    drain(limit: number) {
      return impulses.splice(0, Math.max(0, limit));
    },
    push,
    pushMany(nextImpulses: ReadonlyArray<WaveImpulse>) {
      for (const impulse of nextImpulses) {
        push(impulse);
      }
    },
    size() {
      return impulses.length;
    },
  };
}

export function createClickWaveImpulse(
  point: {
    readonly x: number;
    readonly y: number;
  },
  cameraRect: DomainRect,
): WaveImpulse {
  const DOT_MATRIX_CLICK_STRENGTH = 0.064;
  const DOT_MATRIX_CLICK_STRENGTH_SCALE = 0.1;
  return {
    radius: 0.034,
    strength: DOT_MATRIX_CLICK_STRENGTH * DOT_MATRIX_CLICK_STRENGTH_SCALE,
    x: cameraRect.x + point.x * cameraRect.width,
    y: cameraRect.y + point.y * cameraRect.height,
  };
}

function clampImpulse(impulse: WaveImpulse): WaveImpulse {
  if (impulse.kind === "front") {
    const normal = normalize({
      x: impulse.normalX ?? 1,
      y: impulse.normalY ?? 0,
    });
    return {
      frontLength: Math.min(2.2, Math.max(0.05, impulse.frontLength ?? impulse.radius)),
      kind: "front",
      normalX: normal.x,
      normalY: normal.y,
      radius: Math.min(0.08, Math.max(0.004, impulse.radius)),
      strength: Math.min(0.2, Math.max(-0.2, impulse.strength)),
      x: clampRange(impulse.x, -0.75, 1.75),
      y: clampRange(impulse.y, -0.75, 1.75),
    };
  }

  return {
    radius: Math.min(0.22, Math.max(0.008, impulse.radius)),
    strength: Math.min(0.2, Math.max(-0.2, impulse.strength)),
    x: clamp01(impulse.x),
    y: clamp01(impulse.y),
  };
}

function clamp01(value: number): number {
  return clampRange(value, 0, 1);
}

function clampRange(value: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, value));
}

function normalize(vector: { readonly x: number; readonly y: number }) {
  const length = Math.max(Math.sqrt(vector.x * vector.x + vector.y * vector.y), 0.0001);
  return {
    x: vector.x / length,
    y: vector.y / length,
  };
}
