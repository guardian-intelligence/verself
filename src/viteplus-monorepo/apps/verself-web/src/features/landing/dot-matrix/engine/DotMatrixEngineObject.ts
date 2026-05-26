import type { RefObject } from "react";
import {
  GLSL3,
  Mesh,
  Object3D,
  PlaneGeometry,
  ShaderMaterial,
  type Camera,
  type IUniform,
  type Texture,
  Vector2,
  Vector4,
  type WebGLRenderer,
} from "three";

import { drainDotMatrixClicks } from "../interaction";
import { createCompiledWaveScene } from "../scene/compile";
import { collectShadowRegions } from "../scene/dom-regions";
import { createLandingWaveScene } from "../scene/model";
import type { CompiledWaveScene, DomainRect, Vec2 } from "../scene/types";
import {
  dotMatrixFragmentShader,
  dotMatrixShaderSourceHash,
  dotMatrixVertexShader,
} from "../shader/dot-matrix.generated";
import { dotMatrixWaveShaderSourceHash } from "../shader/wave-simulation.generated";
import { sampleDotMatrixTimeline } from "../timeline";
import { createAmbientWaveScheduler } from "../wave/ambient";
import { createFixedTimestepLoop } from "../wave/fixed-timestep";
import type { FixedTimestepAdvance } from "../wave/fixed-timestep";
import { createClickWaveImpulse, createWaveImpulseQueue } from "../wave/impulses";
import { createDotMatrixWaveSimulation } from "../wave/simulation";
import { DOT_MATRIX_MAX_WAVE_IMPULSES } from "../wave/types";
import { DOT_MATRIX_DOT_SPACING_CSS_PX, DOT_MATRIX_FRAME_BUDGET } from "./frame-budget";
import { DOT_MATRIX_LIGHTING } from "./lighting";
import {
  createDotMatrixTelemetry,
  type DotMatrixDegradedReason,
  type DotMatrixR3FPerformanceTelemetry,
  type DotMatrixTelemetry,
} from "./telemetry";

interface DotMatrixEngineOptions {
  readonly camera: Camera;
  readonly hostRef: RefObject<HTMLElement | null>;
  readonly renderer: WebGLRenderer;
  readonly stageRef: RefObject<HTMLElement | null>;
}

export interface DotMatrixFrameInput {
  readonly deltaSeconds: number;
  readonly nowSeconds: number;
  readonly nowMs: number;
  readonly r3fPerformance: DotMatrixR3FPerformanceTelemetry;
}

interface DotMatrixViewport {
  readonly dpr: number;
  readonly h: number;
  readonly w: number;
}

interface ShadowUniforms {
  readonly falloff: readonly Vector4[];
  readonly params: readonly Vector4[];
  readonly rects: readonly Vector4[];
}

interface DotMatrixUniforms extends Record<string, IUniform> {
  readonly uActive: { value: number };
  readonly uAmbient: { value: number };
  readonly uCameraRect: { value: Vector4 };
  readonly uContrast: { value: number };
  readonly uDotSpacingPx: { value: number };
  readonly uDpr: { value: number };
  readonly uFieldState: { value: Texture };
  readonly uKeyLightColor: { value: typeof DOT_MATRIX_LIGHTING.keyColor };
  readonly uKeyLightDirection: { value: typeof DOT_MATRIX_LIGHTING.keyDirection };
  readonly uLightAmbient: { value: number };
  readonly uPhase: { value: number };
  readonly uPostExposure: { value: number };
  readonly uResolution: { value: Vector2 };
  readonly uRimLightColor: { value: typeof DOT_MATRIX_LIGHTING.rimColor };
  readonly uRimLightDirection: { value: typeof DOT_MATRIX_LIGHTING.rimDirection };
  readonly uSeed: { value: number };
  readonly uShadowMaskCount: { value: number };
  readonly uShadowMaskFalloff: { value: readonly Vector4[] };
  readonly uShadowMaskParams: { value: readonly Vector4[] };
  readonly uShadowMaskRects: { value: readonly Vector4[] };
  readonly uSurfaceDepth: { value: number };
  readonly uWaveState: { value: Texture };
}

