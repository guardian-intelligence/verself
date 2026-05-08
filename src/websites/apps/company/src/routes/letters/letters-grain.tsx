"use client";

import { useEffect, useState } from "react";
import { FilmGrain } from "~/components/film-grain";

// Letters grain. Wraps the FilmGrain component, switches it to fixed
// positioning so the SVG fills the viewport, and fades the opacity from
// 0 → 0.22 on the next animation frame after mount. Loaded from the route
// layout via idle dynamic import: the grain chunk never blocks initial
// paint, and the texture dissolves in once the page has settled.
//
// Position is overridden via the `style` prop because FilmGrain defaults to
// position: absolute (sized to a positioned ancestor); the letters layout
// wants the grain to track the viewport, not the document.

export function LettersGrain() {
  const [shown, setShown] = useState(false);
  useEffect(() => {
    const id = requestAnimationFrame(() => setShown(true));
    return () => cancelAnimationFrame(id);
  }, []);
  return (
    <FilmGrain
      intensity={0.22}
      style={{
        position: "fixed",
        opacity: shown ? 0.22 : 0,
        transition: "opacity 1200ms ease-out",
      }}
    />
  );
}
