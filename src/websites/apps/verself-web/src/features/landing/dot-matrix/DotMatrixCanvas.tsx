import { useEffect, useRef } from "react";

import { createDotMatrixRenderer } from "./renderer";

export function DotMatrixCanvas() {
  const hostRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const host = hostRef.current;
    if (host === null) return;

    return createDotMatrixRenderer(host).dispose;
  }, []);

  return (
    <div
      ref={hostRef}
      className="absolute inset-0 opacity-0 transition-opacity duration-700 ease-out"
    />
  );
}
