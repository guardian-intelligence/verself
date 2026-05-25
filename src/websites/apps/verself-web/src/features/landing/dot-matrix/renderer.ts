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
import { drainDotMatrixClicks, recordSyntheticDotMatrixClick } from "./interaction";
import { createCompiledWaveScene } from "./scene/compile";
import { collectShadowRegions, DOT_SHADOW_MASK_SELECTOR } from "./scene/dom-regions";
import { createLandingWaveScene } from "./scene/model";
import type { DomainRect, Vec2 } from "./scene/types";
import {
  dotMatrixFragmentShader,
  dotMatrixShaderSourceHash,
  dotMatrixVertexShader,
} from "./shader/dot-matrix.generated";
import { dotMatrixWaveShaderSourceHash } from "./shader/wave-simulation.generated";
import { sampleDotMatrixTimeline } from "./timeline";
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

interface DotMatrixRenderer {
  readonly dispose: () => void;
}

interface DotMatrixViewport {
  readonly dpr: number;
  readonly h: number;
  readonly w: number;
}

type DotMatrixDegradedReason =
  | "compile_error"
  | "context_lost"
  | "float_render_target_unsupported"
  | "renderer_init_failed";

type DotMatrixRendererBackend = "none" | "webgl2" | "webgl2_no_float_render_target";

const DOT_MATRIX_DOT_SPACING_CSS_PX = 7.75;
const DOT_MATRIX_MAX_SHADOW_MASKS = 32;
const DOT_MATRIX_RENDER_DPR_CAP = 1.5;
const DOT_MATRIX_WAVE_FOCUS_SELECTOR = "[data-wave-focus]";

