// Pure flight-path geometry. One cubic bezier from origin endpoint to
// destination endpoint; progress t in [0,1] splits it into a traversed (solid)
// head and a remaining (dotted) tail, and positions/orients the marker. No
// React, no time — composed by <FlightArc>.

export type Point = { readonly x: number; readonly y: number };

// p0 = origin, p1/p2 = controls, p3 = destination.
export type Bezier = readonly [Point, Point, Point, Point];

export type ArcBox = {
  readonly width: number;
  readonly height: number;
  // x-inset of both endpoints (= endpoint-disc radius) so the curve begins and
  // ends exactly at the disc centres and reads as one continuous line.
  readonly inset?: number;
};

// The reference arc: a confident lob that rises off the origin disc, crests
// high and broad through the middle, and settles into the destination disc
// (matches the Flighty reference). Control points are a fraction of the box so
// the curve is resolution-independent. Both endpoints sit exactly on the
// vertical centre (the disc-centre line), inset by the disc radius, so the
// curve provably runs disc-centre → disc-centre as one continuous line.
export function arcGeometry({ width, height, inset = 0 }: ArcBox): Bezier {
  const x0 = inset;
  const x1 = width - inset;
  const span = x1 - x0;
  const cy = height * 0.5; // disc-centre line
  // Pull both controls hard toward the top so the apex clears the discs by a
  // healthy margin and the span between the discs reads as the dominant lob,
  // not a flat hop. Slight asymmetry (left control a touch higher) gives the
  // climb-out → cruise feel of the reference rather than a symmetric hump.
  return [
    { x: x0, y: cy },
    { x: x0 + span * 0.3, y: height * 0.04 },
    { x: x0 + span * 0.68, y: height * -0.02 },
    { x: x1, y: cy },
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

function lerpPt(a: Point, b: Point, t: number): Point {
  return { x: a.x + (b.x - a.x) * t, y: a.y + (b.y - a.y) * t };
}

function fmt(p: Point): string {
  return `${p.x.toFixed(3)} ${p.y.toFixed(3)}`;
}

// De Casteljau subdivision at t -> two sub-bezier `d` strings. Splitting the
// curve itself (not a dash-offset hack) keeps the dotted tail's dash rhythm
// stable as the marker advances. Below/above the [eps,1-eps] band one side is
// empty so a round line-cap never leaves a stray dot at an endpoint.
const SPLIT_EPS = 1e-3;
export function splitAt(b: Bezier, t: number): SplitPaths {
  const u = Math.min(Math.max(t, 0), 1);
  const [p0, p1, p2, p3] = b;
  const a = lerpPt(p0, p1, u);
  const bc = lerpPt(p1, p2, u);
  const c = lerpPt(p2, p3, u);
  const d = lerpPt(a, bc, u);
  const e = lerpPt(bc, c, u);
  const f = lerpPt(d, e, u); // point on the curve at u
  return {
    solid: u <= SPLIT_EPS ? "" : `M ${fmt(p0)} C ${fmt(a)} ${fmt(d)} ${fmt(f)}`,
    dotted: u >= 1 - SPLIT_EPS ? "" : `M ${fmt(f)} C ${fmt(e)} ${fmt(c)} ${fmt(p3)}`,
  };
}
