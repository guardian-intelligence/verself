import { Bloom, EffectComposer, N8AO, Noise, SMAA, ToneMapping, Vignette } from "@react-three/postprocessing";

export function LandingPostprocessingPipeline() {
  return (
    <EffectComposer depthBuffer enableNormalPass multisampling={0}>
      <N8AO aoRadius={1.35} color="#140d08" distanceFalloff={0.62} intensity={1.18} quality="medium" />
      <Bloom intensity={0.42} luminanceThreshold={0.72} luminanceSmoothing={0.34} mipmapBlur />
      <ToneMapping />
      <SMAA />
      <Noise opacity={0.028} premultiply />
      <Vignette offset={0.14} darkness={0.68} />
    </EffectComposer>
  );
}
