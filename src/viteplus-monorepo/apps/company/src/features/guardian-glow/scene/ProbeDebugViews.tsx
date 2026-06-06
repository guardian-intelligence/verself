import type { Texture } from "three";
import type { GuardianGlowRuntime, GuardianGlowSettings } from "../guardian-glow-config";

type ProbeDebugViewsProps = {
  readonly envMap: Texture;
  readonly runtime: GuardianGlowRuntime;
  readonly settings: GuardianGlowSettings;
};

export function ProbeDebugViews({ envMap, runtime, settings }: ProbeDebugViewsProps) {
  if (settings.developmentMode !== "1" || settings.stage !== "probe") {
    return null;
  }

  return (
    <group position={[-2.42, -1.72, 0.1]} rotation={[0.08, 0.18, 0]} visible={runtime.logoVisible}>
      <mesh position={[0, 0, 0]}>
        <sphereGeometry args={[0.22, 48, 24]} />
        <meshPhysicalMaterial
          clearcoat={1}
          clearcoatRoughness={0.04}
          color="#020203"
          envMap={envMap}
          envMapIntensity={settings.env * 1.35}
          metalness={1}
          roughness={0.035}
        />
      </mesh>
      <mesh position={[0.55, 0.02, 0]}>
        <sphereGeometry args={[0.2, 48, 24]} />
        <meshPhysicalMaterial
          clearcoat={0.55}
          color="#111114"
          envMap={envMap}
          envMapIntensity={settings.env}
          metalness={0.55}
          roughness={0.28}
        />
      </mesh>
    </group>
  );
}
