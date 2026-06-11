import { useFrame } from "@react-three/fiber";
import { useRef } from "react";
import type { Group } from "three";
import { RectAreaLightUniformsLib } from "three/examples/jsm/lights/RectAreaLightUniformsLib.js";
import type { GuardianGlowRuntime } from "../guardian-glow-config";
import { guardianRigMotionQuaternion, type GuardianLightingRig } from "./lighting-rig-model";

RectAreaLightUniformsLib.init();

type LightingRigProps = {
  readonly lightingRig: GuardianLightingRig;
  readonly runtime: GuardianGlowRuntime;
};

export function LightingRig({ lightingRig, runtime }: LightingRigProps) {
  const groupRef = useRef<Group>(null);
  const enabled = runtime.directLights;

  useFrame(({ clock }) => {
    const group = groupRef.current;
    if (!group) return;

    group.position.copy(lightingRig.origin);
    group.quaternion.copy(guardianRigMotionQuaternion(lightingRig, clock.elapsedTime));
  });

  return (
    <>
      <ambientLight color="#ffffff" intensity={enabled ? 0.012 : 0} />
      <group ref={groupRef} position={lightingRig.origin}>
        {lightingRig.rectLights.map((light) => (
          <rectAreaLight
            key={light.id}
            color={light.color}
            height={light.height}
            intensity={enabled ? light.intensity : 0}
            position={light.position}
            rotation={light.rotation}
            width={light.width}
          />
        ))}
        {lightingRig.pointLights.map((light) => (
          <pointLight
            key={light.id}
            color={light.color}
            decay={2}
            distance={light.distance}
            intensity={enabled ? light.intensity : 0}
            position={light.position}
          />
        ))}
      </group>
    </>
  );
}
