import { useMemo } from "react";
import type { Texture } from "three";
import type { GuardianGlowRuntime, GuardianGlowSettings } from "../guardian-glow-config";
import { createGuardianLogoGeometries } from "./GuardianLogoGeometry";
import { GuardianLogoMaterial } from "./GuardianLogoMaterial";
import { GUARDIAN_LOGO_POSITION, GUARDIAN_LOGO_ROTATION } from "./logo-frame";

type GuardianLogoProps = {
  readonly envMap: Texture;
  readonly runtime: GuardianGlowRuntime;
  readonly settings: GuardianGlowSettings;
};

export function GuardianLogo({ envMap, runtime, settings }: GuardianLogoProps) {
  const geometries = useMemo(() => createGuardianLogoGeometries(), []);

  return (
    <group
      position={GUARDIAN_LOGO_POSITION}
      rotation={GUARDIAN_LOGO_ROTATION}
      visible={runtime.logoVisible}
    >
      {geometries.map((geometry) => (
        <mesh key={geometry.uuid} geometry={geometry}>
          <GuardianLogoMaterial envMap={envMap} runtime={runtime} settings={settings} />
        </mesh>
      ))}
    </group>
  );
}