export function createDotMatrixRenderer(host: HTMLElement): DotMatrixRenderer {
  const search = new URLSearchParams(window.location.search);
  const reducedMotion = window.matchMedia("(prefers-reduced-motion: reduce)").matches;

  if (reducedMotion) {
    emitCapability("none", true, host);
    return noopRenderer();
  }

  let renderer: WebGLRenderer;
  try {
    renderer = new WebGLRenderer({
      alpha: true,
      antialias: false,
      powerPreference: "high-performance",
      preserveDrawingBuffer: search.has("capture-buffer"),
    });
  } catch (error) {
    emitCapability("none", false, host);
    emitDegraded("renderer_init_failed", error);
    return noopRenderer();
  }

  if (renderer.extensions.get("EXT_color_buffer_float") === null) {
    renderer.dispose();
    emitCapability("webgl2_no_float_render_target", false, host);
    emitDegraded("float_render_target_unsupported");
    return noopRenderer();
  }

  emitCapability("webgl2", false, host);

  renderer.setClearColor(0x000000, 0);
  styleCanvas(renderer.domElement);
  host.append(renderer.domElement);

  const scene = new Scene();
  const camera = new OrthographicCamera(-1, 1, 1, -1, 0.1, 10);
  camera.position.z = 1;

  const geometry = new PlaneGeometry(2, 2);
  const compiledScene = createCompiledWaveScene(createLandingWaveScene(currentViewport(host)), {
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
  const fixedTimestep = createFixedTimestepLoop({
    maxFrameSeconds: 0.1 * DOT_MATRIX_WAVE_TRAVEL_SPEED_SCALE,
    maxSubSteps: Math.ceil(2 * DOT_MATRIX_WAVE_TRAVEL_SPEED_SCALE),
    stepSeconds: DOT_MATRIX_WAVE_FIXED_STEP_SECONDS,
  });
  const inspection = search.has("wave-inspection") ? createDotMatrixWaveInspection(900) : undefined;
  inspection?.install();

  const shadowMaskRects = Array.from({ length: DOT_MATRIX_MAX_SHADOW_MASKS }, () => new Vector4());
  const shadowMaskParams = Array.from({ length: DOT_MATRIX_MAX_SHADOW_MASKS }, () => new Vector4());
  const shadowMaskFalloff = Array.from(
    { length: DOT_MATRIX_MAX_SHADOW_MASKS },
    () => new Vector4(),
  );

  const uniforms = {
    uActive: { value: 1 },
    uAmbient: { value: 0 },
    uCameraRect: { value: cameraRectVector(compiledScene.cameraRect) },
    uContrast: { value: 1 },
    uDotSpacingPx: { value: DOT_MATRIX_DOT_SPACING_CSS_PX },
    uDpr: { value: 1 },
    uPhase: { value: 0 },
    uResolution: { value: new Vector2() },
    uSeed: { value: 0.38196601125 },
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
  let disposed = false;
  let firstFrameSent = false;
  let intersecting = true;
  let lastFrame = performance.now();
  let lastGeometryKey = "";
  let lastMaskKey = "";
  let lastSceneKey = "";
  let nextInspectionSeconds = 0;
  let sceneVersion = 0;
  let simulationSeconds = 0;
  let timelineSeconds = 0;
  let visible = document.visibilityState === "visible";
  let waveFocusPoint = resolveWaveFocusPoint(host, currentViewport(host));
  const autoClickInterval = startAutoClicks(search);
  const resizeObserver = createSceneResizeObserver(host, () => {
    sceneVersion += 1;
  });

  const active = () => visible && intersecting;
  const markDegraded = (reason: DotMatrixDegradedReason, error?: unknown) => {
    if (degraded) return;
    degraded = true;
    if (animationFrame !== 0) window.cancelAnimationFrame(animationFrame);
    host.style.opacity = "0";
    emitDegraded(reason, error);
  };

  const handleVisibilityChange = () => {
    visible = document.visibilityState === "visible";
  };
  document.addEventListener("visibilitychange", handleVisibilityChange);

  const intersectionObserver =
    typeof IntersectionObserver === "function"
      ? new IntersectionObserver(([entry]) => {
          intersecting = entry?.isIntersecting ?? false;
        })
      : undefined;
  intersectionObserver?.observe(host);

  const handleContextLost = (event: Event) => {
    event.preventDefault();
    markDegraded("context_lost");
  };
  renderer.domElement.addEventListener("webglcontextlost", handleContextLost);

  document.fonts?.ready
    .then(() => {
      if (!disposed) sceneVersion += 1;
    })
    .catch(() => {});

  const syncShadowRegions = (dpr: number) => {
    const regions = collectShadowRegions({ dpr, host });
    const regionCount = Math.min(regions.length, DOT_MATRIX_MAX_SHADOW_MASKS);
    uniforms.uShadowMaskCount.value = regionCount;

    for (let index = 0; index < DOT_MATRIX_MAX_SHADOW_MASKS; index += 1) {
      const rectUniform = shadowMaskRects[index];
      const paramUniform = shadowMaskParams[index];
      const falloffUniform = shadowMaskFalloff[index];
      const region = regions[index];
      if (rectUniform === undefined || paramUniform === undefined || falloffUniform === undefined) {
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

  const syncGeometry = () => {
    const viewport = currentViewport(host);
    const dpr = resolveRendererDpr(viewport.dpr);
    const geometryKey = rendererGeometryKey(viewport, dpr);
    if (geometryKey !== lastGeometryKey) {
      renderer.setPixelRatio(dpr);
      renderer.setSize(viewport.w, viewport.h, false);
      uniforms.uResolution.value.set(Math.max(1, viewport.w * dpr), Math.max(1, viewport.h * dpr));
      uniforms.uDpr.value = dpr;
      waveSimulation.resize(viewport.w, viewport.h);
      lastGeometryKey = geometryKey;
    }

    const sceneKey = `${geometryKey}:${sceneVersion}`;
    if (sceneKey !== lastSceneKey) {
      compiledScene.update(createLandingWaveScene(viewport));
      uniforms.uCameraRect.value.copy(cameraRectVector(compiledScene.cameraRect));
      waveSimulation.setFieldTexture(compiledScene.texture);
      waveFocusPoint = resolveWaveFocusPoint(host, viewport);
      lastSceneKey = sceneKey;
    }

    const maskKey = `${geometryKey}:${sceneVersion}`;
    if (maskKey !== lastMaskKey) {
      syncShadowRegions(dpr);
      lastMaskKey = maskKey;
    }

    return viewport;
  };

  const render = (now: number) => {
    if (degraded || disposed) return;

    const frameDeltaSeconds = Math.min(Math.max(now - lastFrame, 0), 100) / 1000;
    lastFrame = now;
    const frameActive = active();
    const viewport = syncGeometry();

    if (frameActive) {
      timelineSeconds += frameDeltaSeconds;
    }
    uniforms.uActive.value = frameActive ? 1 : 0;

    const timeline = sampleDotMatrixTimeline(timelineSeconds);
    uniforms.uAmbient.value = timeline.ambient;
    uniforms.uContrast.value = timeline.contrast;
    uniforms.uPhase.value = timeline.phase;

    const clickImpulses = drainDotMatrixClicks().map((click) =>
      createClickWaveImpulse(click, compiledScene.cameraRect),
    );
    const ambientImpulses = frameActive
      ? ambientWaves.collect({
          ambient: timeline.ambient,
          cameraRect: compiledScene.cameraRect,
          fields: compiledScene.fields,
          focusPoint: waveFocusPoint,
          ignition: timeline.ignition,
          nowSeconds: timelineSeconds,
          viewport,
        })
      : [];
    impulseQueue.pushMany(clickImpulses);
    impulseQueue.pushMany(ambientImpulses);

    const fixedFrame = fixedTimestep.advance(
      frameDeltaSeconds * DOT_MATRIX_WAVE_TRAVEL_SPEED_SCALE,
      frameActive,
    );
    try {
      for (let stepIndex = 0; stepIndex < fixedFrame.steps; stepIndex += 1) {
        uniforms.uWaveState.value = waveSimulation.step({
          active: frameActive,
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
        ambientFrontImpulses: ambientImpulses.filter((impulse) => impulse.kind === "front").length,
        ambientImpulseOrigins: ambientImpulses.map((impulse) => ({
          x: impulse.x,
          y: impulse.y,
        })),
        ambientImpulses: ambientImpulses.length,
        clickImpulses: clickImpulses.length,
        droppedSeconds: fixedFrame.droppedSeconds,
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
      host.dataset.dotMatrixLive = "";
      host.style.opacity = "1";
      emitSpan("verself.landing_dot_matrix.ready", {
        "shader.hash": `${dotMatrixShaderSourceHash}:${dotMatrixWaveShaderSourceHash}`,
      });
    }

    animationFrame = window.requestAnimationFrame(render);
  };

  syncGeometry();
  animationFrame = window.requestAnimationFrame(render);

  return {
    dispose() {
      if (disposed) return;
      disposed = true;
      window.cancelAnimationFrame(animationFrame);
      document.removeEventListener("visibilitychange", handleVisibilityChange);
      renderer.domElement.removeEventListener("webglcontextlost", handleContextLost);
      resizeObserver?.disconnect();
      intersectionObserver?.disconnect();
      if (autoClickInterval !== undefined) window.clearInterval(autoClickInterval);
      inspection?.uninstall();
      scene.remove(plane);
      compiledScene.dispose();
      waveSimulation.dispose();
      geometry.dispose();
      material.dispose();
      renderer.dispose();
      renderer.domElement.remove();
    },
  };
}

function noopRenderer(): DotMatrixRenderer {
  return { dispose() {} };
}

function styleCanvas(canvas: HTMLCanvasElement): void {
  canvas.style.position = "absolute";
  canvas.style.inset = "0";
  canvas.style.width = "100%";
  canvas.style.height = "100%";
  canvas.style.pointerEvents = "none";
}

function currentViewport(host: HTMLElement): DotMatrixViewport {
  const box = host.getBoundingClientRect();
  return {
    dpr: window.devicePixelRatio || 1,
    h: Math.max(box.height, 1),
    w: Math.max(box.width, 1),
  };
}

function createSceneResizeObserver(
  host: HTMLElement,
  onResize: () => void,
): ResizeObserver | undefined {
  if (typeof ResizeObserver !== "function") return undefined;

  const observer = new ResizeObserver(onResize);
  observer.observe(host);
  document
    .querySelectorAll<HTMLElement>(DOT_SHADOW_MASK_SELECTOR)
    .forEach((element) => observer.observe(element));
  document
    .querySelectorAll<HTMLElement>(DOT_MATRIX_WAVE_FOCUS_SELECTOR)
    .forEach((element) => observer.observe(element));
  return observer;
}

function startAutoClicks(search: URLSearchParams): number | undefined {
  const autoClickRate = resolveAutoClickRate(search);
  if (autoClickRate === undefined) return undefined;

  let autoClickIndex = 0;
  return window.setInterval(() => {
    const band = autoClickIndex % 3;
    autoClickIndex += 1;
    recordSyntheticDotMatrixClick({
      timeStamp: performance.now(),
      x: Math.random(),
      y: (band + Math.random()) / 3,
    });
  }, 1000 / autoClickRate);
}

function cameraRectVector(rect: DomainRect): Vector4 {
  return new Vector4(rect.x, rect.y, rect.width, rect.height);
}

function resolveWaveFocusPoint(host: HTMLElement, viewport: DotMatrixViewport): Vec2 {
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

function resolveFallbackWaveFocusPoint(_viewport: DotMatrixViewport): Vec2 {
  return { x: 0.5, y: 0.62 };
}

function rendererGeometryKey(viewport: DotMatrixViewport, dpr: number): string {
  return `${viewport.w}:${viewport.h}:${dpr}`;
}

function resolveRendererDpr(dpr: number): number {
  return Math.min(Math.max(dpr, 1), DOT_MATRIX_RENDER_DPR_CAP);
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

function emitCapability(
  rendererBackend: DotMatrixRendererBackend,
  reducedMotion: boolean,
  host?: HTMLElement,
): void {
  const viewport = host === undefined ? undefined : currentViewport(host);
  emitSpan("verself.landing_dot_matrix.capability", {
    renderer_backend: rendererBackend,
    prefers_reduced_motion: String(reducedMotion),
    device_pixel_ratio: String(window.devicePixelRatio || 1),
    "viewport.w": String(viewport?.w ?? window.innerWidth),
    "viewport.h": String(viewport?.h ?? window.innerHeight),
  });
}

function emitDegraded(reason: DotMatrixDegradedReason, error?: unknown): void {
  emitSpan("verself.landing_dot_matrix.degraded", {
    reason,
    ...errorAttrs(error),
  });
}

function errorAttrs(error: unknown): Record<string, string> {
  if (!(error instanceof Error)) {
    return {};
  }
  return {
    "error.message": error.message.slice(0, 256),
    "error.name": error.name,
  };
}

function clamp01(value: number): number {
  return Math.min(1, Math.max(0, value));
}
