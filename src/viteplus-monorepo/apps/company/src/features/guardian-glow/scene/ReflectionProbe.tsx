import { useFrame, useThree } from "@react-three/fiber";
import type { JSX } from "react";
import { useEffect, useMemo, useRef } from "react";
import {
  CubeCamera,
  CubeReflectionMapping,
  HalfFloatType,
  LinearFilter,
  LinearMipmapLinearFilter,
  WebGLCubeRenderTarget,
} from "three";
import type { GuardianGlowRuntime } from "../guardian-glow-config";
import { guardianGlowMetricIds, setGuardianGlowMetric } from "../telemetry/metrics";
import { REFLECTION_PROBE_LAYER } from "./layers";

type ReflectionProbeResult = {
  readonly node: JSX.Element;
  readonly texture: WebGLCubeRenderTarget["texture"];
};

export function useReflectionProbe(runtime: GuardianGlowRuntime): ReflectionProbeResult {
  const { gl, scene } = useThree();
  const frameRef = useRef(0);
  const updatesRef = useRef(0);

  const renderTarget = useMemo(() => {
    const target = new WebGLCubeRenderTarget(runtime.probeResolution, {
      generateMipmaps: true,
      magFilter: LinearFilter,
      minFilter: LinearMipmapLinearFilter,
      type: HalfFloatType,
    });
    target.texture.mapping = CubeReflectionMapping;
    target.texture.name = `guardian-glow-probe-${runtime.probeResolution}`;
    return target;
  }, [runtime.probeResolution]);

  const cubeCamera = useMemo(() => {
    const camera = new CubeCamera(0.08, 24, renderTarget);
    camera.layers.set(REFLECTION_PROBE_LAYER);
    camera.position.set(0, -0.06, 0.32);
    return camera;
  }, [renderTarget]);

  useEffect(
    () => () => {
      renderTarget.dispose();
    },
    [renderTarget],
  );

  useEffect(() => {
    setGuardianGlowMetric(guardianGlowMetricIds.probeRes, `${runtime.probeResolution}px`);
    setGuardianGlowMetric(guardianGlowMetricIds.probeRate, `${runtime.probeFrameInterval}f`);
    if (!runtime.dynamicProbe) {
      setGuardianGlowMetric(guardianGlowMetricIds.probeCount, "--");
      setGuardianGlowMetric(guardianGlowMetricIds.probeMs, "--");
    }
  }, [runtime.dynamicProbe, runtime.probeFrameInterval, runtime.probeResolution]);

  useFrame(() => {
    if (!runtime.dynamicProbe) {
      return;
    }

    frameRef.current += 1;
    if (updatesRef.current > 0 && frameRef.current % runtime.probeFrameInterval !== 0) {
      return;
    }

    const start = performance.now();
    const previousAutoClear = gl.autoClear;
    const previousRenderTarget = gl.getRenderTarget();

    gl.autoClear = true;
    cubeCamera.update(gl, scene);
    gl.setRenderTarget(previousRenderTarget);
    gl.autoClear = previousAutoClear;

    updatesRef.current += 1;
    setGuardianGlowMetric(guardianGlowMetricIds.probeCount, updatesRef.current.toString());
    setGuardianGlowMetric(guardianGlowMetricIds.probeMs, (performance.now() - start).toFixed(2));
  });

  return {
    node: <primitive object={cubeCamera} visible={false} />,
    texture: renderTarget.texture,
  };
}
