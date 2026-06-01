import { Canvas, type CanvasProps } from "@react-three/fiber";
import type { ReactNode } from "react";
import { ACESFilmicToneMapping, PCFSoftShadowMap, SRGBColorSpace } from "three";
import { SceneFallback } from "./scene-fallback";

interface SceneCanvasProps {
  readonly camera: NonNullable<CanvasProps["camera"]>;
  readonly children: ReactNode;
  readonly className?: string;
  readonly fallback?: ReactNode;
  readonly interactive?: boolean;
}

export function SceneCanvas({
  camera,
  children,
  className,
  fallback,
  interactive = false,
}: SceneCanvasProps) {
  const fallbackNode = fallback ?? <SceneFallback />;

  return (
    <Canvas
      camera={camera}
      className={className ?? "absolute inset-0"}
      dpr={[1.25, 2]}
      fallback={fallbackNode}
      gl={{ alpha: true, antialias: false, powerPreference: "high-performance" }}
      onCreated={({ gl }) => {
        gl.outputColorSpace = SRGBColorSpace;
        gl.shadowMap.type = PCFSoftShadowMap;
        gl.toneMapping = ACESFilmicToneMapping;
        gl.toneMappingExposure = 1.08;
      }}
      shadows
      style={{ pointerEvents: interactive ? "auto" : "none" }}
    >
      {children}
    </Canvas>
  );
}
