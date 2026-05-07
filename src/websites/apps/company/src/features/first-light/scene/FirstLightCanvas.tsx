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
import { firstLightDebugMode, firstLightDebugModeCode } from "../profile";
import {
  firstLightFragmentShader,
  firstLightShaderSourceHash,
  firstLightVertexShader,
} from "../shader/first-light.generated";
import type { CelestialRenderProfile, DegradedReason, FirstLightFrame } from "../types";
import { frameMetrics } from "./metrics";

interface FirstLightCanvasProps {
  readonly active: boolean;
  readonly frame: FirstLightFrame;
  readonly onDegraded: (reason: DegradedReason, error?: unknown) => void;
  readonly profile: CelestialRenderProfile;
}

export function FirstLightCanvas({ active, frame, onDegraded, profile }: FirstLightCanvasProps) {
  const hostRef = useRef<HTMLDivElement>(null);
  const activeRef = useRef(active);
  const frameRef = useRef(frame);
  const onDegradedRef = useRef(onDegraded);
  const profileRef = useRef(profile);

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
    profileRef.current = profile;
  }, [profile]);

  useEffect(() => {
    const host = hostRef.current;
    if (!host) return;
    const search = new URLSearchParams(window.location.search);
    const debugMode = firstLightDebugMode(window.location.search);

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
    renderer.setPixelRatio(Math.min(frameRef.current.viewport.dpr, 2));
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
      uDpr: { value: 1 },
      uAspect: { value: 1 },
      uMotionScale: { value: 1 },
      uSeed: { value: 0.61803398875 },
      uDebugMode: { value: firstLightDebugModeCode(debugMode) },
      uShapeCenter: { value: new Vector2() },
      uShapeParams: { value: new Vector4() },
      uShapeWarp: { value: new Vector4() },
      uLensParams: { value: new Vector4() },
      uDiskParams: { value: new Vector4() },
      uDiskTone: { value: new Vector4() },
    };
    const material = new ShaderMaterial({
      vertexShader: firstLightVertexShader,
      fragmentShader: firstLightFragmentShader,
      uniforms,
      glslVersion: GLSL3,
      transparent: true,
      depthTest: false,
      depthWrite: false,
    });
    const plane = new Mesh(new PlaneGeometry(2, 2), material);
    plane.frustumCulled = false;
    scene.add(plane);

    const frameTimes: Array<number> = [];
    let settledSent = false;
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
      const current = frameRef.current;
      renderer.setSize(current.viewport.w, current.viewport.h, false);
      renderer.setPixelRatio(Math.min(current.viewport.dpr, 2));
      const dpr = Math.min(current.viewport.dpr, 2);
      uniforms.uResolution.value.set(
        Math.max(1, current.viewport.w * dpr),
        Math.max(1, current.viewport.h * dpr),
      );
      uniforms.uDpr.value = dpr;
      uniforms.uAspect.value = current.viewport.w / Math.max(current.viewport.h, 1);
    };

    const syncProfile = () => {
      const current = profileRef.current;
      uniforms.uMotionScale.value = current.motionScale;
      uniforms.uSeed.value = current.seed;
      uniforms.uShapeCenter.value.set(current.shape.center[0], current.shape.center[1]);
      uniforms.uShapeParams.value.set(
        current.shape.radius,
        current.shape.aspect,
        current.shape.rotation,
        current.shape.exponent,
      );
      uniforms.uShapeWarp.value.set(
        current.shape.pinch,
        current.shape.ripple,
        current.shape.rippleFrequency,
        current.shape.softness,
      );
      uniforms.uLensParams.value.set(
        current.lens.strength,
        current.lens.radius,
        current.lens.falloff,
        current.lens.ringWidth,
      );
      uniforms.uDiskParams.value.set(
        current.disk.tilt,
        current.disk.innerRadius,
        current.disk.outerRadius,
        current.disk.intensity,
      );
      uniforms.uDiskTone.value.set(
        current.disk.warmth,
        current.disk.spin,
        current.disk.anisotropy,
        current.lens.ringIntensity,
      );
    };

    const render = (now: number) => {
      if (degraded) return;
      syncGeometry();
      syncProfile();
      uniforms.uActive.value = activeRef.current ? 1 : 0;
      if (activeRef.current) {
        const deltaMs = Math.min(now - lastFrame, 50);
        uniforms.uTime.value += deltaMs / 1000;
        frameTimes.push(deltaMs);
        if (!settledSent && frameTimes.length >= 90) {
          settledSent = true;
          const metrics = frameMetrics(frameTimes);
          emitSpan("company.first_light.plate_ready", {
            "frame_time.p50": String(metrics.p50),
            "frame_time.p99": String(metrics.p99),
            "frame_time.samples": String(metrics.samples),
            "renderer.debug_mode": debugMode,
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
    syncProfile();
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
