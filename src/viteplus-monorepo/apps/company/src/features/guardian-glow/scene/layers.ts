import type { Object3D } from "three";

export const MAIN_CAMERA_LAYER = 0;
export const REFLECTION_PROBE_LAYER = 1;

export function setReflectionProbeLayers(root: Object3D, showToMainCamera: boolean) {
  root.traverse((object) => {
    object.layers.set(REFLECTION_PROBE_LAYER);
    if (showToMainCamera) {
      object.layers.enable(MAIN_CAMERA_LAYER);
    }
  });
}

export function enableReflectionProbeLayer(root: Object3D) {
  root.traverse((object) => {
    object.layers.enable(REFLECTION_PROBE_LAYER);
  });
}
