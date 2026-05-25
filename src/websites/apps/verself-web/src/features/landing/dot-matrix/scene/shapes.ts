import type { DomainRect, Vec2, WaveFieldShape } from "./types";

export function sampleShape(shape: WaveFieldShape, point: Vec2, feather: number): number {
  switch (shape.kind) {
    case "ellipse":
      return sampleEllipse(shape.center, shape.radius, point, feather);
    case "polygon":
      return samplePolygon(shape.points, point, feather);
    case "rect":
      return sampleRect(shape.rect, point, feather);
    case "roundedRect":
      return sampleRoundedRect(shape.rect, shape.radius, point, feather);
  }
}

export function shapeBounds(shape: WaveFieldShape): DomainRect {
  switch (shape.kind) {
    case "ellipse":
      return {
        height: shape.radius.y * 2,
        width: shape.radius.x * 2,
        x: shape.center.x - shape.radius.x,
        y: shape.center.y - shape.radius.y,
      };
    case "polygon":
      return boundsForPoints(shape.points);
    case "rect":
      return shape.rect;
    case "roundedRect":
      return shape.rect;
  }
}

export function rectCenter(rect: DomainRect): Vec2 {
  return {
    x: rect.x + rect.width * 0.5,
    y: rect.y + rect.height * 0.5,
  };
}

export function expandRect(rect: DomainRect, amount: Vec2): DomainRect {
  return {
    height: rect.height + amount.y * 2,
    width: rect.width + amount.x * 2,
    x: rect.x - amount.x,
    y: rect.y - amount.y,
  };
}

export function clampRect(rect: DomainRect, bounds: DomainRect): DomainRect {
  const x = clamp(rect.x, bounds.x, bounds.x + bounds.width);
  const y = clamp(rect.y, bounds.y, bounds.y + bounds.height);
  const right = clamp(rect.x + rect.width, bounds.x, bounds.x + bounds.width);
  const top = clamp(rect.y + rect.height, bounds.y, bounds.y + bounds.height);
  return {
    height: Math.max(0, top - y),
    width: Math.max(0, right - x),
    x,
    y,
  };
}

function sampleRect(rect: DomainRect, point: Vec2, feather: number): number {
  if (
    point.x >= rect.x &&
    point.x <= rect.x + rect.width &&
    point.y >= rect.y &&
    point.y <= rect.y + rect.height
  ) {
    return 1;
  }

  const dx = Math.max(rect.x - point.x, 0, point.x - (rect.x + rect.width));
  const dy = Math.max(rect.y - point.y, 0, point.y - (rect.y + rect.height));
  return featheredDistance(Math.sqrt(dx * dx + dy * dy), feather);
}

function sampleRoundedRect(rect: DomainRect, radius: number, point: Vec2, feather: number): number {
  const rx = Math.max(rect.width * 0.5, 0.0001);
  const ry = Math.max(rect.height * 0.5, 0.0001);
  const r = Math.min(Math.max(radius, 0), rx, ry);
  const cx = rect.x + rx;
  const cy = rect.y + ry;
  const qx = Math.abs(point.x - cx) - (rx - r);
  const qy = Math.abs(point.y - cy) - (ry - r);
  const outsideX = Math.max(qx, 0);
  const outsideY = Math.max(qy, 0);
  const outsideDistance = Math.sqrt(outsideX * outsideX + outsideY * outsideY);
  const insideDistance = Math.min(Math.max(qx, qy), 0);
  const signedDistance = outsideDistance + insideDistance - r;
  if (signedDistance <= 0) return 1;
  return featheredDistance(signedDistance, feather);
}

function sampleEllipse(center: Vec2, radius: Vec2, point: Vec2, feather: number): number {
  const rx = Math.max(radius.x, 0.0001);
  const ry = Math.max(radius.y, 0.0001);
  const nx = (point.x - center.x) / rx;
  const ny = (point.y - center.y) / ry;
  const normalizedDistance = Math.sqrt(nx * nx + ny * ny);
  if (normalizedDistance <= 1) return 1;

  const worldDistance = (normalizedDistance - 1) * Math.min(rx, ry);
  return featheredDistance(worldDistance, feather);
}

function samplePolygon(points: readonly Vec2[], point: Vec2, feather: number): number {
  if (points.length < 3) return 0;
  if (isPointInPolygon(points, point)) return 1;

  let minDistance = Number.POSITIVE_INFINITY;
  for (let index = 0; index < points.length; index += 1) {
    const a = points[index];
    const b = points[(index + 1) % points.length];
    if (a === undefined || b === undefined) continue;
    minDistance = Math.min(minDistance, distanceToSegment(point, a, b));
  }
  return featheredDistance(minDistance, feather);
}

function featheredDistance(distance: number, feather: number): number {
  if (feather <= 0) return distance <= 0 ? 1 : 0;
  return smoothstep(feather, 0, distance);
}

function boundsForPoints(points: readonly Vec2[]): DomainRect {
  let minX = Number.POSITIVE_INFINITY;
  let minY = Number.POSITIVE_INFINITY;
  let maxX = Number.NEGATIVE_INFINITY;
  let maxY = Number.NEGATIVE_INFINITY;

  for (const point of points) {
    minX = Math.min(minX, point.x);
    minY = Math.min(minY, point.y);
    maxX = Math.max(maxX, point.x);
    maxY = Math.max(maxY, point.y);
  }

  if (
    !Number.isFinite(minX) ||
    !Number.isFinite(minY) ||
    !Number.isFinite(maxX) ||
    !Number.isFinite(maxY)
  ) {
    return { height: 0, width: 0, x: 0, y: 0 };
  }

  return {
    height: maxY - minY,
    width: maxX - minX,
    x: minX,
    y: minY,
  };
}

function isPointInPolygon(points: readonly Vec2[], point: Vec2): boolean {
  let inside = false;

  for (
    let current = 0, previous = points.length - 1;
    current < points.length;
    previous = current, current += 1
  ) {
    const a = points[current];
    const b = points[previous];
    if (a === undefined || b === undefined) continue;
    const intersects =
      a.y > point.y !== b.y > point.y &&
      point.x < ((b.x - a.x) * (point.y - a.y)) / Math.max(b.y - a.y, 0.000001) + a.x;
    if (intersects) inside = !inside;
  }

  return inside;
}

function distanceToSegment(point: Vec2, a: Vec2, b: Vec2): number {
  const abx = b.x - a.x;
  const aby = b.y - a.y;
  const ab2 = Math.max(abx * abx + aby * aby, 0.000001);
  const t = clamp(((point.x - a.x) * abx + (point.y - a.y) * aby) / ab2, 0, 1);
  const x = a.x + abx * t;
  const y = a.y + aby * t;
  const dx = point.x - x;
  const dy = point.y - y;
  return Math.sqrt(dx * dx + dy * dy);
}

function smoothstep(edge0: number, edge1: number, value: number): number {
  const t = clamp((value - edge0) / (edge1 - edge0), 0, 1);
  return t * t * (3 - 2 * t);
}

function clamp(value: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, value));
}
