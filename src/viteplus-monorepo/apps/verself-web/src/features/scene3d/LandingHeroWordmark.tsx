import { lazy } from "react";
import { SceneHost } from "./scene-host";

const LandingHeroWordmarkCanvas = lazy(() =>
  import("./scenes/hero-wordmark/LandingHeroWordmarkCanvas").then((module) => ({
    default: module.LandingHeroWordmarkCanvas,
  })),
);

type LandingHeroWordmarkProps = {
  readonly className?: string;
};

export function LandingHeroWordmark({ className }: LandingHeroWordmarkProps) {
  return (
    <SceneHost
      className={className ?? "pointer-events-none absolute inset-0 overflow-visible"}
      fallback={null}
    >
      <LandingHeroWordmarkCanvas />
    </SceneHost>
  );
}
