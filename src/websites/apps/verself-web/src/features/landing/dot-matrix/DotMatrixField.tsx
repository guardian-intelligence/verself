"use client";

import { lazy, Suspense } from "react";

import { cn } from "@verself/ui/lib/utils";

const DotMatrixCanvas = lazy(() =>
  import("./DotMatrixCanvas").then((module) => ({ default: module.DotMatrixCanvas })),
);

interface DotMatrixFieldProps {
  readonly className?: string;
}

export function DotMatrixField({ className }: DotMatrixFieldProps) {
  return (
    <div
      aria-hidden="true"
      className={cn("pointer-events-none absolute inset-0 overflow-hidden", className)}
    >
      <Suspense fallback={null}>
        <DotMatrixCanvas />
      </Suspense>
    </div>
  );
}
