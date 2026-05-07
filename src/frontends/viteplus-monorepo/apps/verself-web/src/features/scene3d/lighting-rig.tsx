import { useFrame } from "@react-three/fiber";
import { useMemo, useRef } from "react";
import { DirectionalLight, HemisphereLight, MathUtils, PointLight, Vector3 } from "three";
import {
  firstLightFocusPoint,
  firstLightStartPoint,
} from "./scenes/first-light/first-light-config";
import { useSceneRuntime } from "./scene-runtime";

const startPoint = new Vector3(...firstLightStartPoint);
const focusPoint = new Vector3(...firstLightFocusPoint);

export function LightingRig() {
  const runtime = useSceneRuntime();
  const pointLight = useRef<PointLight>(null);
  const fillLight = useRef<DirectionalLight>(null);
  const bounceLight = useRef<DirectionalLight>(null);
  const rimLight = useRef<DirectionalLight>(null);
  const skyFill = useRef<HemisphereLight>(null);
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
      .add(
        orbitOffset.set(
          Math.sin(orbit) * 1.05,
          Math.cos(orbit * 0.7) * 0.22,
          Math.cos(orbit) * 0.85,
        ),
      );
    desiredPosition.lerpVectors(startPoint, desiredPosition, settle);
    desiredPosition.x += runtime.pointer.x * 0.055;
    desiredPosition.y += runtime.pointer.y * 0.04;
    currentPosition.lerp(desiredPosition, 1 - Math.exp(-delta * 5.8));

    const keyIntensity = MathUtils.lerp(0.08, 4.65, reveal);
    const fillIntensity = MathUtils.lerp(0.16, 0.86, reveal) + scroll * 0.08;
    const bounceIntensity = MathUtils.lerp(0.08, 0.54, reveal) + scroll * 0.06;
    const rimIntensity = MathUtils.lerp(0.02, 0.38, reveal);
    const skyIntensity = MathUtils.lerp(0.1, 0.26, reveal);

    runtime.light.position.copy(currentPosition);
    runtime.light.intensity = keyIntensity;

    if (pointLight.current) {
      pointLight.current.position.copy(currentPosition);
      pointLight.current.intensity = keyIntensity;
    }

    if (fillLight.current) {
      fillLight.current.intensity = fillIntensity;
    }

    if (bounceLight.current) {
      bounceLight.current.intensity = bounceIntensity;
    }

    if (rimLight.current) {
      rimLight.current.intensity = rimIntensity;
    }

    if (skyFill.current) {
      skyFill.current.intensity = skyIntensity;
    }
  });

  return (
    <>
      <hemisphereLight ref={skyFill} color="#a9c7ec" groundColor="#4c2b18" intensity={0.1} />
      <pointLight ref={pointLight} color="#ffd0a0" decay={2} distance={8.8} intensity={0.08} />
      <directionalLight
        ref={fillLight}
        color="#8eb0df"
        intensity={0.16}
        position={[-2.6, 1.5, 2.8]}
      />
      <directionalLight
        ref={bounceLight}
        color="#f0ad6f"
        intensity={0.08}
        position={[2.8, -1.6, 1.4]}
      />
      <directionalLight
        ref={rimLight}
        color="#fff2d1"
        intensity={0.02}
        position={[-1.4, 2.1, -2.5]}
      />
    </>
  );
}
