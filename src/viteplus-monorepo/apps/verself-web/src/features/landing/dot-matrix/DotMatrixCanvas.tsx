import { Canvas, useFrame, useThree } from "@react-three/fiber";
import { Bloom, EffectComposer, Vignette } from "@react-three/postprocessing";
import type { RefObject } from "react";
import { useMemo, useRef } from "react";
import { HalfFloatType, NoToneMapping } from "three";

import { DotMatrixEngineObject } from "./engine/DotMatrixEngineObject";
import { DOT_MATRIX_RENDER_DPR_RANGE } from "./engine/frame-budget";

interface DotMatrixCanvasProps {
  readonly hostRef: RefObject<HTMLElement | null>;
}

export function DotMatrixCanvas({ hostRef }: DotMatrixCanvasProps) {
  const stageRef = useRef<HTMLDivElement>(null);
  const reducedMotion = useMemo(resolveReducedMotion, []);

  if (reducedMotion) return null;

  return (
    <div ref={stageRef} className="absolute inset-0 opacity-0 transition-opacity duration-700">
      <Canvas
        orthographic
        camera={{ far: 10, near: 0.1, position: [0, 0, 1], zoom: 1 }}
        dpr={DOT_MATRIX_RENDER_DPR_RANGE}
        frameloop="always"
        gl={{
          alpha: true,
          antialias: false,
          powerPreference: "high-performance",
          toneMapping: NoToneMapping,
        }}
        onCreated={({ gl }) => {
          gl.setClearColor(0x000000, 0);
        }}
        style={{ height: "100%", pointerEvents: "none", width: "100%" }}
      >
        <DotMatrixScene hostRef={hostRef} stageRef={stageRef} />
        <DotMatrixPostProcessing />
      </Canvas>
    </div>
  );
}

interface DotMatrixSceneProps {
  readonly hostRef: RefObject<HTMLElement | null>;
  readonly stageRef: RefObject<HTMLElement | null>;
}

function DotMatrixScene({ hostRef, stageRef }: DotMatrixSceneProps) {
  const { camera, gl } = useThree();
  const engine = useMemo(
    () => new DotMatrixEngineObject({ camera, hostRef, renderer: gl, stageRef }),
    [camera, gl, hostRef, stageRef],
  );
  const bindEngineLifecycle = useMemo(
    () => (object: DotMatrixEngineObject | null) => {
      if (object === null) return;
      return () => object.dispose();
    },
    [],
  );

  useFrame((state, deltaSeconds) => {
    engine.frame({
      deltaSeconds,
      nowSeconds: state.clock.elapsedTime,
    });
  }, -1);

  return <primitive ref={bindEngineLifecycle} object={engine} />;
}

function DotMatrixPostProcessing() {
  return (
    <EffectComposer autoClear frameBufferType={HalfFloatType} multisampling={0} renderPriority={1}>
      <Bloom intensity={0.1} luminanceSmoothing={0.18} luminanceThreshold={0.82} mipmapBlur />
      <Vignette darkness={0.18} offset={0.24} />
    </EffectComposer>
  );
}

function resolveReducedMotion(): boolean {
  if (typeof window === "undefined") return true;
  return window.matchMedia("(prefers-reduced-motion: reduce)").matches;
}
