import { EffectComposer, Noise, SMAA, ToneMapping, Vignette } from "@react-three/postprocessing";
import { ToneMappingMode } from "postprocessing";
import { HalfFloatType } from "three";
import type { GuardianGlowRuntime, GuardianGlowSettings } from "../guardian-glow-config";
import { ChromaticBloomComposite } from "./ChromaticBloomComposite";

type GuardianComposerProps = {
  readonly runtime: GuardianGlowRuntime;
  readonly settings: GuardianGlowSettings;
};

export function GuardianComposer({ runtime, settings }: GuardianComposerProps) {
  if (!runtime.composer) {
    return null;
  }

  return (
    <EffectComposer
      frameBufferType={HalfFloatType}
      multisampling={runtime.multisampling}
      renderPriority={1}
      resolutionScale={runtime.composerResolutionScale}
    >
      <ChromaticBloomComposite settings={settings} />
      <ToneMapping exposure={settings.exposure} mode={ToneMappingMode.AGX} />
      <SMAA />
      <Noise opacity={0.012} premultiply />
      <Vignette darkness={0.48} offset={0.24} />
    </EffectComposer>
  );
}
