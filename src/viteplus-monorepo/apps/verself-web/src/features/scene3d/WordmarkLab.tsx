import { lazy } from "react";
import { SceneHost } from "./scene-host";
import { SceneFallback } from "./scene-fallback";

const WordmarkLabCanvas = lazy(() =>
  import("./scenes/wordmark-lab/WordmarkLabCanvas").then((module) => ({
    default: module.WordmarkLabCanvas,
  })),
);

export function WordmarkLab() {
  return (
    <main className="relative h-[100svh] w-full overflow-hidden bg-black">
      <SceneHost className="absolute inset-0" fallback={<SceneFallback />}>
        <WordmarkLabCanvas />
      </SceneHost>
    </main>
  );
}
