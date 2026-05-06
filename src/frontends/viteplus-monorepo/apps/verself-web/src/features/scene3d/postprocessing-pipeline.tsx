import { Bloom, EffectComposer, Vignette } from "@react-three/postprocessing";

export function LandingPostprocessingPipeline() {
  return (
    <EffectComposer multisampling={4}>
      <Bloom intensity={0.32} luminanceThreshold={0.58} luminanceSmoothing={0.24} mipmapBlur />
      <Vignette offset={0.18} darkness={0.72} />
    </EffectComposer>
  );
}
