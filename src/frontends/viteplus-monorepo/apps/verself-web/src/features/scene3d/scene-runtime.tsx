import { createContext, useContext, useMemo, type ReactNode } from "react";
import { Vector2, Vector3 } from "three";

interface ScrollRuntime {
  current: number;
  target: number;
  velocity: number;
}

interface CameraRuntime {
  focusDistance: number;
  target: Vector3;
}

interface LightRuntime {
  intensity: number;
  position: Vector3;
}

export interface SceneRuntimeState {
  readonly camera: CameraRuntime;
  readonly light: LightRuntime;
  readonly pointer: Vector2;
  pointerActive: boolean;
  readonly pointerTarget: Vector2;
  readonly scroll: ScrollRuntime;
}

const SceneRuntimeContext = createContext<SceneRuntimeState | null>(null);

function createSceneRuntime(): SceneRuntimeState {
  return {
    camera: {
      focusDistance: 6,
      target: new Vector3(0, -0.4, 0),
    },
    light: {
      intensity: 0,
      position: new Vector3(),
    },
    pointer: new Vector2(),
    pointerActive: false,
    pointerTarget: new Vector2(),
    scroll: {
      current: 0,
      target: 0,
      velocity: 0,
    },
  };
}

export function SceneRuntime({ children }: { readonly children: ReactNode }) {
  const runtime = useMemo(createSceneRuntime, []);

  return <SceneRuntimeContext.Provider value={runtime}>{children}</SceneRuntimeContext.Provider>;
}

export function useSceneRuntime() {
  const runtime = useContext(SceneRuntimeContext);
  if (runtime === null) {
    throw new Error("scene runtime is only available inside <SceneRuntime>");
  }

  return runtime;
}
