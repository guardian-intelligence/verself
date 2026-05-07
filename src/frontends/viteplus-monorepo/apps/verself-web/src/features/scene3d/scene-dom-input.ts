import type { PointerEvent } from "react";

interface ScenePointerState {
  active: boolean;
  x: number;
  y: number;
}

const scenePointer: ScenePointerState = {
  active: false,
  x: 0,
  y: 0,
};

export function captureScenePointer(event: PointerEvent<HTMLElement>) {
  const bounds = event.currentTarget.getBoundingClientRect();
  const width = Math.max(bounds.width, 1);
  const height = Math.max(bounds.height, 1);

  scenePointer.active = true;
  scenePointer.x = ((event.clientX - bounds.left) / width) * 2 - 1;
  scenePointer.y = -(((event.clientY - bounds.top) / height) * 2 - 1);
}

export function releaseScenePointer() {
  scenePointer.active = false;
}

export function readScenePointer() {
  return scenePointer;
}
