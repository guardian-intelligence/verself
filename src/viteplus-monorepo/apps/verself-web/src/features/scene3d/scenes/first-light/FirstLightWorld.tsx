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

    cube.current.rotation.x += delta * 0.13;
    cube.current.rotation.y += delta * 0.18;
    cube.current.rotation.z += delta * 0.025;
  });

  return (
    <mesh
      ref={cube}
      castShadow
      receiveShadow
      position={firstLightCubePosition}
      rotation={[0.58, 0.78, 0.1]}
    >
      <boxGeometry args={[0.82, 0.82, 0.82]} />
      <meshStandardMaterial
        color="#d8d6cf"
        emissive="#3d2b1d"
        emissiveIntensity={0.04}
        metalness={0.02}
        roughness={0.72}
      />
    </mesh>
  );
}
