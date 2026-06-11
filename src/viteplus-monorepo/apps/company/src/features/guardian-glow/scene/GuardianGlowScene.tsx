import { Environment } from "@react-three/drei";
import { useMemo } from "react";
import type { GuardianGlowRuntime, GuardianGlowSettings } from "../guardian-glow-config";
import { GuardianComposer } from "../pipeline/GuardianComposer";
import { EclipseSourceRig } from "./EclipseSourceRig";
import { GuardianLogo } from "./GuardianLogo";
import { LightingRig } from "./LightingRig";
import { ProbeDebugViews } from "./ProbeDebugViews";
import { ReflectionCardRig } from "./ReflectionCardRig";
import { useReflectionProbe } from "./ReflectionProbe";
import { resolveGuardianLightingRig } from "./lighting-rig-model";
import { createGuardianLogoFrame } from "./logo-frame";

type GuardianGlowSceneProps = {
  readonly runtime: GuardianGlowRuntime;
  readonly settings: GuardianGlowSettings;
};

export function GuardianGlowScene({ runtime, settings }: GuardianGlowSceneProps) {
  const logoFrame = useMemo(() => createGuardianLogoFrame(), []);
  const lightingRig = useMemo(
    () => resolveGuardianLightingRig(settings, logoFrame),
    [logoFrame, settings],
  );
  const probe = useReflectionProbe(runtime, lightingRig);

  return (
    <>
      <color attach="background" args={["#000000"]} />
      <Environment preset="studio" environmentIntensity={runtime.dynamicProbe ? 0.06 : 0.28} />
      {probe.node}
      <EclipseSourceRig lightingRig={lightingRig} runtime={runtime} settings={settings} />
      <LightingRig lightingRig={lightingRig} runtime={runtime} />
      <ReflectionCardRig lightingRig={lightingRig} runtime={runtime} />
      <GuardianLogo envMap={probe.texture} runtime={runtime} settings={settings} />
      <ProbeDebugViews
        envMap={probe.texture}
        lightingRig={lightingRig}
        runtime={runtime}
        settings={settings}
      />
      <GuardianComposer lightingRig={lightingRig} runtime={runtime} settings={settings} />
    </>
  );
}
