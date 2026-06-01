import {
  Bloom,
  EffectComposer,
  N8AO,
  SMAA,
  ToneMapping,
  Vignette,
} from "@react-three/postprocessing";
import { WordmarkLabScene } from "./WordmarkLabScene";
import { SceneCanvas } from "../../scene-canvas";
import { wordmarkLabCamera } from "./wordmark-lab-config";

export function WordmarkLabCanvas() {
  return (
    <SceneCanvas camera={wordmarkLabCamera} className="absolute inset-0">
      <WordmarkLabScene />
      <EffectComposer depthBuffer enableNormalPass multisampling={0}>
        <N8AO
          aoRadius={0.95}
          color="#1a1410"
          distanceFalloff={0.72}
          intensity={0.42}
          quality="medium"
        />
        <Bloom intensity={0.34} luminanceSmoothing={0.42} luminanceThreshold={0.72} mipmapBlur />
        <ToneMapping />
        <SMAA />
        <Vignette darkness={0.46} offset={0.22} />
      </EffectComposer>
    </SceneCanvas>
  );
}
