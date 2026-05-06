import { LandingPostprocessingPipeline } from "./postprocessing-pipeline";
import { FirstLightScene } from "./scenes/first-light/FirstLightScene";
import { SceneCanvas } from "./scene-canvas";
import { SceneFallback } from "./scene-fallback";

const landingCamera = {
  position: [0, 0, 5.8],
  fov: 35,
  near: 0.1,
  far: 40,
} as const;

export function LandingSceneCanvas() {
  return (
    <SceneCanvas camera={landingCamera} fallback={<SceneFallback />}>
      <FirstLightScene />
      <LandingPostprocessingPipeline />
    </SceneCanvas>
  );
}
