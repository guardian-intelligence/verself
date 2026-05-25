import type { DomainRect, Vec2 } from "./types";

interface ClosedContourOptions {
  readonly contourJitter?: number;
  readonly exponent?: number;
  readonly points: number;
  readonly seed?: number;
}

export function createClosedContour(
  rect: DomainRect,
  options: ClosedContourOptions,
): readonly Vec2[] {
  const centerX = rect.x + rect.width * 0.5;
  const centerY = rect.y + rect.height * 0.5;
  const radiusX = rect.width * 0.5;
  const radiusY = rect.height * 0.5;
  const exponent = options.exponent ?? 0.72;
  const jitter = options.contourJitter ?? 0;
  const seed = options.seed ?? 0;
  const points: Vec2[] = [];

  for (let index = 0; index < options.points; index += 1) {
    const angle = (index / options.points) * Math.PI * 2;
    const xUnit = signedPower(Math.cos(angle), exponent);
    const yUnit = signedPower(Math.sin(angle), exponent);
    const contour =
      1 +
      Math.sin(angle * 3 + seed * 1.73) * jitter +
      Math.sin(angle * 5 - seed * 0.81) * jitter * 0.6;

    points.push({
      x: clamp(centerX + xUnit * radiusX * contour, 0, 1),
      y: clamp(centerY + yUnit * radiusY * contour, 0, 1),
    });
  }

  return points;
}

function signedPower(value: number, exponent: number): number {
  return Math.sign(value) * Math.abs(value) ** exponent;
}

function clamp(value: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, value));
}
