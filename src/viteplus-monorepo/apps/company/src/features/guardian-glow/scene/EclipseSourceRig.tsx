import { useLayoutEffect, useMemo, useRef } from "react";
import {
  AdditiveBlending,
  CanvasTexture,
  Color,
  DoubleSide,
  LinearFilter,
  SRGBColorSpace,
  type Group,
  type Texture,
} from "three";
import { RectAreaLightUniformsLib } from "three/examples/jsm/lights/RectAreaLightUniformsLib.js";
import type { GuardianGlowRuntime, GuardianGlowSettings } from "../guardian-glow-config";
import { enableReflectionProbeLayer } from "./layers";
import type { GuardianLightingRig } from "./lighting-rig-model";

RectAreaLightUniformsLib.init();

type EclipseSourceRigProps = {
  readonly lightingRig: GuardianLightingRig;
  readonly runtime: GuardianGlowRuntime;
  readonly settings: GuardianGlowSettings;
};

export function EclipseSourceRig({ lightingRig, runtime, settings }: EclipseSourceRigProps) {
  const groupRef = useRef<Group>(null);
  const coronaTexture = useMemo(() => createCoronaTexture(), []);
  const source = lightingRig.eclipseSource;
  const sourceColor = useMemo(() => new Color(...source.color), [source.color]);
  const sourceVisible = settings.stage !== "beauty" || source.intensity > 0;

  useLayoutEffect(() => {
    const group = groupRef.current;
    if (!group) return;
    enableReflectionProbeLayer(group);
  }, []);

  useLayoutEffect(
    () => () => {
      coronaTexture.dispose();
    },
    [coronaTexture],
  );

  return (
    <group ref={groupRef} position={lightingRig.origin} visible={sourceVisible}>
      <mesh
        position={source.position}
        renderOrder={-10}
        rotation={source.rotation}
        scale={[source.radius * 2, source.radius * 2, 1]}
      >
        <planeGeometry args={[1, 1, 1, 1]} />
        <meshBasicMaterial
          blending={AdditiveBlending}
          color={sourceColor}
          depthTest
          depthWrite={false}
          map={coronaTexture}
          opacity={source.opacity}
          side={DoubleSide}
          toneMapped={false}
          transparent
        />
      </mesh>
      <rectAreaLight
        color="#fff1c7"
        height={source.rectHeight}
        intensity={runtime.directLights ? source.rectIntensity : 0}
        position={source.position}
        rotation={source.rotation}
        width={source.rectWidth}
      />
    </group>
  );
}

function createCoronaTexture(): Texture {
  const canvas = document.createElement("canvas");
  canvas.width = 512;
  canvas.height = 512;
  const context = canvas.getContext("2d");
  if (!context) {
    return new CanvasTexture(canvas);
  }

  const center = canvas.width * 0.5;
  const gradient = context.createRadialGradient(center, center, 0, center, center, center);
  gradient.addColorStop(0, "rgba(255,255,255,1)");
  gradient.addColorStop(0.24, "rgba(255,249,220,0.96)");
  gradient.addColorStop(0.38, "rgba(255,240,190,0.72)");
  gradient.addColorStop(0.54, "rgba(180,225,255,0.34)");
  gradient.addColorStop(0.74, "rgba(100,170,255,0.12)");
  gradient.addColorStop(1, "rgba(255,255,255,0)");
  context.fillStyle = gradient;
  context.fillRect(0, 0, canvas.width, canvas.height);

  const texture = new CanvasTexture(canvas);
  texture.colorSpace = SRGBColorSpace;
  texture.magFilter = LinearFilter;
  texture.minFilter = LinearFilter;
  texture.needsUpdate = true;
  return texture;
}
