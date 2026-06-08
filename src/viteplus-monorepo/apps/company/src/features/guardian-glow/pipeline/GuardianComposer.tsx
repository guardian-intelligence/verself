import { EffectComposer, Noise, SMAA, ToneMapping, Vignette } from "@react-three/postprocessing";
import { ToneMappingMode } from "postprocessing";
import { HalfFloatType } from "three";
import type { GuardianGlowRuntime, GuardianGlowSettings } from "../guardian-glow-config";
import type { GuardianLightingRig } from "../scene/lighting-rig-model";
import { ChromaticBloomComposite } from "./ChromaticBloomComposite";

type GuardianComposerProps = {
  readonly lightingRig: GuardianLightingRig;
  readonly runtime: GuardianGlowRuntime;
  readonly settings: GuardianGlowSettings;
};

export function GuardianComposer({ lightingRig, runtime, settings }: GuardianComposerProps) {
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
      <ChromaticBloomComposite
        eclipseSourceWorldPosition={[
          lightingRig.origin.x + lightingRig.eclipseSource.position[0],
          lightingRig.origin.y + lightingRig.eclipseSource.position[1],
          lightingRig.origin.z + lightingRig.eclipseSource.position[2],
        ]}
        settings={settings}
      />
      <ToneMapping exposure={settings.exposure} mode={ToneMappingMode.AGX} />
      <SMAA />
      <Noise opacity={0.012} premultiply />
      <Vignette darkness={0.48} offset={0.24} />
    </EffectComposer>
  );
}
