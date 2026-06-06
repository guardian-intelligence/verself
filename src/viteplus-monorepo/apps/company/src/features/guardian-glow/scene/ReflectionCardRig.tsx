import { useFrame } from "@react-three/fiber";
import { useEffect, useLayoutEffect, useMemo, useRef } from "react";
import {
  CanvasTexture,
  Color,
  DoubleSide,
  LinearFilter,
  SRGBColorSpace,
  type Group,
  type Texture,
} from "three";
import type { GuardianGlowRuntime, GuardianGlowSettings } from "../guardian-glow-config";
import { setReflectionProbeLayers } from "./layers";

type ReflectionCardRigProps = {
  readonly runtime: GuardianGlowRuntime;
  readonly settings: GuardianGlowSettings;
};

type ReflectionCardSpec = {
  readonly color: readonly [number, number, number];
  readonly id: string;
  readonly opacity: number;
  readonly position: readonly [number, number, number];
  readonly rotation: readonly [number, number, number];
  readonly scale: readonly [number, number, number];
  readonly texture: "bar" | "soft" | "edge";
};

export function ReflectionCardRig({ runtime, settings }: ReflectionCardRigProps) {
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

    const sweep = -1.7 + settings.band * 3.4;
    const drift = Math.sin(clock.elapsedTime * 0.38) * 0.1;
    group.position.set(sweep + drift, -0.08 + settings.band * 0.12, 2.25);
  });

  const cards = useMemo(() => reflectionCards(settings), [settings]);

  return (
    <group
      ref={groupRef}
      rotation={[0.2, -0.18, -0.74]}
      visible={runtime.dynamicProbe || runtime.cardDebugVisible}
    >
      {cards.map((card) => (
        <mesh key={card.id} position={card.position} rotation={card.rotation} scale={card.scale}>
          <planeGeometry args={[1, 1, 1, 1]} />
          <meshBasicMaterial
            color={new Color(...card.color)}
            depthWrite={false}
            map={textures[card.texture]}
            opacity={runtime.cardDebugVisible ? Math.min(1, card.opacity + 0.16) : card.opacity}
            side={DoubleSide}
            toneMapped={false}
            transparent
          />
        </mesh>
      ))}
    </group>
  );
}

function reflectionCards(settings: GuardianGlowSettings): readonly ReflectionCardSpec[] {
  const spread = 0.55 + settings.bandWidth * 4.2;
  const gain = settings.cardIntensity;
  const cool = 1.2 + settings.aberration * 0.015;
  const warm = 0.8 + settings.warm * 0.32;

  return [
    {
      color: [gain * 2.6, gain * 2.6, gain * 2.55],
      id: "white-key",
      opacity: 0.92,
      position: [0, 0, 0],
      rotation: [0, 0, 0],
      scale: [spread, 3.85, 1],
      texture: "bar",
    },
    {
      color: [gain * 0.38, gain * 0.82, gain * 2.8 * cool],
      id: "blue-rim",
      opacity: 0.78,
      position: [spread * 0.58, 1.2, -0.14],
      rotation: [0.08, -0.04, 0.18],
      scale: [Math.max(0.18, spread * 0.34), 2.65, 1],
      texture: "edge",
    },
    {
      color: [gain * 2.35 * warm, gain * 1.22 * warm, gain * 0.42],
      id: "warm-foot",
      opacity: 0.64,
      position: [-spread * 0.56, -1.08, -0.1],
      rotation: [-0.05, 0.08, -0.16],
      scale: [Math.max(0.2, spread * 0.38), 2.2, 1],
      texture: "soft",
    },
    {
      color: [gain * 0.52, gain * 0.72, gain * 1.5],
      id: "thin-cyan-edge",
      opacity: 0.68,
      position: [spread * 0.28, -1.48, 0.1],
      rotation: [0.02, -0.1, 0.05],
      scale: [Math.max(0.11, spread * 0.14), 1.65, 1],
      texture: "edge",
    },
  ];
}

function useReflectionCardTextures(): Record<ReflectionCardSpec["texture"], Texture> {
  const textures = useMemo(
    () => ({
      bar: createReflectionCardTexture("bar"),
      edge: createReflectionCardTexture("edge"),
      soft: createReflectionCardTexture("soft"),
    }),
    [],
  );

  useEffect(
    () => () => {
      Object.values(textures).forEach((texture) => texture.dispose());
    },
    [textures],
  );

  return textures;
}

function createReflectionCardTexture(kind: ReflectionCardSpec["texture"]): Texture {
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
