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
import { dotMatrixWaveShaderSourceHash } from "./shader/wave-simulation.generated";
import { drainDotMatrixClicks, recordSyntheticDotMatrixClick } from "./interaction";
import { createCompiledWaveScene } from "./scene/compile";
import { collectShadowRegions, DOT_SHADOW_MASK_SELECTOR } from "./scene/dom-regions";
import { createLandingWaveScene } from "./scene/model";
import type { DomainRect, Vec2, WaveSceneModel } from "./scene/types";
import { sampleDotMatrixTimeline } from "./timeline";
import {
  DOT_MATRIX_DOT_SPACING_CSS_PX,
  DOT_MATRIX_RENDER_DPR_CAP,
  type DotMatrixDegradedReason,
  type DotMatrixFrame,
} from "./types";
import { createAmbientWaveScheduler } from "./wave/ambient";
import { createFixedTimestepLoop } from "./wave/fixed-timestep";
import { createClickWaveImpulse, createWaveImpulseQueue } from "./wave/impulses";
import { createDotMatrixWaveInspection } from "./wave/inspection";
import { createDotMatrixWaveSimulation } from "./wave/simulation";
import {
  DOT_MATRIX_MAX_WAVE_IMPULSES,
  DOT_MATRIX_WAVE_FIXED_STEP_SECONDS,
  DOT_MATRIX_WAVE_TRAVEL_SPEED_SCALE,
} from "./wave/types";

interface DotMatrixCanvasProps {
  readonly active: boolean;
  readonly frame: DotMatrixFrame;
  readonly onDegraded: (reason: DotMatrixDegradedReason, error?: unknown) => void;
  readonly onReady: () => void;
}

