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
  return {
    radius: 0.044,
    strength: 0.115,
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
      frontLength: Math.min(0.72, Math.max(0.05, impulse.frontLength ?? impulse.radius)),
      kind: "front",
      normalX: normal.x,
      normalY: normal.y,
      radius: Math.min(0.08, Math.max(0.006, impulse.radius)),
      strength: Math.min(0.2, Math.max(-0.2, impulse.strength)),
      x: clamp01(impulse.x),
      y: clamp01(impulse.y),
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
  return Math.min(1, Math.max(0, value));
}

function normalize(vector: { readonly x: number; readonly y: number }) {
  const length = Math.max(Math.sqrt(vector.x * vector.x + vector.y * vector.y), 0.0001);
  return {
    x: vector.x / length,
    y: vector.y / length,
  };
}
