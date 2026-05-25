import type { PointerEvent } from "react";

export interface DotMatrixClick {
  readonly timeStamp: number;
  readonly x: number;
  readonly y: number;
}

const clicks: DotMatrixClick[] = [];

export function recordDotMatrixClick(event: PointerEvent<HTMLElement>) {
  if (event.button !== 0) return;

  const bounds = event.currentTarget.getBoundingClientRect();
  const width = Math.max(bounds.width, 1);
  const height = Math.max(bounds.height, 1);

  clicks.push({
    timeStamp: event.timeStamp,
    x: clamp01((event.clientX - bounds.left) / width),
    y: clamp01(1 - (event.clientY - bounds.top) / height),
  });

  trimClicks();
}

export function recordSyntheticDotMatrixClick(input: {
  readonly timeStamp: number;
  readonly x: number;
  readonly y: number;
}) {
  clicks.push({
    timeStamp: input.timeStamp,
    x: clamp01(input.x),
    y: clamp01(input.y),
  });

  trimClicks();
}

function trimClicks() {
  while (clicks.length > 12) {
    clicks.shift();
  }
}

export function drainDotMatrixClicks(): ReadonlyArray<DotMatrixClick> {
  return clicks.splice(0, clicks.length);
}

function clamp01(value: number): number {
  return Math.min(1, Math.max(0, value));
}
