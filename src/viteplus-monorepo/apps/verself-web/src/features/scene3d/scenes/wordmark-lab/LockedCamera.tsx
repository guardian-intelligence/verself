import { useFrame } from "@react-three/fiber";
import { useMemo } from "react";
import { Vector3 } from "three";
import { wordmarkLabCamera } from "./wordmark-lab-config";

export function LockedCamera() {
  const target = useMemo(() => new Vector3(...wordmarkLabCamera.target), []);

  useFrame(({ camera }) => {
    camera.position.set(...wordmarkLabCamera.position);
    camera.lookAt(target);
  });

  return null;
}