interface ReadyEngineResources {
  readonly ambientWaves: ReturnType<typeof createAmbientWaveScheduler>;
  readonly compiledScene: CompiledWaveScene;
  readonly fixedTimestep: ReturnType<typeof createFixedTimestepLoop>;
  readonly geometry: PlaneGeometry;
  readonly impulseQueue: ReturnType<typeof createWaveImpulseQueue>;
  readonly material: ShaderMaterial;
  readonly mesh: Mesh;
  readonly shadowUniforms: ShadowUniforms;
  readonly uniforms: DotMatrixUniforms;
  readonly waveSimulation: ReturnType<typeof createDotMatrixWaveSimulation>;
}

type DotMatrixEngineResources =
  | {
      readonly kind: "degraded";
    }
  | {
      readonly kind: "ready";
      readonly value: ReadyEngineResources;
    };

const DOT_MATRIX_MAX_SHADOW_MASKS = 32;
const DOT_MATRIX_DOM_SYNC_INTERVAL_SECONDS = 0.3;
const DOT_MATRIX_WAVE_FOCUS_SELECTOR = "[data-wave-focus]";

export class DotMatrixEngineObject extends Object3D {
  private readonly camera: Camera;
  private readonly hostRef: RefObject<HTMLElement | null>;
  private readonly renderer: WebGLRenderer;
  private readonly stageRef: RefObject<HTMLElement | null>;
  private readonly resources: DotMatrixEngineResources;
  private readonly telemetry: DotMatrixTelemetry;
  private degraded = false;
  private disposed = false;
  private firstFrameSent = false;
  private lastGeometryKey = "";
  private lastMaskKey = "";
  private lastSceneKey = "";
  private nextDomSyncSeconds = 0;
  private simulationSeconds = 0;
  private timelineSeconds = 0;
  private waveFocusPoint: Vec2 = { x: 0.5, y: 0.62 };

  constructor(options: DotMatrixEngineOptions) {
    super();

    this.camera = options.camera;
    this.hostRef = options.hostRef;
    this.renderer = options.renderer;
    this.stageRef = options.stageRef;

    const startedAtMs = performance.now();
    const shaderHash = `${dotMatrixShaderSourceHash}:${dotMatrixWaveShaderSourceHash}`;
    const host = this.hostRef.current ?? undefined;
    if (options.renderer.extensions.get("EXT_color_buffer_float") === null) {
      this.telemetry = createDotMatrixTelemetry({
        rendererBackend: "webgl2_no_float_render_target",
        shaderHash,
        startedAtMs,
        viewport:
          host === undefined ? currentWindowViewport() : currentViewport(host, this.renderer),
      });
      this.resources = { kind: "degraded" };
      this.telemetry.markDegraded({
        nowMs: performance.now(),
        reason: "float_render_target_unsupported",
        viewport:
          host === undefined ? currentWindowViewport() : currentViewport(host, this.renderer),
        waveGrid: { height: 0, width: 0 },
      });
      return;
    }

    this.telemetry = createDotMatrixTelemetry({
      rendererBackend: "webgl2",
      shaderHash,
      startedAtMs,
      viewport: host === undefined ? currentWindowViewport() : currentViewport(host, this.renderer),
    });

    try {
      this.resources = {
        kind: "ready",
        value: createReadyResources({
          camera: this.camera,
          renderer: this.renderer,
        }),
      };
      this.add(this.resources.value.mesh);
    } catch (error) {
      this.resources = { kind: "degraded" };
      this.telemetry.markDegraded({
        error,
        nowMs: performance.now(),
        reason: "resource_init_failed",
        viewport:
          host === undefined ? currentWindowViewport() : currentViewport(host, this.renderer),
        waveGrid: { height: 0, width: 0 },
      });
    }
  }

