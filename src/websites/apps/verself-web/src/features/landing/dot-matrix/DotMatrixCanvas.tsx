import { useEffect, useRef } from "react";
import {
  GLSL3,
  Mesh,
  OrthographicCamera,
  PlaneGeometry,
  Scene,
  ShaderMaterial,
  Vector2,
  Vector4,
  WebGLRenderer,
} from "three";

import { emitSpan } from "~/lib/telemetry/browser";
import {
  dotMatrixFragmentShader,
  dotMatrixShaderSourceHash,
  dotMatrixVertexShader,
} from "./shader/dot-matrix.generated";
import { readDotMatrixSignals } from "./interaction";
import type { DotMatrixDegradedReason, DotMatrixFrame } from "./types";

interface DotMatrixCanvasProps {
  readonly active: boolean;
  readonly frame: DotMatrixFrame;
  readonly onDegraded: (reason: DotMatrixDegradedReason, error?: unknown) => void;
  readonly onReady: () => void;
}

export function DotMatrixCanvas({ active, frame, onDegraded, onReady }: DotMatrixCanvasProps) {
  const hostRef = useRef<HTMLDivElement>(null);
  const activeRef = useRef(active);
  const frameRef = useRef(frame);
  const onDegradedRef = useRef(onDegraded);
  const onReadyRef = useRef(onReady);

  useEffect(() => {
    activeRef.current = active;
  }, [active]);

  useEffect(() => {
    frameRef.current = frame;
  }, [frame]);

  useEffect(() => {
    onDegradedRef.current = onDegraded;
  }, [onDegraded]);

  useEffect(() => {
    onReadyRef.current = onReady;
  }, [onReady]);

  useEffect(() => {
    const host = hostRef.current;
    if (!host) return;
    const search = new URLSearchParams(window.location.search);

    let renderer: WebGLRenderer;
    try {
      renderer = new WebGLRenderer({
        alpha: true,
        antialias: false,
        powerPreference: "high-performance",
        preserveDrawingBuffer: search.has("visual-test"),
      });
    } catch (error) {
      onDegradedRef.current("renderer_init_failed", error);
      return;
    }

    renderer.setClearColor(0x000000, 0);
    renderer.domElement.style.position = "absolute";
    renderer.domElement.style.inset = "0";
    renderer.domElement.style.width = "100%";
    renderer.domElement.style.height = "100%";
    renderer.domElement.style.pointerEvents = "none";
    host.append(renderer.domElement);

    const scene = new Scene();
    const camera = new OrthographicCamera(-1, 1, 1, -1, 0.1, 10);
    camera.position.z = 1;

    const uniforms = {
      uActive: { value: activeRef.current ? 1 : 0 },
      uDpr: { value: 1 },
      uLoadProgress: { value: 0 },
      uPointer: { value: new Vector4(0.5, 0.5, 0, 0) },
      uResolution: { value: new Vector2() },
      uScroll: { value: new Vector4(0, 0, 0, 0) },
      uSeed: { value: 0.38196601125 },
      uTime: { value: 0 },
    };
    const material = new ShaderMaterial({
      depthTest: false,
      depthWrite: false,
      fragmentShader: dotMatrixFragmentShader,
      glslVersion: GLSL3,
      transparent: true,
      uniforms,
      vertexShader: dotMatrixVertexShader,
    });
    const plane = new Mesh(new PlaneGeometry(2, 2), material);
    plane.frustumCulled = false;
    scene.add(plane);

    let animationFrame = 0;
    let degraded = false;
    let firstFrameSent = false;
    let lastFrame = performance.now();
    let startTime = lastFrame;

    const markDegraded = (reason: DotMatrixDegradedReason, error?: unknown) => {
      if (degraded) return;
      degraded = true;
      if (animationFrame) {
        window.cancelAnimationFrame(animationFrame);
      }
      onDegradedRef.current(reason, error);
    };

    const handleContextLost = (event: Event) => {
      event.preventDefault();
      markDegraded("context_lost");
    };
    renderer.domElement.addEventListener("webglcontextlost", handleContextLost);

    const syncGeometry = () => {
      const current = frameRef.current;
      const dpr = Math.min(current.viewport.dpr, 2);
      renderer.setPixelRatio(dpr);
      renderer.setSize(current.viewport.w, current.viewport.h, false);
      uniforms.uResolution.value.set(
        Math.max(1, current.viewport.w * dpr),
        Math.max(1, current.viewport.h * dpr),
      );
      uniforms.uDpr.value = dpr;
    };

    const render = (now: number) => {
      if (degraded) return;
      const deltaMs = Math.min(now - lastFrame, 50);
      lastFrame = now;
      if (activeRef.current) {
        uniforms.uTime.value += deltaMs / 1000;
      }
      uniforms.uActive.value = activeRef.current ? 1 : 0;
      uniforms.uLoadProgress.value = Math.min((now - startTime) / 1450, 1);

      const signals = readDotMatrixSignals(now);
      uniforms.uPointer.value.set(
        signals.pointer.x,
        signals.pointer.y,
        signals.pointer.active ? 1 : 0,
        0,
      );
      uniforms.uScroll.value.set(signals.scroll.current, signals.scroll.velocity, 0, 0);

      syncGeometry();

      try {
        renderer.render(scene, camera);
      } catch (error) {
        markDegraded("compile_error", error);
        return;
      }

      if (!firstFrameSent) {
        firstFrameSent = true;
        onReadyRef.current();
        emitSpan("verself.landing_dot_matrix.ready", {
          "shader.hash": dotMatrixShaderSourceHash,
        });
      }
      animationFrame = window.requestAnimationFrame(render);
    };

    syncGeometry();
    animationFrame = window.requestAnimationFrame(render);

    return () => {
      window.cancelAnimationFrame(animationFrame);
      renderer.domElement.removeEventListener("webglcontextlost", handleContextLost);
      scene.remove(plane);
      plane.geometry.dispose();
      material.dispose();
      renderer.dispose();
      renderer.domElement.remove();
    };
  }, []);

  return <div ref={hostRef} className="absolute inset-0" />;
}
