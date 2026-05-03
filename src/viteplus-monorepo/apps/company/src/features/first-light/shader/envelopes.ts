export const FIRST_LIGHT_TOTAL_MS = 2_800;
export const FIRST_LIGHT_HOLD_MS = 700;
export const FIRST_LIGHT_ARRIVAL_END_MS = 1_300;
export const FIRST_LIGHT_TRAIL_END_MS = 2_100;

export function trailLuminance(elapsedMs: number): number {
  if (elapsedMs < FIRST_LIGHT_HOLD_MS || elapsedMs > FIRST_LIGHT_TOTAL_MS) {
    return 0;
  }
  if (elapsedMs < FIRST_LIGHT_ARRIVAL_END_MS) {
    const t =
      (elapsedMs - FIRST_LIGHT_HOLD_MS) / (FIRST_LIGHT_ARRIVAL_END_MS - FIRST_LIGHT_HOLD_MS);
    return 0.1 * easeOutQuint(t);
  }
  if (elapsedMs < FIRST_LIGHT_TRAIL_END_MS) {
    const t =
      (elapsedMs - FIRST_LIGHT_ARRIVAL_END_MS) /
      (FIRST_LIGHT_TRAIL_END_MS - FIRST_LIGHT_ARRIVAL_END_MS);
    return 0.28 * Math.sin(Math.PI * t);
  }
  const t =
    (elapsedMs - FIRST_LIGHT_TRAIL_END_MS) / (FIRST_LIGHT_TOTAL_MS - FIRST_LIGHT_TRAIL_END_MS);
  return 0.12 * (1 - easeInOutCubic(t));
}

function easeOutQuint(t: number): number {
  return 1 - (1 - t) ** 5;
}

function easeInOutCubic(t: number): number {
  return t < 0.5 ? 4 * t * t * t : 1 - (-2 * t + 2) ** 3 / 2;
}
