import { lazy } from "react";
import { SceneHost } from "./scene-host";
import { SceneFallback } from "./scene-fallback";

export interface LandingSceneProps {
  readonly motion?: boolean;
}

const LandingSceneCanvas = lazy(() =>
  import("./LandingSceneCanvas").then((module) => ({ default: module.LandingSceneCanvas })),
);

export function LandingScene({ motion = true }: LandingSceneProps) {
  if (!motion) {
    return (
      <div aria-hidden="true" className="pointer-events-none fixed inset-0 z-0 overflow-hidden">
        <SceneFallback />
      </div>
    );
  }

  return (
    <SceneHost
      className="pointer-events-none fixed inset-0 z-0 overflow-hidden"
      fallback={<SceneFallback />}
    >
      <LandingSceneCanvas />
    </SceneHost>
  );
}
