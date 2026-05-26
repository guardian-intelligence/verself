import { LandingPostprocessingPipeline } from "./postprocessing-pipeline";
import { FirstLightScene } from "./scenes/first-light/FirstLightScene";
import { SceneCanvas } from "./scene-canvas";
import { SceneFallback } from "./scene-fallback";
import { SceneRuntime } from "./scene-runtime";

const landingCamera = {
  position: [1.35, 0.54, 6.15],
  fov: 33,
  near: 0.1,
  far: 40,
} as const;

export function LandingSceneCanvas() {
  return (
    <SceneCanvas camera={landingCamera} fallback={<SceneFallback />}>
      <SceneRuntime>
        <FirstLightScene />
        <LandingPostprocessingPipeline />
      </SceneRuntime>
    </SceneCanvas>
  );
}
