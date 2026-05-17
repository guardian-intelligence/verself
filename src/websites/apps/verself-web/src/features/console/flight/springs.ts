import { useEffect, useRef, useState } from "react";
import type { AccentToken } from "./phase";

// Hand-rolled critically-damped spring (no overshoot — a flight marker must
// never bounce backward) on rAF. Vendored rather than @react-spring/web because
// this repo has no agent-side npm-lockfile path; the physics is ~40 lines and
// the monotone guarantee already lives in the machine (it only ever feeds a
// non-decreasing target). SSR-safe: with no window the value is the target and
// the spring is at rest.

type SpringConfig = { readonly stiffness: number };
// ~0.55s settle, critically damped (damping = 2√stiffness, mass = 1).
const DEFAULT: SpringConfig = { stiffness: 120 };
const MAX_DT = 1 / 30; // clamp after tab refocus so the step stays stable
const EPS = 1e-3;

function step(pos: number, vel: number, target: number, dt: number, k: number) {
  const c = 2 * Math.sqrt(k); // critical damping, m = 1
  const a = -k * (pos - target) - c * vel;
  const nextVel = vel + a * dt;
  const nextPos = pos + nextVel * dt;
  return { pos: nextPos, vel: nextVel };
}

// Animated scalar. Returns the live value and whether the spring has settled
// (the machine samples `atRest` as its discrete-phase morph boundary).
export function useSpring(
  target: number,
  config: SpringConfig = DEFAULT,
): { value: number; atRest: boolean } {
  const posRef = useRef(target);
  const velRef = useRef(0);
  const rafRef = useRef<number | null>(null);
  const lastRef = useRef<number | null>(null);
  const [, force] = useState(0);

  const atRest = Math.abs(posRef.current - target) < EPS && Math.abs(velRef.current) < EPS;

  useEffect(() => {
    if (typeof window === "undefined") return;
    if (atRest) {
      posRef.current = target;
      velRef.current = 0;
      return;
    }
    const tick = (t: number) => {
      const last = lastRef.current ?? t;
      const dt = Math.min((t - last) / 1000, MAX_DT);
      lastRef.current = t;
      const next = step(posRef.current, velRef.current, target, dt, config.stiffness);
      posRef.current = next.pos;
      velRef.current = next.vel;
      if (Math.abs(posRef.current - target) < EPS && Math.abs(velRef.current) < EPS) {
        posRef.current = target;
        velRef.current = 0;
        lastRef.current = null;
        force((n) => n + 1);
        return;
      }
      force((n) => n + 1);
      rafRef.current = window.requestAnimationFrame(tick);
    };
    rafRef.current = window.requestAnimationFrame(tick);
    return () => {
      if (rafRef.current !== null) window.cancelAnimationFrame(rafRef.current);
      lastRef.current = null;
    };
  }, [target, atRest, config.stiffness]);

  if (typeof window === "undefined") return { value: target, atRest: true };
  return { value: posRef.current, atRest };
}

type Lch = readonly [number, number, number];
const ACCENT_LCH: Record<AccentToken, Lch> = {
  // oklch(L C H). The on-time accent is iOS `systemGreen` (dark) — #30D158,
  // the value the reference renders on a dark lock screen — computed once from
  // sRGB → OKLab so the widget stays oklch-only (brief note f: match the
  // reference literally). This REVERTS the prior brand-Flare divergence; the
  // green is now the platform's, not the Newsroom treatment's. Amber (running
  // late) keeps the reference marigold.
  "accent-green": [0.7556, 0.2082, 146.98],
  "accent-amber": [0.8, 0.16, 70],
};

export function resolveAccent(token: AccentToken): string {
  const [l, c, h] = ACCENT_LCH[token];
  return `oklch(${l} ${c} ${h})`;
}

// Crossfades the accent in oklch space on phase change (green ⇄ amber). Each
// channel rides its own spring so the transition is perceptually smooth.
export function useAccentSpring(token: AccentToken): string {
  const [tl, tc, th] = ACCENT_LCH[token];
  const l = useSpring(tl);
  const c = useSpring(tc);
  const h = useSpring(th);
  return `oklch(${l.value.toFixed(4)} ${c.value.toFixed(4)} ${h.value.toFixed(2)})`;
}
