import { useFrame } from "@react-three/fiber";
import { useRef } from "react";
import type { Mesh } from "three";
import { firstLightCubePosition } from "./first-light-config";

export function FirstLightWorld() {
  const cube = useRef<Mesh>(null);

  useFrame((_state, delta) => {
    if (!cube.current) {
      return;
    }

    cube.current.rotation.x += delta * 0.18;
    cube.current.rotation.y += delta * 0.28;
  });

  return (
    <mesh
      ref={cube}
      castShadow
      receiveShadow
      position={firstLightCubePosition}
      rotation={[0.46, 0.64, 0.08]}
    >
      <boxGeometry args={[1.08, 1.08, 1.08]} />
      <meshStandardMaterial
        color="#d9dde2"
        emissive="#4a3723"
        emissiveIntensity={0.025}
        metalness={0.08}
        roughness={0.46}
      />
    </mesh>
  );
}
