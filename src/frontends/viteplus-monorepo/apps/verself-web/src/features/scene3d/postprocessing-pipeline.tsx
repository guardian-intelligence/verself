import { Bloom, EffectComposer, N8AO, Noise, SMAA, ToneMapping, Vignette } from "@react-three/postprocessing";

export function LandingPostprocessingPipeline() {
  return (
    <EffectComposer depthBuffer enableNormalPass multisampling={0}>
      <N8AO aoRadius={1.1} color="#2a2119" distanceFalloff={0.74} intensity={0.56} quality="medium" />
      <Bloom intensity={0.28} luminanceThreshold={0.78} luminanceSmoothing={0.38} mipmapBlur />
      <ToneMapping />
      <SMAA />
      <Noise opacity={0.018} premultiply />
      <Vignette offset={0.18} darkness={0.52} />
    </EffectComposer>
  );
}
