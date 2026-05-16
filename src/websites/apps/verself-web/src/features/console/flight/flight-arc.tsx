import { type ReactNode, useLayoutEffect, useRef, useState } from "react";
import { arcGeometry, pointAt, splitAt, tangentDeg } from "./geometry";
import type { PhaseKind } from "./phase";

// The arc is its own component (per brief): it owns the bezier, the
// solid/dotted split at the marker, and the marker's position + rotation.
// `progress` arrives already monotone + spring-driven from the machine, so the
// marker only ever advances. The marker is ALWAYS the Verself triangle mark
// (brand `▽`), oriented apex-forward along the path tangent — never a plane,
// never swapped. The slot exists only so the brand mark is injected rather
// than hardcoded in the geometry layer.
//
// The SVG is sized to the measured pixel box (no preserveAspectRatio="none"):
// a non-uniform stretch would skew the stroke and the triangle. Same
// ResizeObserver pattern as the squircle — pixel-faithful by construction.

const FALLBACK = { w: 200, h: 56 } as const; // SSR / pre-measure (~host aspect)

const useIsomorphicLayoutEffect = typeof window === "undefined" ? () => {} : useLayoutEffect;

function useSize(ref: React.RefObject<HTMLElement | null>) {
  const [size, setSize] = useState<{ w: number; h: number } | null>(null);
  useIsomorphicLayoutEffect(() => {
    const el = ref.current;
    if (!el || typeof ResizeObserver === "undefined") return;
    const measure = () => {
      const r = el.getBoundingClientRect();
      setSize((p) => (p && p.w === r.width && p.h === r.height ? p : { w: r.width, h: r.height }));
    };
    measure();
    const ro = new ResizeObserver(measure);
    ro.observe(el);
    return () => ro.disconnect();
  }, [ref]);
  return size;
}

export function FlightArc({
  progress,
  accent,
  phaseKind,
  marker = <VerselfTriangleMarker />,
}: {
  readonly progress: number; // already monotone + spring-driven by the machine
  readonly accent: string;
  readonly phaseKind: PhaseKind;
  readonly marker?: ReactNode;
}) {
  const hostRef = useRef<HTMLDivElement | null>(null);
  const size = useSize(hostRef);
  const w = size && size.w > 0 ? size.w : FALLBACK.w;
  const h = size && size.h > 0 ? size.h : FALLBACK.h;

  const t = Math.min(Math.max(progress, 0), 1);
  const bezier = arcGeometry({ width: w, height: h });
  const { solid, dotted } = splitAt(bezier, t);
  const at = pointAt(bezier, t);
  const angle = tangentDeg(bezier, t);

  return (
    <div
      ref={hostRef}
      className="relative z-0 -mx-3 flex h-14 min-w-[3rem] flex-1 items-center"
      aria-hidden="true"
      data-phase={phaseKind}
    >
      {/* h-full w-full + viewBox in measured px: the SVG always fills the host
          (never overflows toward the widget edge, killing the SSR flash) and
          maps 1:1 once measured (no preserveAspectRatio skew). */}
      <svg
        viewBox={`0 0 ${w} ${h}`}
        className="absolute inset-0 h-full w-full overflow-visible"
        fill="none"
      >
        <ArcPath d={solid} color={accent} />
        <ArcPath d={dotted} color={accent} dashed />
        <g transform={`translate(${at.x} ${at.y}) rotate(${angle})`}>{marker}</g>
      </svg>
    </div>
  );
}

function ArcPath({
  d,
  color,
  dashed = false,
}: {
  readonly d: string;
  readonly color: string;
  readonly dashed?: boolean;
}) {
  if (!d) return null;
  return (
    <path
      d={d}
      stroke={color}
      strokeWidth={5}
      strokeLinecap="round"
      {...(dashed ? { strokeDasharray: "1.5 9" } : {})}
    />
  );
}

// The Verself brand triangle (`▽`), an equilateral mark centered on its
// centroid with the apex on +x so <FlightArc>'s tangent rotation makes it lead
// the path. Solid white per the reference. This is the only marker.
function VerselfTriangleMarker() {
  // Equilateral, circumradius 7: apex (7,0), base (-3.5, ±6.06).
  return <polygon points="7,0 -3.5,6.06 -3.5,-6.06" fill="oklch(1 0 0)" />;
}
