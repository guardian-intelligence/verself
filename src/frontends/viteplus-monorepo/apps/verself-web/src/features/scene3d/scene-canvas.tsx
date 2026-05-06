import { Canvas, type CanvasProps } from "@react-three/fiber";
import type { ReactNode } from "react";
import { SceneFallback } from "./scene-fallback";

interface SceneCanvasProps {
  readonly camera: NonNullable<CanvasProps["camera"]>;
  readonly children: ReactNode;
  readonly className?: string;
  readonly fallback?: ReactNode;
}

export function SceneCanvas({ camera, children, className, fallback }: SceneCanvasProps) {
  const fallbackNode = fallback ?? <SceneFallback />;

  return (
    <Canvas
      camera={camera}
      className={className ?? "absolute inset-0"}
      dpr={[1, 2]}
      fallback={fallbackNode}
      gl={{ alpha: true, antialias: true, powerPreference: "high-performance" }}
      shadows
      style={{ pointerEvents: "none" }}
    >
      {children}
    </Canvas>
  );
}
