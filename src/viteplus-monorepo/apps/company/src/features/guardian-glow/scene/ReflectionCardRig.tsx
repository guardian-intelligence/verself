import { useFrame } from "@react-three/fiber";
import { useLayoutEffect, useMemo, useRef } from "react";
import {
  CanvasTexture,
  Color,
  DoubleSide,
  LinearFilter,
  SRGBColorSpace,
  type Group,
  type Texture,
} from "three";
import type { GuardianGlowRuntime } from "../guardian-glow-config";
import { setReflectionProbeLayers } from "./layers";
import {
  guardianRigMotionQuaternion,
  type GuardianLightingRig,
  type ReflectionCardTexture,
} from "./lighting-rig-model";

type ReflectionCardRigProps = {
  readonly lightingRig: GuardianLightingRig;
  readonly runtime: GuardianGlowRuntime;
};

export function ReflectionCardRig({ lightingRig, runtime }: ReflectionCardRigProps) {
  const groupRef = useRef<Group>(null);
  const textures = useReflectionCardTextures();

  useLayoutEffect(() => {
    const group = groupRef.current;
    if (!group) return;
    setReflectionProbeLayers(group, runtime.cardDebugVisible);
  }, [runtime.cardDebugVisible]);

  useFrame(({ clock }) => {
    const group = groupRef.current;
    if (!group) return;

    group.position.copy(lightingRig.origin);
    group.quaternion.copy(guardianRigMotionQuaternion(lightingRig, clock.elapsedTime));
  });

  return (
    <group
      ref={groupRef}
      position={lightingRig.origin}
      visible={runtime.dynamicProbe || runtime.cardDebugVisible}
    >
      {lightingRig.cards.map((card) => (
        <group key={card.id}>
          <mesh position={card.position} rotation={card.rotation} scale={card.scale}>
            <planeGeometry args={[1, 1, 1, 1]} />
            <meshBasicMaterial
              color={new Color(...card.color)}
              depthWrite={false}
              map={textures[card.texture]}
              opacity={runtime.cardDebugVisible ? Math.min(1, card.opacity + 0.22) : card.opacity}
              side={DoubleSide}
              toneMapped={false}
              transparent
            />
          </mesh>
          {runtime.cardDebugVisible ? (
            <mesh position={card.position}>
              <sphereGeometry args={[0.055, 16, 8]} />
              <meshBasicMaterial color={debugCardColor(card.color)} toneMapped={false} />
            </mesh>
          ) : null}
        </group>
      ))}
    </group>
  );
}

function debugCardColor(color: readonly [number, number, number]) {
  const energy = color[0] + color[1] + color[2];
  return energy < 0.05 ? "#5f6570" : new Color(...color);
}

function useReflectionCardTextures(): Record<ReflectionCardTexture, Texture> {
  const textures = useMemo(
    () => ({
      bar: createReflectionCardTexture("bar"),
      edge: createReflectionCardTexture("edge"),
      field: createReflectionCardTexture("field"),
      soft: createReflectionCardTexture("soft"),
    }),
    [],
  );

  useLayoutEffect(
    () => () => {
      Object.values(textures).forEach((texture) => texture.dispose());
    },
    [textures],
  );

  return textures;
}

function createReflectionCardTexture(kind: ReflectionCardTexture): Texture {
  const canvas = document.createElement("canvas");
  canvas.width = 512;
  canvas.height = 512;
  const context = canvas.getContext("2d");
  if (!context) {
    return new CanvasTexture(canvas);
  }

  const horizontal = context.createLinearGradient(0, 0, canvas.width, 0);
  if (kind === "edge") {
    horizontal.addColorStop(0, "rgba(255,255,255,0)");
    horizontal.addColorStop(0.44, "rgba(255,255,255,0.12)");
    horizontal.addColorStop(0.54, "rgba(255,255,255,1)");
    horizontal.addColorStop(0.64, "rgba(255,255,255,0.2)");
    horizontal.addColorStop(1, "rgba(255,255,255,0)");
  } else if (kind === "field") {
    horizontal.addColorStop(0, "rgba(255,255,255,0.08)");
    horizontal.addColorStop(0.22, "rgba(255,255,255,0.74)");
    horizontal.addColorStop(0.5, "rgba(255,255,255,1)");
    horizontal.addColorStop(0.78, "rgba(255,255,255,0.74)");
    horizontal.addColorStop(1, "rgba(255,255,255,0.08)");
  } else {
    horizontal.addColorStop(0, "rgba(255,255,255,0)");
    horizontal.addColorStop(0.2, "rgba(255,255,255,0.48)");
    horizontal.addColorStop(0.5, "rgba(255,255,255,1)");
    horizontal.addColorStop(0.8, "rgba(255,255,255,0.48)");
    horizontal.addColorStop(1, "rgba(255,255,255,0)");
  }

  const vertical = context.createLinearGradient(0, 0, 0, canvas.height);
  const edgeAlpha = kind === "soft" ? 0.02 : 0;
  vertical.addColorStop(0, `rgba(255,255,255,${edgeAlpha})`);
  vertical.addColorStop(0.32, "rgba(255,255,255,0.92)");
  vertical.addColorStop(0.5, "rgba(255,255,255,1)");
  vertical.addColorStop(0.68, "rgba(255,255,255,0.92)");
  vertical.addColorStop(1, `rgba(255,255,255,${edgeAlpha})`);

  context.fillStyle = horizontal;
  context.fillRect(0, 0, canvas.width, canvas.height);
  context.globalCompositeOperation = "destination-in";
  context.fillStyle = vertical;
  context.fillRect(0, 0, canvas.width, canvas.height);

  const texture = new CanvasTexture(canvas);
  texture.colorSpace = SRGBColorSpace;
  texture.magFilter = LinearFilter;
  texture.minFilter = LinearFilter;
  texture.needsUpdate = true;
  return texture;
}