const DOT_MATRIX_MAX_SHADOW_MASKS = 32;
const DOT_MATRIX_WAVE_FOCUS_SELECTOR = "[data-wave-focus]";

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
        preserveDrawingBuffer: search.has("capture-buffer"),
      });
    } catch (error) {
      onDegradedRef.current("renderer_init_failed", error);
      return;
    }

    if (renderer.extensions.get("EXT_color_buffer_float") === null) {
      renderer.dispose();
      onDegradedRef.current("float_render_target_unsupported");
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

    const geometry = new PlaneGeometry(2, 2);
    const createSceneModel = (): WaveSceneModel => {
      const current = frameRef.current;
      return createLandingWaveScene({
        viewport: current.viewport,
      });
    };
    let sceneVersion = 0;
    const requestSceneRefresh = () => {
      sceneVersion += 1;
    };
    const initialDpr = resolveRendererDpr(frameRef.current.viewport.dpr);
    const compiledScene = createCompiledWaveScene(createSceneModel(), {
      height: 192,
      width: 192,
    });
    const waveSimulation = createDotMatrixWaveSimulation(
      renderer,
      camera,
      geometry,
      compiledScene.texture,
    );
    const impulseQueue = createWaveImpulseQueue(36);
    const ambientWaves = createAmbientWaveScheduler({ seed: createSessionSeed() });
    const autoClickRate = resolveAutoClickRate(search);
    const fixedTimestep = createFixedTimestepLoop({
      maxFrameSeconds: 0.1 * DOT_MATRIX_WAVE_TRAVEL_SPEED_SCALE,
      maxSubSteps: Math.ceil(2 * DOT_MATRIX_WAVE_TRAVEL_SPEED_SCALE),
      stepSeconds: DOT_MATRIX_WAVE_FIXED_STEP_SECONDS,
    });
    const inspection = search.has("wave-inspection")
      ? createDotMatrixWaveInspection(900)
      : undefined;
    inspection?.install();

    const readableResizeObserver =
      typeof ResizeObserver === "function" ? new ResizeObserver(requestSceneRefresh) : undefined;
    readableResizeObserver?.observe(host);
    document
      .querySelectorAll<HTMLElement>(DOT_SHADOW_MASK_SELECTOR)
      .forEach((element) => readableResizeObserver?.observe(element));
    document
      .querySelectorAll<HTMLElement>(DOT_MATRIX_WAVE_FOCUS_SELECTOR)
      .forEach((element) => readableResizeObserver?.observe(element));
    let disposed = false;
    document.fonts?.ready
      .then(() => {
        if (!disposed) requestSceneRefresh();
      })
      .catch(() => {});
    let autoClickIndex = 0;
    const autoClickInterval =
      autoClickRate === undefined
        ? undefined
        : window.setInterval(() => {
            const band = autoClickIndex % 3;
            autoClickIndex += 1;
            recordSyntheticDotMatrixClick({
              timeStamp: performance.now(),
              x: Math.random(),
              y: (band + Math.random()) / 3,
            });
          }, 1000 / autoClickRate);
    const shadowMaskRects = Array.from(
      { length: DOT_MATRIX_MAX_SHADOW_MASKS },
      () => new Vector4(),
    );
    const shadowMaskParams = Array.from(
      { length: DOT_MATRIX_MAX_SHADOW_MASKS },
      () => new Vector4(),
    );
    const shadowMaskFalloff = Array.from(
      { length: DOT_MATRIX_MAX_SHADOW_MASKS },
      () => new Vector4(),
    );

    const uniforms = {
      uActive: { value: activeRef.current ? 1 : 0 },
      uAmbient: { value: 0 },
      uContrast: { value: 1 },
      uDpr: { value: 1 },
      uDotSpacingPx: { value: DOT_MATRIX_DOT_SPACING_CSS_PX },
      uPhase: { value: 0 },
      uResolution: { value: new Vector2() },
      uSeed: { value: 0.38196601125 },
      uCameraRect: { value: cameraRectVector(compiledScene.cameraRect) },
      uShadowMaskCount: { value: 0 },
      uShadowMaskFalloff: { value: shadowMaskFalloff },
      uShadowMaskParams: { value: shadowMaskParams },
      uShadowMaskRects: { value: shadowMaskRects },
      uWaveState: { value: waveSimulation.texture() },
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
    const plane = new Mesh(geometry, material);
    plane.frustumCulled = false;
    scene.add(plane);

    let animationFrame = 0;
    let degraded = false;
    let firstFrameSent = false;
    let lastGeometryKey = "";
    let lastMaskKey = "";
    let lastSceneKey = `${rendererGeometryKey(frameRef.current, initialDpr)}:${sceneVersion}`;
    let lastFrame = performance.now();
    let nextInspectionSeconds = 0;
    let nextSceneRefresh = Number.POSITIVE_INFINITY;
    let simulationSeconds = 0;
    let timelineSeconds = 0;
    let waveFocusPoint = resolveWaveFocusPoint(host, frameRef.current.viewport);

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

    const syncShadowRegions = (dpr: number) => {
      const regions = collectShadowRegions({ dpr, host });
      const regionCount = Math.min(regions.length, DOT_MATRIX_MAX_SHADOW_MASKS);
      uniforms.uShadowMaskCount.value = regionCount;

      for (let index = 0; index < DOT_MATRIX_MAX_SHADOW_MASKS; index += 1) {
        const rectUniform = shadowMaskRects[index];
        const paramUniform = shadowMaskParams[index];
        const falloffUniform = shadowMaskFalloff[index];
        const region = regions[index];
        if (
          rectUniform === undefined ||
          paramUniform === undefined ||
          falloffUniform === undefined
        ) {
          continue;
        }

        if (region !== undefined && index < regionCount) {
          rectUniform.set(
            region.rectPx.x,
            region.rectPx.y,
            region.rectPx.width,
            region.rectPx.height,
          );
          falloffUniform.set(region.falloffPower, 0, 0, 0);
          paramUniform.set(
            region.radiusPx,
            region.featherPx,
            region.strength,
            region.mode === "outside" ? 1 : 0,
          );
        } else {
          rectUniform.set(0, 0, 0, 0);
          falloffUniform.set(1, 0, 0, 0);
          paramUniform.set(0, 0, 0, 0);
        }
      }
    };

    const syncGeometry = (now: number) => {
      const current = frameRef.current;
      const dpr = resolveRendererDpr(current.viewport.dpr);
      const geometryKey = rendererGeometryKey(current, dpr);
      if (geometryKey !== lastGeometryKey) {
        renderer.setPixelRatio(dpr);
        renderer.setSize(current.viewport.w, current.viewport.h, false);
        uniforms.uResolution.value.set(
          Math.max(1, current.viewport.w * dpr),
          Math.max(1, current.viewport.h * dpr),
        );
        uniforms.uDpr.value = dpr;
        waveSimulation.resize(current.viewport.w, current.viewport.h);
        lastGeometryKey = geometryKey;
      }

      const sceneKey = `${geometryKey}:${sceneVersion}`;
      if (sceneKey !== lastSceneKey || now >= nextSceneRefresh) {
        compiledScene.update(createSceneModel());
        uniforms.uCameraRect.value.copy(cameraRectVector(compiledScene.cameraRect));
        waveSimulation.setFieldTexture(compiledScene.texture);
        waveFocusPoint = resolveWaveFocusPoint(host, current.viewport);
        lastSceneKey = sceneKey;
        nextSceneRefresh = Number.POSITIVE_INFINITY;
      }

      const maskKey = `${geometryKey}:${sceneVersion}`;
      if (maskKey !== lastMaskKey) {
        syncShadowRegions(dpr);
        lastMaskKey = maskKey;
      }
    };

    const render = (now: number) => {
      if (degraded) return;
      const frameDeltaSeconds = Math.min(Math.max(now - lastFrame, 0), 100) / 1000;
      lastFrame = now;
      if (activeRef.current) {
        timelineSeconds += frameDeltaSeconds;
      }
      uniforms.uActive.value = activeRef.current ? 1 : 0;
      const timeline = sampleDotMatrixTimeline(timelineSeconds);
      uniforms.uAmbient.value = timeline.ambient;
      uniforms.uContrast.value = timeline.contrast;
      uniforms.uPhase.value = timeline.phase;

      const clickImpulses = drainDotMatrixClicks().map((click) =>
        createClickWaveImpulse(click, compiledScene.cameraRect),
      );
      const ambientImpulses = activeRef.current
        ? ambientWaves.collect({
            ambient: timeline.ambient,
            cameraRect: compiledScene.cameraRect,
            fields: compiledScene.fields,
            focusPoint: waveFocusPoint,
            ignition: timeline.ignition,
            nowSeconds: timelineSeconds,
            viewport: frameRef.current.viewport,
          })
        : [];
      impulseQueue.pushMany(clickImpulses);
      impulseQueue.pushMany(ambientImpulses);

      syncGeometry(now);

      const fixedFrame = fixedTimestep.advance(
        frameDeltaSeconds * DOT_MATRIX_WAVE_TRAVEL_SPEED_SCALE,
        activeRef.current,
      );
      try {
        for (let stepIndex = 0; stepIndex < fixedFrame.steps; stepIndex += 1) {
          uniforms.uWaveState.value = waveSimulation.step({
            active: activeRef.current,
            ambient: timeline.ambient,
            deltaSeconds: fixedFrame.stepSeconds,
            impulses: impulseQueue.drain(DOT_MATRIX_MAX_WAVE_IMPULSES),
          });
          simulationSeconds += fixedFrame.stepSeconds;
        }
        renderer.render(scene, camera);
      } catch (error) {
        markDegraded("compile_error", error);
        return;
      }

      if (inspection !== undefined) {
        const nowSeconds = now / 1000;
        const shouldInspectTexture = nowSeconds >= nextInspectionSeconds;
        const textureStats = shouldInspectTexture ? waveSimulation.inspect() : undefined;
        if (shouldInspectTexture) {
          nextInspectionSeconds = nowSeconds + 0.25;
        }
        inspection.record({
          ambientFrontLengths: ambientImpulses
            .map((impulse) => impulse.frontLength)
            .filter((frontLength): frontLength is number => frontLength !== undefined),
          ambientFrontImpulses: ambientImpulses.filter((impulse) => impulse.kind === "front")
            .length,
          ambientImpulseOrigins: ambientImpulses.map((impulse) => ({
            x: impulse.x,
            y: impulse.y,
          })),
          ambientImpulses: ambientImpulses.length,
          clickImpulses: clickImpulses.length,
          droppedSeconds: fixedFrame.droppedSeconds,
          fieldRegionCounts: countFieldRegions(compiledScene.model),
          frameDeltaSeconds,
          queuedImpulses: impulseQueue.size(),
          simulationSeconds,
          steps: fixedFrame.steps,
          timeSeconds: nowSeconds,
          travelSpeedScale: DOT_MATRIX_WAVE_TRAVEL_SPEED_SCALE,
          ...(textureStats === undefined ? {} : { textureStats }),
        });
      }

      if (!firstFrameSent) {
        firstFrameSent = true;
        onReadyRef.current();
        emitSpan("verself.landing_dot_matrix.ready", {
          "shader.hash": `${dotMatrixShaderSourceHash}:${dotMatrixWaveShaderSourceHash}`,
        });
      }
      animationFrame = window.requestAnimationFrame(render);
    };

    syncGeometry(performance.now());
    animationFrame = window.requestAnimationFrame(render);

    return () => {
      disposed = true;
      window.cancelAnimationFrame(animationFrame);
      renderer.domElement.removeEventListener("webglcontextlost", handleContextLost);
      readableResizeObserver?.disconnect();
      if (autoClickInterval !== undefined) window.clearInterval(autoClickInterval);
      inspection?.uninstall();
      scene.remove(plane);
      compiledScene.dispose();
      waveSimulation.dispose();
      geometry.dispose();
      material.dispose();
      renderer.dispose();
      renderer.domElement.remove();
    };
  }, []);

  return <div ref={hostRef} className="absolute inset-0" />;
}

