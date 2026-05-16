// Pure flight-path geometry. One cubic bezier from origin endpoint to
// destination endpoint; progress t in [0,1] splits it into a traversed (solid)
// head and a remaining (dotted) tail, and positions/orients the marker. No
// React, no time — composed by <FlightArc>.

export type Point = { readonly x: number; readonly y: number };

// p0 = origin, p1/p2 = controls, p3 = destination.
export type Bezier = readonly [Point, Point, Point, Point];

export type ArcBox = { readonly width: number; readonly height: number };

// The reference arc: a shallow, confident lob that rises off the origin and
// settles toward the destination (matches Image #1/#2). Control points are a
// fraction of the box so the curve is resolution-independent.
export function arcGeometry({ width, height }: ArcBox): Bezier {
  // TODO(audit): final control-point ratios are tuned against the reference
  // screenshots after the composition is approved.
  const y0 = height * 0.62;
  const y3 = height * 0.46;
  return [
    { x: 0, y: y0 },
    { x: width * 0.34, y: -height * 0.14 },
    { x: width * 0.66, y: height * 0.06 },
    { x: width, y: y3 },
  ];
}

export function pointAt(b: Bezier, t: number): Point {
  const u = 1 - t;
  const a = u * u * u;
  const c = 3 * u * u * t;
  const d = 3 * u * t * t;
  const e = t * t * t;
  return {
    x: a * b[0].x + c * b[1].x + d * b[2].x + e * b[3].x,
    y: a * b[0].y + c * b[1].y + d * b[2].y + e * b[3].y,
  };
}

// Tangent (first derivative) -> marker rotation. Degrees, atan2 of dB/dt.
export function tangentDeg(b: Bezier, t: number): number {
  const u = 1 - t;
  const dx =
    3 * u * u * (b[1].x - b[0].x) + 6 * u * t * (b[2].x - b[1].x) + 3 * t * t * (b[3].x - b[2].x);
  const dy =
    3 * u * u * (b[1].y - b[0].y) + 6 * u * t * (b[2].y - b[1].y) + 3 * t * t * (b[3].y - b[2].y);
  return (Math.atan2(dy, dx) * 180) / Math.PI;
}

export type SplitPaths = {
  // Origin -> marker (drawn solid).
  readonly solid: string;
  // Marker -> destination (drawn dotted).
  readonly dotted: string;
};

// De Casteljau subdivision at t -> two sub-bezier `d` strings. Splitting the
// curve itself (rather than dash-offset hacks) keeps the dotted tail's dash
// rhythm stable as the marker advances.
export function splitAt(_b: Bezier, _t: number): SplitPaths {
  // TODO(audit): implement De Casteljau subdivision once the tree is approved.
  return { solid: "", dotted: "" };
}
