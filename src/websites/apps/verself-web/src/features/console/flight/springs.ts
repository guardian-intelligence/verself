import type { AccentToken } from "./phase";

// Interpolation seam. Post-audit this is backed by @react-spring/web ^10.0.3
// (same react-spring monorepo line already cataloged for @react-spring/three).
// Kept behind these hooks so the component tree never imports the engine
// directly and the skeleton compiles dependency-free for this audit pass.
//
// Invariant: monotonicity is enforced on the *target* (here), not in the
// spring. The spring only ever eases toward a non-decreasing value, so the
// marker is structurally incapable of moving backward.

// OLED-tuned brand tokens, resolved to oklch at the call site. Re-tuned to
// Image #2 after the composition is approved.
const ACCENT_OKLCH: Record<AccentToken, string> = {
  "accent-green": "oklch(0.82 0.20 152)",
  "accent-amber": "oklch(0.80 0.16 70)",
};

export function resolveAccent(token: AccentToken): string {
  return ACCENT_OKLCH[token];
}

// Animated scalar (arc progress, marker glide). Monotonicity is the machine's
// job (it gates the target); this is a pure spring. Today: instant pass-
// through. Post-audit: drives a react-spring value toward `target`.
export function useNumberSpring(target: number): number {
  return target; // TODO(audit): spring toward target
}

// Crossfaded accent color on phase change. Today: instant. Post-audit: spring.
export function useAccentSpring(token: AccentToken): string {
  return resolveAccent(token); // TODO(audit): crossfade between tokens
}
