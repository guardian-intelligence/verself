import { Environment } from "@react-three/drei";
import type { GuardianGlowRuntime, GuardianGlowSettings } from "../guardian-glow-config";
import { GuardianLogo } from "./GuardianLogo";
import { LightingRig } from "./LightingRig";
import { ProbeDebugViews } from "./ProbeDebugViews";
import { ReflectionCardRig } from "./ReflectionCardRig";
import { useReflectionProbe } from "./ReflectionProbe";

type GuardianGlowSceneProps = {
  readonly runtime: GuardianGlowRuntime;
  readonly settings: GuardianGlowSettings;
};

export function GuardianGlowScene({ runtime, settings }: GuardianGlowSceneProps) {
  const probe = useReflectionProbe(runtime);

  return (
    <>
      <color attach="background" args={["#000000"]} />
      <Environment preset="studio" environmentIntensity={runtime.dynamicProbe ? 0.06 : 0.28} />
      {probe.node}
      <LightingRig runtime={runtime} settings={settings} />
      <ReflectionCardRig runtime={runtime} settings={settings} />
      <GuardianLogo envMap={probe.texture} runtime={runtime} settings={settings} />
      <ProbeDebugViews envMap={probe.texture} runtime={runtime} settings={settings} />
    </>
  );
}
