import { CameraRig } from "../../camera-rig";
import { LightingRig } from "../../lighting-rig";
import { SceneInputs } from "../../scene-inputs";
import { FirstLightWorld } from "./FirstLightWorld";

export function FirstLightScene() {
  return (
    <>
      <SceneInputs />
      <CameraRig />
      <color attach="background" args={["#070605"]} />
      <fog attach="fog" args={["#080706", 5.8, 11.8]} />
      <ambientLight color="#283447" intensity={0.095} />
      <LightingRig />
      <FirstLightWorld />
    </>
  );
}