  frame(input: DotMatrixFrameInput): void {
    if (this.disposed || this.degraded || this.resources.kind !== "ready") return;

    const host = this.hostRef.current;
    if (host === null) return;

    const resources = this.resources.value;
    const frameDeltaSeconds = Math.min(Math.max(input.deltaSeconds, 0), 0.1);
    const frameActive = document.visibilityState === "visible" && hostIntersectsViewport(host);
    const viewport = this.syncGeometry(resources, host, input.nowSeconds);

    if (frameActive) {
      this.timelineSeconds += frameDeltaSeconds;
    }
    resources.uniforms.uActive.value = frameActive ? 1 : 0;

    const timeline = sampleDotMatrixTimeline(this.timelineSeconds);
    resources.uniforms.uAmbient.value = timeline.ambient;
    resources.uniforms.uContrast.value = timeline.contrast;
    resources.uniforms.uPhase.value = timeline.phase;

    const clickImpulses = drainDotMatrixClicks().map((click) =>
      createClickWaveImpulse(click, resources.compiledScene.cameraRect),
    );
    const ambientImpulses = frameActive
      ? resources.ambientWaves.collect({
          ambient: timeline.ambient,
          cameraRect: resources.compiledScene.cameraRect,
          fields: resources.compiledScene.fields,
          focusPoint: this.waveFocusPoint,
          ignition: timeline.ignition,
          nowSeconds: this.timelineSeconds,
          viewport,
        })
      : [];

    resources.impulseQueue.pushMany(clickImpulses);
    resources.impulseQueue.pushMany(ambientImpulses);

    const fixedFrame = resources.fixedTimestep.advance(
      frameDeltaSeconds * DOT_MATRIX_FRAME_BUDGET.travelSpeedScale,
      frameActive,
    );
    this.stepSimulation(resources, fixedFrame, frameActive, timeline.ambient);
    this.telemetry.recordFrame({
      active: frameActive,
      ambientImpulses: ambientImpulses.length,
      clickImpulses: clickImpulses.length,
      deltaSeconds: frameDeltaSeconds,
      droppedSeconds: fixedFrame.droppedSeconds,
      fixedSteps: fixedFrame.steps,
      nowMs: input.nowMs,
      queuedImpulses: resources.impulseQueue.size(),
      r3fPerformance: input.r3fPerformance,
      renderer: this.renderer,
      viewport,
      waveGrid: resources.waveSimulation.size(),
    });

    if (!this.firstFrameSent) {
      this.firstFrameSent = true;
      const stage = this.stageRef.current;
      if (stage !== null) {
        stage.dataset.dotMatrixLive = "";
        stage.style.opacity = "1";
      }
      this.telemetry.markReady({
        nowMs: input.nowMs,
        viewport,
        waveGrid: resources.waveSimulation.size(),
      });
    }
  }

  dispose(): void {
    if (this.disposed) return;
    this.disposed = true;
    this.telemetry.dispose();

    if (this.resources.kind === "ready") {
      const resources = this.resources.value;
      this.remove(resources.mesh);
      resources.compiledScene.dispose();
      resources.waveSimulation.dispose();
      resources.geometry.dispose();
      resources.material.dispose();
    }
  }

  private markDegraded(reason: DotMatrixDegradedReason, error?: unknown): void {
    if (this.degraded) return;
    this.degraded = true;
    const stage = this.stageRef.current;
    if (stage !== null) stage.style.opacity = "0";
    const host = this.hostRef.current;
    this.telemetry.markDegraded({
      error,
      nowMs: performance.now(),
      reason,
      viewport: host === null ? currentWindowViewport() : currentViewport(host, this.renderer),
      waveGrid:
        this.resources.kind === "ready"
          ? this.resources.value.waveSimulation.size()
          : { height: 0, width: 0 },
    });
  }

  private stepSimulation(
    resources: ReadyEngineResources,
    fixedFrame: FixedTimestepAdvance,
    active: boolean,
    ambient: number,
  ): void {
    try {
      for (let stepIndex = 0; stepIndex < fixedFrame.steps; stepIndex += 1) {
        resources.uniforms.uWaveState.value = resources.waveSimulation.step({
          active,
          ambient,
          deltaSeconds: fixedFrame.stepSeconds,
          impulses: resources.impulseQueue.drain(DOT_MATRIX_MAX_WAVE_IMPULSES),
        });
        this.simulationSeconds += fixedFrame.stepSeconds;
      }
    } catch (error) {
      this.markDegraded("compile_error", error);
    }
  }

