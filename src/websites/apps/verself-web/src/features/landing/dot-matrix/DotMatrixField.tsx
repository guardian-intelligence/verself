"use client";

import { lazy, Suspense, useRef } from "react";
import { ClientOnly } from "@tanstack/react-router";

import { cn } from "@verself/ui/lib/utils";

const DotMatrixCanvas = lazy(() =>
  import("./DotMatrixCanvas").then((module) => ({ default: module.DotMatrixCanvas })),
);

interface DotMatrixFieldProps {
  readonly className?: string;
}

export function DotMatrixField({ className }: DotMatrixFieldProps) {
  const hostRef = useRef<HTMLDivElement>(null);

  return (
    <div
      ref={hostRef}
      aria-hidden="true"
      className={cn("pointer-events-none absolute inset-0 overflow-hidden", className)}
    >
      <ClientOnly fallback={null}>
        <Suspense fallback={null}>
          <DotMatrixCanvas hostRef={hostRef} />
        </Suspense>
      </ClientOnly>
    </div>
  );
}
