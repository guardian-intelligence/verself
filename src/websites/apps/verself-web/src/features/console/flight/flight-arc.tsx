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

// Shimmer wavelength as a fraction of the box width. A solid head is roughly
// half-to-two-thirds of the box, so ≈0.42 puts about one-and-a-half bright
// bands on it at once (brief note g: "a gradient and a half"). It is also the
// exact translate distance per loop, which is what makes the motion seamless.
const SHIMMER_PERIOD = 0.42;

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
            {/* Continuous "work in motion" sheen. ONE period of a soft bright
                band, tiled across the whole path with spreadMethod="repeat"
                so ≈1.5 bands ride the solid head at once ("a gradient and a
                half"). It translates by EXACTLY one period at constant
                velocity and loops — because the pattern is period-periodic,
                +1 period maps it onto itself, so the motion is perfectly
                seamless: it flows continuously rather than flashing once and
                pausing. Pure SMIL, no rAF/React state, zero coupling to the
                spring or `t` (DESIGN.md). */}
            <linearGradient
              id={shimmerId}
              gradientUnits="userSpaceOnUse"
              spreadMethod="repeat"
              x1="0"
              y1="0"
              x2={SHIMMER_PERIOD * width}
              y2="0"
            >
              <stop offset="0" stopColor="oklch(1 0 0)" stopOpacity="0" />
              <stop offset="0.5" stopColor="oklch(1 0 0)" stopOpacity="0.5" />
              <stop offset="1" stopColor="oklch(1 0 0)" stopOpacity="0" />
              <animateTransform
                attributeName="gradientTransform"
                type="translate"
                from="0 0"
                to={`${SHIMMER_PERIOD * width} 0`}
                dur="1.6s"
                calcMode="linear"
                repeatCount="indefinite"
              />
            </linearGradient>
          </defs>
        ) : null}
        <ArcPath d={solid} color={accent} strokeWidth={strokeWidth} />
        <ArcPath d={dotted} color={accent} strokeWidth={strokeWidth} dashed />
        {showShimmer ? (
          // Same path as the solid head, stroked with the flowing gradient so
          // the sheen rides exactly the traversed segment, origin → marker.
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
