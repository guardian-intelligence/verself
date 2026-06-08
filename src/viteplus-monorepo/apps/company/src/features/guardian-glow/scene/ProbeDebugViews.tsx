import type { Texture } from "three";
import type { GuardianGlowRuntime, GuardianGlowSettings } from "../guardian-glow-config";
import type { GuardianLightingRig } from "./lighting-rig-model";

type ProbeDebugViewsProps = {
  readonly envMap: Texture;
  readonly lightingRig: GuardianLightingRig;
  readonly runtime: GuardianGlowRuntime;
  readonly settings: GuardianGlowSettings;
};

export function ProbeDebugViews({ envMap, lightingRig, runtime, settings }: ProbeDebugViewsProps) {
  if (settings.developmentMode !== "1" || settings.stage !== "probe") {
    return null;
  }

  const materialBallPosition = [
    lightingRig.origin.x - 2.45,
    lightingRig.origin.y - 1.56,
    lightingRig.origin.z + 0.18,
  ] as const;

  return (
    <>
      {lightingRig.anchorDebug.map((anchor) => (
        <mesh key={anchor.id} position={anchor.position} visible={runtime.logoVisible}>
          <sphereGeometry args={[0.035, 16, 8]} />
          <meshBasicMaterial color={anchor.color} toneMapped={false} />
        </mesh>
      ))}
      <mesh position={lightingRig.probePosition} visible={runtime.logoVisible}>
        <sphereGeometry args={[0.06, 20, 10]} />
        <meshBasicMaterial color="#ff5fca" toneMapped={false} />
      </mesh>
      <group
        position={materialBallPosition}
        rotation={[0.08, 0.18, 0]}
        visible={runtime.logoVisible}
      >
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
    </>
  );
}
