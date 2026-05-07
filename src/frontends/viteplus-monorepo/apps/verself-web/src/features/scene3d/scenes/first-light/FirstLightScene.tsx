import { CameraRig } from "../../camera-rig";
import { LightingRig } from "../../lighting-rig";
import { SceneInputs } from "../../scene-inputs";
import { FirstLightWorld } from "./FirstLightWorld";

export function FirstLightScene() {
  return (
    <>
      <SceneInputs />
      <CameraRig />
      <color attach="background" args={["#050403"]} />
      <fog attach="fog" args={["#060504", 5.6, 11.5]} />
      <ambientLight color="#2d3c50" intensity={0.055} />
      <LightingRig />
      <FirstLightWorld />
    </>
  );
}
