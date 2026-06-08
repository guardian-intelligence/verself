import type { Texture } from "three";
import type { GuardianGlowRuntime, GuardianGlowSettings } from "../guardian-glow-config";

type GuardianLogoMaterialProps = {
  readonly envMap: Texture;
  readonly runtime: GuardianGlowRuntime;
  readonly settings: GuardianGlowSettings;
};

export function GuardianLogoMaterial({ envMap, runtime, settings }: GuardianLogoMaterialProps) {
  const probeOnly = settings.stage === "probe";
  const roughness = probeOnly ? Math.max(0.035, settings.roughness * 0.45) : settings.roughness;
  const envMapIntensity = runtime.dynamicProbe ? settings.env * 2.1 : settings.env * 0.35;

  return (
    <meshPhysicalMaterial
      clearcoat={settings.clearcoat}
      clearcoatRoughness={0.1 + roughness * 0.42}
      color={probeOnly ? "#04050a" : "#11141a"}
      envMap={runtime.dynamicProbe ? envMap : null}
      envMapIntensity={envMapIntensity}
      ior={1.56}
      metalness={probeOnly ? Math.max(0.94, settings.metalness) : settings.metalness}
      reflectivity={0.88}
      roughness={roughness}
      specularIntensity={1.5}
    />
  );
}
