import { lazy, Suspense } from "react";
import { ClientOnly } from "@tanstack/react-router";

const LandingSpotlightCanvas = lazy(() =>
  import("./scenes/landing-spotlight/LandingSpotlightCanvas").then((module) => ({
    default: module.LandingSpotlightCanvas,
  })),
);

type LandingSpotlightProps = {
  readonly className?: string;
};

export function LandingSpotlight({ className }: LandingSpotlightProps) {
  return (
    <div aria-hidden="true" className={className ?? "pointer-events-none absolute inset-0"}>
      <ClientOnly fallback={null}>
        <Suspense fallback={null}>
          <LandingSpotlightCanvas />
        </Suspense>
      </ClientOnly>
    </div>
  );
}
