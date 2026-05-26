import { ClientOnly } from "@tanstack/react-router";
import { Suspense, type ReactNode } from "react";
import { SceneFallback } from "./scene-fallback";

interface SceneHostProps {
  readonly children: ReactNode;
  readonly className?: string;
  readonly fallback?: ReactNode;
}

export function SceneHost({ children, className, fallback }: SceneHostProps) {
  const fallbackNode = fallback ?? <SceneFallback />;

  return (
    <div aria-hidden="true" className={className}>
      <ClientOnly fallback={fallbackNode}>
        <Suspense fallback={fallbackNode}>{children}</Suspense>
      </ClientOnly>
    </div>
  );
}
