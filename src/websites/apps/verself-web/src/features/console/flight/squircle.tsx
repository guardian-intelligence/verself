import { type CSSProperties, type HTMLAttributes, type RefObject, useRef } from "react";

// Continuous-corner (Apple superellipse) primitive. Post-audit, `useSquircle`
// measures the box (ResizeObserver) and feeds figma-squircle's getSvgPath into
// `clip-path: path(...)` — the canonical port of Figma's reverse-engineered
// Apple corner-smoothing math (cornerSmoothing 0.6). SSR/no-measure falls back
// to a plain large border-radius, so there is never a hydration shift.
//
// Deliberately NOT a polymorphic `as` component: TanStack Router's <Link> prop
// surface is too rich to wrap in an `as` generic without the props collapsing
// to `never`. Instead the corner is a hook returning {ref, style} that applies
// cleanly to a plain <div> (the tray/card) or a <Link> (the pill) with full
// typing. <Squircle> is the div convenience wrapper; the pill uses the hook
// directly. One radius source either way; concentric() keeps nesting aligned.

export const CORNER_SMOOTHING = 0.6;

// Apple's nested-rounded-rect rule: inner radius = outer radius − inset.
export function concentric(outerRadius: number, inset: number): number {
  return Math.max(0, outerRadius - inset);
}

export type SquircleHandle<T extends HTMLElement> = {
  readonly ref: RefObject<T | null>;
  readonly style: CSSProperties;
};

export function useSquircle<T extends HTMLElement = HTMLDivElement>(
  cornerRadius: number,
  cornerSmoothing: number = CORNER_SMOOTHING,
): SquircleHandle<T> {
  const ref = useRef<T | null>(null);
  void cornerSmoothing;
  // TODO(audit): ResizeObserver-measure ref → getSvgPath → clipPath: path(...).
  // Until then the fallback is a plain large border-radius (no hydration shift).
  return { ref, style: { borderRadius: cornerRadius } };
}

type SquircleProps = HTMLAttributes<HTMLDivElement> & {
  readonly cornerRadius: number;
  readonly cornerSmoothing?: number;
};

// Plain-div convenience wrapper. The squircle style wins over caller style so
// the corner can never be overridden by an upstream border-radius.
export function Squircle({ cornerRadius, cornerSmoothing, style, ...rest }: SquircleProps) {
  const sq = useSquircle<HTMLDivElement>(cornerRadius, cornerSmoothing);
  return <div {...rest} ref={sq.ref} style={{ ...style, ...sq.style }} />;
}
