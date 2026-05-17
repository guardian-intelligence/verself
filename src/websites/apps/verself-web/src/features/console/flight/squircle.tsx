import {
  type CSSProperties,
  type HTMLAttributes,
  type RefObject,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { getSvgPath } from "./squircle-path";

// Continuous-corner (Apple superellipse) primitive. `useSquircle` measures the
// element with a ResizeObserver and applies the figma-squircle curve as
// `clip-path: path(...)` (layout-neutral, iOS-Safari supported). Before the
// first client measurement (SSR + first paint) it returns a plain
// border-radius — identical on the server and the first client render, so
// there is no hydration mismatch; the clip swaps in after mount.
//
// The radius is `number` (uniform) OR `{ top, bottom }` (asymmetric). The card
// uses the asymmetric form: an iOS Live Activity reads as a fixed 3-D corner
// "wheel" projected onto the lock-screen, so the top corners (under the
// Dynamic Island) and the free bottom corners ride different points of that
// wheel. figma-squircle's per-corner distribution keeps all four corners on
// Apple's continuous-corner curve while they differ.
//
// Deliberately NOT a polymorphic `as` component: TanStack <Link>'s prop
// surface collapses an `as` generic to `never`. The corner is a hook returning
// {ref, style} that applies cleanly to a <div> (card/chip) or a <Link> (pill)
// with full typing. <Squircle> is the div convenience wrapper.

export const CORNER_SMOOTHING = 0.6; // Apple's app-icon value.

// number → uniform; { top, bottom } → top corners vs. bottom corners differ.
export type CornerRadius = number | { readonly top: number; readonly bottom: number };

// Apple's nested-rounded-rect rule: inner radius = outer radius − inset.
export function concentric(outerRadius: number, inset: number): number {
  return Math.max(0, outerRadius - inset);
}

function topRadius(r: CornerRadius): number {
  return typeof r === "number" ? r : r.top;
}

function bottomRadius(r: CornerRadius): number {
  return typeof r === "number" ? r : r.bottom;
}

export type SquircleHandle<T extends HTMLElement> = {
  readonly ref: RefObject<T | null>;
  readonly style: CSSProperties;
};

const useIsomorphicLayoutEffect = typeof window === "undefined" ? () => {} : useLayoutEffect;

export function useSquircle<T extends HTMLElement = HTMLDivElement>(
  cornerRadius: CornerRadius,
  cornerSmoothing: number = CORNER_SMOOTHING,
): SquircleHandle<T> {
  const ref = useRef<T | null>(null);
  const [box, setBox] = useState<{ w: number; h: number } | null>(null);

  useIsomorphicLayoutEffect(() => {
    const el = ref.current;
    if (!el || typeof ResizeObserver === "undefined") return;
    const measure = () => {
      const rect = el.getBoundingClientRect();
      setBox((prev) =>
        prev && prev.w === rect.width && prev.h === rect.height
          ? prev
          : { w: rect.width, h: rect.height },
      );
    };
    measure();
    const ro = new ResizeObserver(measure);
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  const top = topRadius(cornerRadius);
  const bottom = bottomRadius(cornerRadius);

  const style = useMemo<CSSProperties>(() => {
    if (!box || box.w <= 0 || box.h <= 0) {
      // SSR / pre-measure fallback. The 4-value border-radius shorthand is
      // top-left, top-right, bottom-right, bottom-left — deterministic from
      // props (not box size), so the server and first client render match.
      return { borderRadius: `${top}px ${top}px ${bottom}px ${bottom}px` };
    }
    const d = getSvgPath({
      width: box.w,
      height: box.h,
      topLeftCornerRadius: top,
      topRightCornerRadius: top,
      bottomLeftCornerRadius: bottom,
      bottomRightCornerRadius: bottom,
      cornerSmoothing,
    });
    return { clipPath: `path('${d}')` };
  }, [box, top, bottom, cornerSmoothing]);

  return { ref, style };
}

type SquircleProps = HTMLAttributes<HTMLDivElement> & {
  readonly cornerRadius: CornerRadius;
  readonly cornerSmoothing?: number;
};

// Plain-div convenience wrapper. The squircle style wins over caller style so
// the corner can never be overridden by an upstream border-radius.
export function Squircle({ cornerRadius, cornerSmoothing, style, ...rest }: SquircleProps) {
  const sq = useSquircle<HTMLDivElement>(cornerRadius, cornerSmoothing);
  return <div {...rest} ref={sq.ref} style={{ ...style, ...sq.style }} />;
}
