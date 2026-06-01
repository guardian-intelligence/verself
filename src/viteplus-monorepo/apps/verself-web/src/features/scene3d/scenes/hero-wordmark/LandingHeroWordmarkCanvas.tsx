import { Bloom, EffectComposer, SMAA, ToneMapping, Vignette } from "@react-three/postprocessing";
import { HeroWordmarkScene } from "./HeroWordmarkScene";
import { SceneCanvas } from "../../scene-canvas";
import { wordmarkLabCamera } from "../wordmark-lab/wordmark-lab-config";

export function LandingHeroWordmarkCanvas() {
  return (
    <SceneCanvas camera={wordmarkLabCamera} className="absolute inset-0">
      <HeroWordmarkScene />
      <EffectComposer depthBuffer enableNormalPass multisampling={0}>
        <Bloom intensity={0.34} luminanceSmoothing={0.42} luminanceThreshold={0.72} mipmapBlur />
        <ToneMapping />
        <SMAA />
        <Vignette darkness={0.28} offset={0.22} />
      </EffectComposer>
    </SceneCanvas>
  );
}
