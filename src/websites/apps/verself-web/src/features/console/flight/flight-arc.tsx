import { type ReactNode } from "react";
import { arcGeometry, pointAt, splitAt, tangentDeg } from "./geometry";
import type { PhaseKind } from "./phase";

// The arc is its own component (per brief): it owns the bezier, the
// solid/dotted split at the marker, and the marker's position + rotation.
// `progress` is the phase-derived target; useMonotoneSpring guarantees the
// marker only ever advances. The marker is ALWAYS the Verself triangle mark
// (brand `▽`), oriented apex-forward along the path tangent — never a plane,
// never swapped. The slot remains only so the brand mark is injected rather
// than hardcoded in the geometry layer.

const VIEW = { width: 200, height: 70 } as const;

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
  const t = progress;
  const bezier = arcGeometry(VIEW);
  const { solid, dotted } = splitAt(bezier, t);
  const at = pointAt(bezier, t);
  const angle = tangentDeg(bezier, t);

  return (
    <div
      className="relative flex min-w-0 flex-1 items-center"
      aria-hidden="true"
      data-phase={phaseKind}
    >
      <svg
        viewBox={`0 0 ${VIEW.width} ${VIEW.height}`}
        preserveAspectRatio="none"
        className="h-12 w-full"
        fill="none"
      >
        <ArcPath kind="solid" d={solid} color={accent} />
        <ArcPath kind="dotted" d={dotted} color={accent} />
        <g transform={`translate(${at.x} ${at.y}) rotate(${angle})`}>{marker}</g>
      </svg>
    </div>
  );
}

function ArcPath({
  kind,
  d,
  color,
}: {
  readonly kind: "solid" | "dotted";
  readonly d: string;
  readonly color: string;
}) {
  return (
    <path
      d={d}
      stroke={color}
      strokeWidth={5}
      strokeLinecap="round"
      {...(kind === "dotted" ? { strokeDasharray: "1.5 9" } : {})}
    />
  );
}

// The Verself brand triangle (`▽`), drawn as an equilateral mark centered on
// its centroid with the apex on +x so <FlightArc>'s tangent rotation makes it
// lead the path. Solid white per the reference. This is the only marker.
function VerselfTriangleMarker() {
  // Equilateral, circumradius 7: apex (7,0), base (-3.5, ±6.06).
  return <polygon points="7,0 -3.5,6.06 -3.5,-6.06" fill="oklch(1 0 0)" />;
}
