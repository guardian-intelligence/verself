// Elevation framework.
//
// A surface's shadow is never ad-hoc numbers — it is ONE choice: how many
// points is this surface lifted off the page? One overhead light source then
// produces three stacked layers from that single lift:
//
//  • contact — "is it touching the surface?"  tiny vertical offset, tiny blur,
//              low alpha; grounds it so it doesn't look pasted on.
//  • key     — "how high is it?"  the readable drop; offset ≈ the lift, blur
//              ≈ 2.4× the offset, negative spread so it tucks under the edge
//              rather than haloing out.
//  • ambient — "is it floating?"  wide, soft, heavily negative spread; the
//              iOS "elevated card" feel.
//
// Invariants, encoded in the ratios so they cannot be violated per-call
// (brief's depth notes): offsets are purely vertical (the light is overhead,
// not raking); each layer's geometry is a fixed multiple of the lift; you pick
// a LEVEL, never per-layer numbers. On a light page the dark shadow reads
// directly; on a true-black surface you'd swap to a light hairline instead
// (shadows vanish on black) — that is a different surface treatment, not a
// different elevation, so it is intentionally out of this function's scope.
//
// Calibration: at `LIVE_ACTIVITY_LIFT` (5pt — an iOS Live Activity sits low,
// a few points, and lets the ambient layer do the floating) the three layers
// evaluate to exactly the brief's reference recipe:
//   contact 0 1px 2px      oklch(0 0 0 / .07)
//   key     0 5px 12px -4px oklch(0 0 0 / .16)
//   ambient 0 22px 46px -16px oklch(0 0 0 / .30)

type ShadowLayer = {
  // All geometry is a ratio of the lift, so a layer is shape + alpha only.
  readonly yRatio: number;
  readonly blurRatio: number;
  readonly spreadRatio: number;
  readonly alpha: number;
};

const CONTACT: ShadowLayer = { yRatio: 0.2, blurRatio: 0.4, spreadRatio: 0, alpha: 0.07 };
const KEY: ShadowLayer = { yRatio: 1, blurRatio: 2.4, spreadRatio: -0.8, alpha: 0.16 };
const AMBIENT: ShadowLayer = { yRatio: 4.4, blurRatio: 9.2, spreadRatio: -3.2, alpha: 0.3 };
const LAYERS: readonly ShadowLayer[] = [CONTACT, KEY, AMBIENT];

function paint(layer: ShadowLayer, lift: number): string {
  const y = Math.round(layer.yRatio * lift);
  const blur = Math.round(layer.blurRatio * lift);
  const spread = Math.round(layer.spreadRatio * lift);
  return `0 ${y}px ${blur}px ${spread}px oklch(0 0 0 / ${layer.alpha})`;
}

// The single entry point: a lift in points → a composed three-layer
// `box-shadow`. Pure; any surface picks a lift and gets a consistent shadow.
export function elevation(liftPt: number): string {
  return LAYERS.map((layer) => paint(layer, liftPt)).join(", ");
}

// iOS Live Activities float low — a few points — and let the wide ambient
// layer carry the "elevated card" feel. The flight card uses this level.
export const LIVE_ACTIVITY_LIFT = 5;
