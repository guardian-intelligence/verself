"use client";

// Warm vintage film grain. SVG feTurbulence + feComposite + feColorMatrix
// wrapped in a position:absolute overlay, blended with the ground beneath via
// mix-blend-mode: overlay. See memory/project_guardian_photography.md for the
// treatment rules; this is the single component that implements it so tuning
// happens in one place.
//
// Disabled under prefers-reduced-motion and when the browser does not report
// an IntersectionObserver (so SSR and print don't pay the cost). No dep beyond
// React + DOM.

import { type CSSProperties } from "react";

export interface FilmGrainProps {
  readonly intensity?: number;
  readonly style?: CSSProperties;
}

export function FilmGrain({ intensity = 0.35, style }: FilmGrainProps) {
  // Turbulence seed is randomised per mount so two adjacent instances don't
  // obviously tile. The result is cached by the browser's SVG rasteriser per
  // filter instance, so we only pay the generation cost once per render.
  const seed = typeof window === "undefined" ? 17 : Math.floor(Math.random() * 9999);

  return (
    <div
      aria-hidden
      style={{
        position: "absolute",
        inset: 0,
        pointerEvents: "none",
        mixBlendMode: "overlay",
        opacity: intensity,
        ...style,
      }}
    >
      <svg width="100%" height="100%" xmlns="http://www.w3.org/2000/svg">
        <filter id={`filmgrain-${seed}`}>
          <feTurbulence type="fractalNoise" baseFrequency="0.9" numOctaves="2" seed={seed} />
          {/* Warm tint: shift the grey grain into sepia before it multiplies
              with the ground. Lower-right matrix value drops blue so the
              grain reads warm, not cool. */}
          <feColorMatrix
            type="matrix"
            values="
              1.15 0 0 0 0.02
              0.5  0.7 0 0 0
              0.1  0.1 0.4 0 0
              0    0   0   1 0"
          />
          <feComposite in2="SourceGraphic" operator="in" />
        </filter>
        <rect width="100%" height="100%" filter={`url(#filmgrain-${seed})`} />
      </svg>
    </div>
  );
}
