import { useEffect, useRef } from "react";
import {
  AdditiveBlending,
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
  firstLightFragmentShader,
  firstLightShaderSourceHash,
  firstLightVertexShader,
} from "../shader/first-light.generated";
import { FIRST_LIGHT_TOTAL_MS } from "../shader/envelopes";
import type { DegradedReason, FirstLightGeometry } from "../types";
import { arrivalFrameMetrics } from "./metrics";

interface FirstLightCanvasProps {
  readonly active: boolean;
  readonly geometry: FirstLightGeometry;
  readonly onDegraded: (reason: DegradedReason, error?: unknown) => void;
}

export function FirstLightCanvas({ active, geometry, onDegraded }: FirstLightCanvasProps) {
  const hostRef = useRef<HTMLDivElement>(null);
  const activeRef = useRef(active);
  const geometryRef = useRef(geometry);
  const onDegradedRef = useRef(onDegraded);

  useEffect(() => {
    activeRef.current = active;
  }, [active]);

  useEffect(() => {
    geometryRef.current = geometry;
  }, [geometry]);

  useEffect(() => {
    onDegradedRef.current = onDegraded;
  }, [onDegraded]);

  useEffect(() => {
    const host = hostRef.current;
    if (!host) return;

    let renderer: WebGLRenderer;
    try {
      renderer = new WebGLRenderer({
        alpha: true,
        antialias: false,
        powerPreference: "high-performance",
      });
    } catch (error) {
      onDegradedRef.current("renderer_init_failed", error);
      return;
    }
    renderer.setClearColor(0x000000, 0);
    renderer.setPixelRatio(Math.min(geometryRef.current.viewport.dpr, 2));
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
      uTime: { value: 0 },
      uActive: { value: activeRef.current ? 1 : 0 },
      uResolution: { value: new Vector2() },
      uTrailRect: { value: new Vector4() },
      uWingsRect: { value: new Vector4() },
    };
    const material = new ShaderMaterial({
      vertexShader: firstLightVertexShader,
      fragmentShader: firstLightFragmentShader,
      uniforms,
      glslVersion: GLSL3,
      transparent: true,
      depthTest: false,
      depthWrite: false,
      blending: AdditiveBlending,
    });
    const plane = new Mesh(new PlaneGeometry(2, 2), material);
    plane.frustumCulled = false;
    scene.add(plane);

    const frameTimes: Array<number> = [];
    let arrivalSent = false;
    let animationFrame = 0;
    let lastFrame = performance.now();
    let degraded = false;

    const markDegraded = (reason: DegradedReason, error?: unknown) => {
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
      const current = geometryRef.current;
      renderer.setSize(current.viewport.w, current.viewport.h, false);
      renderer.setPixelRatio(Math.min(current.viewport.dpr, 2));
      uniforms.uResolution.value.set(
        current.viewport.w * current.viewport.dpr,
        current.viewport.h * current.viewport.dpr,
      );
      uniforms.uTrailRect.value.set(
        current.trail.x,
        current.trail.y,
        current.trail.w,
        current.trail.h,
      );
      uniforms.uWingsRect.value.set(
        current.wings.x,
        current.wings.y,
        current.wings.w,
        current.wings.h,
      );
    };

    const render = (now: number) => {
      if (degraded) return;
      syncGeometry();
      uniforms.uActive.value = activeRef.current ? 1 : 0;
      if (activeRef.current) {
        const deltaMs = Math.min(now - lastFrame, 50);
        uniforms.uTime.value += deltaMs / 1000;
        const elapsedMs = uniforms.uTime.value * 1000;
        if (elapsedMs <= FIRST_LIGHT_TOTAL_MS) {
          frameTimes.push(deltaMs);
        }
        if (!arrivalSent && elapsedMs >= FIRST_LIGHT_TOTAL_MS) {
          arrivalSent = true;
          const metrics = arrivalFrameMetrics(frameTimes);
          emitSpan("company.first_light.arrival_complete", {
            "frame_time.p50": String(metrics.p50),
            "frame_time.p99": String(metrics.p99),
            "frame_time.samples": String(metrics.samples),
            "shader.hash": firstLightShaderSourceHash,
          });
        }
      }
      lastFrame = now;
      try {
        renderer.render(scene, camera);
      } catch (error) {
        markDegraded("compile_error", error);
        return;
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
