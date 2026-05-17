import { type ReactNode, useId } from "react";
import { type Bezier, pointAt, splitAt, tangentDeg } from "./geometry";
import type { PhaseKind } from "./phase";

// The arc draws one bezier, the solid/dotted split at the marker, the marker
// itself, and a decorative progress shimmer. It no longer measures: the route
// band builds the bezier ONCE (so the same curve drives the arc AND the two
// endpoint arrow angles — single geometry source) and hands it down with the
// already-measured pixel box. `progress` arrives already monotone +
// spring-driven from the machine, so the marker only ever advances. The marker
// is ALWAYS the Verself triangle (`▽`), apex-forward along the path tangent.
//
// The SVG maps 1:1 to the integer pixel box (no preserveAspectRatio skew, no
// fractional viewBox — brief note k), so the stroke and triangle stay crisp.

export function FlightArc({
  bezier,
  width,
  height,
  progress,
  accent,
  phaseKind,
  strokeWidth,
  markerR,
  marker,
}: {
  readonly bezier: Bezier;
  readonly width: number;
  readonly height: number;
  readonly progress: number; // already monotone + spring-driven by the machine
  readonly accent: string;
  readonly phaseKind: PhaseKind;
  readonly strokeWidth: number;
  readonly markerR: number;
  readonly marker?: ReactNode;
}) {
  const mark = marker ?? <VerselfTriangleMarker r={markerR} />;
  const shimmerId = useId();

  const t = Math.min(Math.max(progress, 0), 1);
  const { solid, dotted } = splitAt(bezier, t);
  const at = pointAt(bezier, t);
  const angle = tangentDeg(bezier, t);

  // The shimmer is decorative and state-independent (DESIGN.md): it only
  // paints while building, never at boarding (t≡0) or late, and it never
  // reads or moves `t`. Removing this block changes nothing about phase.
  const showShimmer = phaseKind === "enroute" && solid !== "";

  return (
    <div className="absolute inset-0 z-0" aria-hidden="true" data-phase={phaseKind}>
      <svg
        viewBox={`0 0 ${width} ${height}`}
        className="absolute inset-0 h-full w-full overflow-visible"
        fill="none"
      >
        {showShimmer ? (
          <defs>
            {/* A narrow bright window swept left→right along the box, then
                held off-screen, so the solid head periodically flashes. Pure
                SMIL: no rAF, no React state, no coupling to the spring. */}
            <linearGradient
              id={shimmerId}
              gradientUnits="userSpaceOnUse"
              x1="0"
              y1="0"
              x2={width * 0.2}
              y2="0"
            >
              <stop offset="0" stopColor="oklch(1 0 0)" stopOpacity="0" />
              <stop offset="0.5" stopColor="oklch(1 0 0)" stopOpacity="0.6" />
              <stop offset="1" stopColor="oklch(1 0 0)" stopOpacity="0" />
              <animateTransform
                attributeName="gradientTransform"
                type="translate"
                values={`${-width * 0.3} 0; ${width * 1.05} 0; ${width * 1.05} 0`}
                keyTimes="0; 0.62; 1"
                dur="1.9s"
                calcMode="spline"
                keySplines="0.4 0 0.2 1; 0 0 1 1"
                repeatCount="indefinite"
              />
            </linearGradient>
          </defs>
        ) : null}
        <ArcPath d={solid} color={accent} strokeWidth={strokeWidth} />
        <ArcPath d={dotted} color={accent} strokeWidth={strokeWidth} dashed />
        {showShimmer ? (
          // Same path as the solid head, stroked with the moving gradient so
          // the sheen rides exactly the traversed segment.
          <path
            d={solid}
            stroke={`url(#${shimmerId})`}
            strokeWidth={strokeWidth}
            strokeLinecap="round"
          />
        ) : null}
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
      // Dotted tail: zero-length dash + a stroke-scaled gap, round-capped, so
      // it reads as evenly spaced dots at any card size (matches the
      // reference). The rhythm is stable because splitAt subdivides the curve
      // itself rather than offsetting a dash pattern.
      {...(dashed
        ? {
            strokeDasharray: `${(strokeWidth * 0.01).toFixed(3)} ${(strokeWidth * 1.8).toFixed(3)}`,
          }
        : {})}
    />
  );
}

// The Verself brand triangle (`▽`), an equilateral mark centred on its
// centroid with the apex on +x so <FlightArc>'s tangent rotation makes it lead
// the path. Solid white per the reference. `r` is the circumradius so it
// scales with the card (composed from the disc size).
function VerselfTriangleMarker({ r }: { readonly r: number }) {
  // Equilateral, circumradius r: apex (r,0), base (-r/2, ±r·√3/2).
  const bx = (-r / 2).toFixed(3);
  const by = ((r * Math.sqrt(3)) / 2).toFixed(3);
  return <polygon points={`${r.toFixed(3)},0 ${bx},${by} ${bx},-${by}`} fill="oklch(1 0 0)" />;
}
