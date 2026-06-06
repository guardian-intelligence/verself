import { useFrame } from "@react-three/fiber";
import { useRef } from "react";
import type { Group } from "three";
import { RectAreaLightUniformsLib } from "three/examples/jsm/lights/RectAreaLightUniformsLib.js";
import type { GuardianGlowRuntime, GuardianGlowSettings } from "../guardian-glow-config";

RectAreaLightUniformsLib.init();

type LightingRigProps = {
  readonly runtime: GuardianGlowRuntime;
  readonly settings: GuardianGlowSettings;
};

export function LightingRig({ runtime, settings }: LightingRigProps) {
  const stripRef = useRef<Group>(null);
  const enabled = runtime.directLights;
  const spread = 0.32 + settings.bandWidth * 3.3;
  const direct = enabled ? settings.lightIntensity : 0;
  const whiteIntensity = direct * (5.5 + settings.bloom * 1.4);

  useFrame(({ clock }) => {
    const strip = stripRef.current;
    if (!strip) return;

    const sweep = -1.75 + settings.band * 3.55;
    const drift = Math.sin(clock.elapsedTime * 0.42) * 0.08;
    strip.position.set(sweep + drift, -0.16 + settings.band * 0.18, 2.35);
  });

  return (
    <>
      <ambientLight color="#ffffff" intensity={enabled ? 0.01 : 0} />
      <directionalLight
        color="#d7e8ff"
        intensity={enabled ? 0.36 * direct : 0}
        position={[2.6, 3.1, 4.8]}
      />
      <pointLight
        color="#f08b33"
        decay={2}
        distance={7}
        intensity={enabled ? direct * (1.1 + settings.warm * 1.4) : 0}
        position={[-3.1, -2.7, 2.6]}
      />
      <pointLight
        color="#ffffff"
        decay={2}
        distance={6}
        intensity={enabled ? 0.3 * direct : 0}
        position={[2.8, 1.2, 3.8]}
      />
      <group ref={stripRef} rotation={[0.24, -0.22, -0.72]}>
        <rectAreaLight
          color="#ffffff"
          height={3.7}
          intensity={direct * (9.5 + settings.bloom * 3)}
          position={[0, 0, 0]}
          width={spread}
        />
        <pointLight
          color="#ffffff"
          decay={2}
          distance={6}
          intensity={whiteIntensity}
          position={[0, 0, 0.42]}
        />
        <pointLight
          color="#d9ecff"
          decay={2}
          distance={6}
          intensity={whiteIntensity * 0.62}
          position={[spread * 0.52, 1.25, 0.18]}
        />
        <pointLight
          color="#fff4df"
          decay={2}
          distance={5.4}
          intensity={whiteIntensity * (0.38 + settings.warm * 0.12)}
          position={[-spread * 0.48, -1.1, 0.16]}
        />
      </group>
    </>
  );
}
