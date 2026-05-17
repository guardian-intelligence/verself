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

// The arc is an absolute layer that fills the whole path group; the endpoint
// discs are positioned on top at exactly the bezier endpoints, so the curve
// provably runs disc-centre → disc-centre with no gap. `discPx` is the disc
// diameter; endpoints inset by its radius.
export function FlightArc({
  progress,
  accent,
  phaseKind,
  discPx,
  strokeWidth,
  markerR,
  marker,
}: {
  readonly progress: number; // already monotone + spring-driven by the machine
  readonly accent: string;
  readonly phaseKind: PhaseKind;
  readonly discPx: number;
  readonly strokeWidth: number;
  readonly markerR: number;
  readonly marker?: ReactNode;
}) {
  const mark = marker ?? <VerselfTriangleMarker r={markerR} />;
  const hostRef = useRef<HTMLDivElement | null>(null);
  const size = useSize(hostRef);
  const w = size && size.w > 0 ? size.w : FALLBACK.w;
  const h = size && size.h > 0 ? size.h : FALLBACK.h;

  const t = Math.min(Math.max(progress, 0), 1);
  const bezier = arcGeometry({ width: w, height: h, inset: discPx / 2 });
  const { solid, dotted } = splitAt(bezier, t);
  const at = pointAt(bezier, t);
  const angle = tangentDeg(bezier, t);

  return (
    <div ref={hostRef} className="absolute inset-0 z-0" aria-hidden="true" data-phase={phaseKind}>
      {/* h-full w-full + viewBox in measured px: the SVG always fills the host
          (never overflows toward the widget edge, killing the SSR flash) and
          maps 1:1 once measured (no preserveAspectRatio skew). */}
      <svg
        viewBox={`0 0 ${w} ${h}`}
        className="absolute inset-0 h-full w-full overflow-visible"
        fill="none"
      >
        <ArcPath d={solid} color={accent} strokeWidth={strokeWidth} />
        <ArcPath d={dotted} color={accent} strokeWidth={strokeWidth} dashed />
        <g transform={`translate(${at.x} ${at.y}) rotate(${angle})`}>{mark}</g>
      </svg>
    </div>
  );
}

function ArcPath({
  d,
  color,
  strokeWidth,
  dashed = false,
}: {
  readonly d: string;
  readonly color: string;
  readonly strokeWidth: number;
  readonly dashed?: boolean;
}) {
  if (!d) return null;
  return (
    <path
      d={d}
      stroke={color}
      strokeWidth={strokeWidth}
      strokeLinecap="round"
      // Dotted tail: near-zero dash + a gap scaled to the stroke, round-capped,
      // so it reads as evenly spaced dots whatever the card size (matches the
      // reference). The gap rhythm is stable because splitAt subdivides the
      // curve itself rather than offsetting a dash pattern.
      {...(dashed
        ? {
            strokeDasharray: `${(strokeWidth * 0.05).toFixed(3)} ${(strokeWidth * 1.9).toFixed(3)}`,
          }
        : {})}
    />
  );
}

// The Verself brand triangle (`▽`), an equilateral mark centred on its
// centroid with the apex on +x so <FlightArc>'s tangent rotation makes it lead
// the path. Solid white per the reference. This is the only marker; `r` is the
// circumradius so it scales with the card (composed from the disc size).
function VerselfTriangleMarker({ r }: { readonly r: number }) {
  // Equilateral, circumradius r: apex (r,0), base (-r/2, ±r·√3/2).
  const bx = (-r / 2).toFixed(3);
  const by = ((r * Math.sqrt(3)) / 2).toFixed(3);
  return <polygon points={`${r.toFixed(3)},0 ${bx},${by} ${bx},-${by}`} fill="oklch(1 0 0)" />;
}
