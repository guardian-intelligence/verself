import type { PointerEvent } from "react";

interface DotMatrixPointerState {
  active: boolean;
  x: number;
  y: number;
}

interface DotMatrixScrollState {
  current: number;
  lastRead: number;
  lastY: number;
  velocity: number;
}

export interface DotMatrixSignals {
  readonly pointer: DotMatrixPointerState;
  readonly scroll: DotMatrixScrollState;
}

const pointer: DotMatrixPointerState = {
  active: false,
  x: 0.5,
  y: 0.5,
};

const scroll: DotMatrixScrollState = {
  current: 0,
  lastRead: 0,
  lastY: 0,
  velocity: 0,
};

export function captureDotMatrixPointer(event: PointerEvent<HTMLElement>) {
  const bounds = event.currentTarget.getBoundingClientRect();
  const width = Math.max(bounds.width, 1);
  const height = Math.max(bounds.height, 1);

  pointer.active = true;
  pointer.x = clamp01((event.clientX - bounds.left) / width);
  pointer.y = clamp01(1 - (event.clientY - bounds.top) / height);
}

export function releaseDotMatrixPointer() {
  pointer.active = false;
}

export function readDotMatrixSignals(now: number): DotMatrixSignals {
  if (typeof window !== "undefined") {
    const documentElement = document.documentElement;
    const travel = Math.max(documentElement.scrollHeight - window.innerHeight, 1);
    const y = clamp01(window.scrollY / travel);
    const elapsed = Math.max((now - scroll.lastRead) / 1000, 1 / 120);
    scroll.velocity = (y - scroll.lastY) / elapsed;
    scroll.current += (y - scroll.current) * 0.12;
    scroll.lastY = y;
    scroll.lastRead = now;
  }

  return { pointer, scroll };
}

function clamp01(value: number): number {
  return Math.min(1, Math.max(0, value));
}
