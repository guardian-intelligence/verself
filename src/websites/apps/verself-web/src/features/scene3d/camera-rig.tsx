import { useFrame } from "@react-three/fiber";
import { useMemo } from "react";
import { MathUtils, Vector3 } from "three";
import { useSceneRuntime } from "./scene-runtime";

const basePosition = new Vector3(1.42, 0.38, 6.35);
const scrollOffset = new Vector3(-0.22, 0.16, -0.42);
const baseTarget = new Vector3(0.58, -0.76, 0.02);
const scrollTargetOffset = new Vector3(0.06, 0.06, 0);

export function CameraRig() {
  const runtime = useSceneRuntime();
  const desiredPosition = useMemo(() => new Vector3(), []);
  const desiredTarget = useMemo(() => new Vector3(), []);
  const pointerOffset = useMemo(() => new Vector3(), []);

  useFrame(({ camera }, delta) => {
    const scroll = Math.min(runtime.scroll.current, 1);
    const pointerX = runtime.pointer.x;
    const pointerY = runtime.pointer.y;

    desiredPosition
      .copy(basePosition)
      .addScaledVector(scrollOffset, scroll)
      .add(pointerOffset.set(pointerX * 0.14, pointerY * 0.08, 0));

    desiredTarget
      .copy(baseTarget)
      .addScaledVector(scrollTargetOffset, scroll)
      .add(pointerOffset.set(pointerX * 0.055, pointerY * 0.035, 0));

    camera.position.x = MathUtils.damp(camera.position.x, desiredPosition.x, 2.25, delta);
    camera.position.y = MathUtils.damp(camera.position.y, desiredPosition.y, 2.25, delta);
    camera.position.z = MathUtils.damp(camera.position.z, desiredPosition.z, 2.25, delta);
    camera.lookAt(desiredTarget);

    runtime.camera.target.copy(desiredTarget);
    runtime.camera.focusDistance = camera.position.distanceTo(desiredTarget);
  });

  return null;
}