  private syncGeometry(
    resources: ReadyEngineResources,
    host: HTMLElement,
    nowSeconds: number,
  ): DotMatrixViewport {
    const viewport = currentViewport(host, this.renderer);
    const geometryKey = rendererGeometryKey(viewport);

    if (geometryKey !== this.lastGeometryKey) {
      resources.uniforms.uResolution.value.set(
        Math.max(1, viewport.w * viewport.dpr),
        Math.max(1, viewport.h * viewport.dpr),
      );
      resources.uniforms.uDpr.value = viewport.dpr;
      resources.waveSimulation.resize(viewport.w, viewport.h);
      this.lastGeometryKey = geometryKey;
    }

    const sceneKey = geometryKey;
    if (sceneKey !== this.lastSceneKey) {
      resources.compiledScene.update(createLandingWaveScene(viewport));
      resources.uniforms.uCameraRect.value.copy(
        cameraRectVector(resources.compiledScene.cameraRect),
      );
      resources.uniforms.uFieldState.value = resources.compiledScene.texture;
      this.lastSceneKey = sceneKey;
    }

    const shouldSyncDom = geometryKey !== this.lastMaskKey || nowSeconds >= this.nextDomSyncSeconds;
    if (shouldSyncDom) {
      this.syncShadowRegions(resources, host, viewport.dpr);
      this.waveFocusPoint = resolveWaveFocusPoint(host);
      this.lastMaskKey = geometryKey;
      this.nextDomSyncSeconds = nowSeconds + DOT_MATRIX_DOM_SYNC_INTERVAL_SECONDS;
    }

    return viewport;
  }