function cameraRectVector(rect: DomainRect): Vector4 {
  return new Vector4(rect.x, rect.y, rect.width, rect.height);
}

function resolveWaveFocusPoint(
  host: HTMLElement,
  viewport: { readonly h: number; readonly w: number },
): Vec2 {
  const focusElement = document.querySelector<HTMLElement>(DOT_MATRIX_WAVE_FOCUS_SELECTOR);
  if (focusElement === null) {
    return resolveFallbackWaveFocusPoint(viewport);
  }

  const hostBox = host.getBoundingClientRect();
  const focusBox = focusElement.getBoundingClientRect();
  if (hostBox.width <= 0 || hostBox.height <= 0 || focusBox.width <= 0 || focusBox.height <= 0) {
    return resolveFallbackWaveFocusPoint(viewport);
  }

  return {
    x: clamp01((focusBox.left + focusBox.width * 0.5 - hostBox.left) / hostBox.width),
    y: clamp01(1 - (focusBox.top + focusBox.height * 0.5 - hostBox.top) / hostBox.height),
  };
}

function resolveFallbackWaveFocusPoint(_viewport: {
  readonly h: number;
  readonly w: number;
}): Vec2 {
  return { x: 0.5, y: 0.62 };
}

function rendererGeometryKey(frame: DotMatrixFrame, dpr: number): string {
  return `${frame.viewport.w}:${frame.viewport.h}:${dpr}`;
}

function resolveRendererDpr(dpr: number): number {
  return Math.min(Math.max(dpr, 1), DOT_MATRIX_RENDER_DPR_CAP);
}

function countFieldRegions(model: WaveSceneModel) {
  return {
    absorption: model.regions.filter((region) => region.field === "absorption").length,
    readability: model.regions.filter((region) => region.field === "readability").length,
    spawn: model.regions.filter((region) => region.field === "spawn").length,
    visibility: model.regions.filter((region) => region.field === "visibility").length,
  };
}

function createSessionSeed(): number {
  const values = new Uint32Array(1);
  window.crypto?.getRandomValues(values);
  return values[0] ?? Math.floor(Math.random() * 0xffffffff);
}

function resolveAutoClickRate(search: URLSearchParams): number | undefined {
  const value = search.get("auto-clicks") ?? search.get("dot-matrix-auto-clicks");
  if (value === null) return undefined;
  if (value === "" || value === "1" || value === "true") return 3;

  const parsed = Number(value);
  if (!Number.isFinite(parsed) || parsed <= 0) return 3;
  return Math.min(parsed, 12);
}

function clamp01(value: number): number {
  return Math.min(1, Math.max(0, value));
}
