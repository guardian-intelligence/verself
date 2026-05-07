import { useFrame } from "@react-three/fiber";
import { useMemo, useRef } from "react";
import { DirectionalLight, MathUtils, PointLight, Vector3 } from "three";
import { firstLightFocusPoint, firstLightStartPoint } from "./scenes/first-light/first-light-config";
import { useSceneRuntime } from "./scene-runtime";

const startPoint = new Vector3(...firstLightStartPoint);
const focusPoint = new Vector3(...firstLightFocusPoint);

export function LightingRig() {
  const runtime = useSceneRuntime();
  const pointLight = useRef<PointLight>(null);
  const fillLight = useRef<DirectionalLight>(null);
  const currentPosition = useMemo(() => new Vector3().copy(startPoint), []);
  const desiredPosition = useMemo(() => new Vector3(), []);
  const orbitOffset = useMemo(() => new Vector3(), []);

  useFrame(({ clock }, delta) => {
    const elapsed = clock.getElapsedTime();
    const reveal = MathUtils.smoothstep(elapsed, 0.2, 3.8);
    const scroll = Math.min(runtime.scroll.current, 1);
    const orbit = elapsed * 0.34 + scroll * 0.48;
    const settle = MathUtils.smoothstep(reveal, 0.12, 0.92);

    desiredPosition
      .copy(focusPoint)
      .add(orbitOffset.set(Math.sin(orbit) * 1.05, Math.cos(orbit * 0.7) * 0.22, Math.cos(orbit) * 0.85));
    desiredPosition.lerpVectors(startPoint, desiredPosition, settle);
    desiredPosition.x += runtime.pointer.x * 0.055;
    desiredPosition.y += runtime.pointer.y * 0.04;
    currentPosition.lerp(desiredPosition, 1 - Math.exp(-delta * 5.8));

    const intensity = MathUtils.lerp(0.18, 9.4, reveal);
    const fillIntensity = MathUtils.lerp(0.04, 0.72, reveal) + scroll * 0.12;

    runtime.light.position.copy(currentPosition);
    runtime.light.intensity = intensity;

    if (pointLight.current) {
      pointLight.current.position.copy(currentPosition);
      pointLight.current.intensity = intensity;
    }

    if (fillLight.current) {
      fillLight.current.intensity = fillIntensity;
    }
  });

  return (
    <>
      <pointLight ref={pointLight} color="#ffd9a1" decay={2} distance={7.8} intensity={0.18} />
      <directionalLight
        ref={fillLight}
        color="#6d8bb1"
        intensity={0.04}
        position={[-1.8, 1.7, 2.2]}
      />
    </>
  );
}