  private syncShadowRegions(resources: ReadyEngineResources, host: HTMLElement, dpr: number): void {
    const regions = collectShadowRegions({ dpr, host });
    const regionCount = Math.min(regions.length, DOT_MATRIX_MAX_SHADOW_MASKS);
    resources.uniforms.uShadowMaskCount.value = regionCount;

    for (let index = 0; index < DOT_MATRIX_MAX_SHADOW_MASKS; index += 1) {
      const rectUniform = resources.shadowUniforms.rects[index];
      const paramUniform = resources.shadowUniforms.params[index];
      const falloffUniform = resources.shadowUniforms.falloff[index];
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
  }
}

function createReadyResources(input: {
  readonly camera: Camera;
  readonly renderer: WebGLRenderer;
}): ReadyEngineResources {
  const geometry = new PlaneGeometry(2, 2);
  const compiledScene = createCompiledWaveScene(createLandingWaveScene(currentWindowViewport()), {
    height: 192,
    width: 192,
  });
  const waveSimulation = createDotMatrixWaveSimulation(input.renderer, input.camera, geometry);
  const shadowUniforms = createShadowUniforms();
  const uniforms = createDisplayUniforms(compiledScene, shadowUniforms, waveSimulation.texture());
  const material = new ShaderMaterial({
    depthTest: false,
    depthWrite: false,
    fragmentShader: dotMatrixFragmentShader,
    glslVersion: GLSL3,
    transparent: true,
    uniforms,
    vertexShader: dotMatrixVertexShader,
  });
  const mesh = new Mesh(geometry, material);
  mesh.frustumCulled = false;

  return {
    ambientWaves: createAmbientWaveScheduler({ seed: createSessionSeed() }),
    compiledScene,
    fixedTimestep: createFixedTimestepLoop({
      maxFrameSeconds: DOT_MATRIX_FRAME_BUDGET.maxFrameSeconds,
      maxSubSteps: DOT_MATRIX_FRAME_BUDGET.maxSubSteps,
      stepSeconds: DOT_MATRIX_FRAME_BUDGET.fixedStepSeconds,
    }),
    geometry,
    impulseQueue: createWaveImpulseQueue(36),
    material,
    mesh,
    shadowUniforms,
    uniforms,
    waveSimulation,
  };
}

function createDisplayUniforms(
  compiledScene: CompiledWaveScene,
  shadowUniforms: ShadowUniforms,
  waveTexture: Texture,
): DotMatrixUniforms {
  return {
    uActive: { value: 1 },
    uAmbient: { value: 0 },
    uCameraRect: { value: cameraRectVector(compiledScene.cameraRect) },
    uContrast: { value: 1 },
    uDotSpacingPx: { value: DOT_MATRIX_DOT_SPACING_CSS_PX },
    uDpr: { value: 1 },
    uFieldState: { value: compiledScene.texture },
    uKeyLightColor: { value: DOT_MATRIX_LIGHTING.keyColor },
    uKeyLightDirection: { value: DOT_MATRIX_LIGHTING.keyDirection },
    uLightAmbient: { value: DOT_MATRIX_LIGHTING.ambient },
    uPhase: { value: 0 },
    uPostExposure: { value: DOT_MATRIX_LIGHTING.exposure },
    uResolution: { value: new Vector2() },
    uRimLightColor: { value: DOT_MATRIX_LIGHTING.rimColor },
    uRimLightDirection: { value: DOT_MATRIX_LIGHTING.rimDirection },
    uSeed: { value: 0.38196601125 },
    uShadowMaskCount: { value: 0 },
    uShadowMaskFalloff: { value: shadowUniforms.falloff },
    uShadowMaskParams: { value: shadowUniforms.params },
    uShadowMaskRects: { value: shadowUniforms.rects },
    uSurfaceDepth: { value: DOT_MATRIX_LIGHTING.surfaceDepth },
    uWaveState: { value: waveTexture },
  };
}

function createShadowUniforms(): ShadowUniforms {
  return {
    falloff: Array.from({ length: DOT_MATRIX_MAX_SHADOW_MASKS }, () => new Vector4()),
    params: Array.from({ length: DOT_MATRIX_MAX_SHADOW_MASKS }, () => new Vector4()),
    rects: Array.from({ length: DOT_MATRIX_MAX_SHADOW_MASKS }, () => new Vector4()),
  };
}

function currentViewport(host: HTMLElement, renderer: WebGLRenderer): DotMatrixViewport {
  const box = host.getBoundingClientRect();
  return {
    dpr: Math.max(renderer.getPixelRatio(), 1),
    h: Math.max(box.height, 1),
    w: Math.max(box.width, 1),
  };
}

function currentWindowViewport(): DotMatrixViewport {
  return {
    dpr: Math.max(window.devicePixelRatio || 1, 1),
    h: Math.max(window.innerHeight, 1),
    w: Math.max(window.innerWidth, 1),
  };
}

function hostIntersectsViewport(host: HTMLElement): boolean {
  const box = host.getBoundingClientRect();
  return (
    box.width > 0 &&
    box.height > 0 &&
    box.bottom >= 0 &&
    box.right >= 0 &&
    box.top <= window.innerHeight &&
    box.left <= window.innerWidth
  );
}

function cameraRectVector(rect: DomainRect): Vector4 {
  return new Vector4(rect.x, rect.y, rect.width, rect.height);
}

function resolveWaveFocusPoint(host: HTMLElement): Vec2 {
  const focusElement = document.querySelector<HTMLElement>(DOT_MATRIX_WAVE_FOCUS_SELECTOR);
  if (focusElement === null) {
    return resolveFallbackWaveFocusPoint();
  }

  const hostBox = host.getBoundingClientRect();
  const focusBox = focusElement.getBoundingClientRect();
  if (hostBox.width <= 0 || hostBox.height <= 0 || focusBox.width <= 0 || focusBox.height <= 0) {
    return resolveFallbackWaveFocusPoint();
  }

  return {
    x: clamp01((focusBox.left + focusBox.width * 0.5 - hostBox.left) / hostBox.width),
    y: clamp01(1 - (focusBox.top + focusBox.height * 0.5 - hostBox.top) / hostBox.height),
  };
}

function resolveFallbackWaveFocusPoint(): Vec2 {
  return { x: 0.5, y: 0.62 };
}

function rendererGeometryKey(viewport: DotMatrixViewport): string {
  return `${viewport.w}:${viewport.h}:${viewport.dpr}`;
}

function createSessionSeed(): number {
  const values = new Uint32Array(1);
  if (window.crypto !== undefined) {
    window.crypto.getRandomValues(values);
  }
  const seed = values[0] ?? 0;
  return seed === 0 ? Math.floor(Math.random() * 0xffffffff) : seed;
}

function clamp01(value: number): number {
  return Math.min(1, Math.max(0, value));
}
