import { useFrame } from "@react-three/fiber";
import { MathUtils } from "three";
import { readScenePointer } from "./scene-dom-input";
import { useSceneRuntime } from "./scene-runtime";

const scrollTravelViewports = 1.35;

export function SceneInputs() {
  const runtime = useSceneRuntime();

  useFrame((_state, delta) => {
    const pointer = readScenePointer();
    const previousScroll = runtime.scroll.current;

    runtime.pointerActive = pointer.active;
    runtime.pointerTarget.set(pointer.x, pointer.y);
    runtime.pointer.lerp(runtime.pointerTarget, 1 - Math.exp(-delta * 4.8));

    const documentElement = document.documentElement;
    const availableScroll = Math.max(documentElement.scrollHeight - window.innerHeight, 1);
    const clampedTravel = Math.max(window.innerHeight * scrollTravelViewports, 1);
    const scrollTarget = Math.min(window.scrollY / Math.min(availableScroll, clampedTravel), 1);

    runtime.scroll.target = scrollTarget;
    runtime.scroll.current = MathUtils.damp(runtime.scroll.current, runtime.scroll.target, 3.2, delta);
    runtime.scroll.velocity = (runtime.scroll.current - previousScroll) / Math.max(delta, 1 / 120);
  });

  return null;
}
