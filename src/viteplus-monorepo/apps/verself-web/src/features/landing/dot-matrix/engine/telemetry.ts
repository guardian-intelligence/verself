import type { WebGLRenderer } from "three";

import { emitSpan, initBrowserTelemetry } from "../../../../lib/telemetry/browser";
import { isDevelopmentModeEnabled } from "../../../../lib/development-mode";

export interface DotMatrixViewportTelemetry {
  readonly dpr: number;
  readonly h: number;
  readonly w: number;
}

export interface DotMatrixWaveGridTelemetry {
  readonly height: number;
  readonly width: number;
}

export interface DotMatrixRendererTelemetry {
  readonly calls: number;
  readonly triangles: number;
}

export interface DotMatrixR3FPerformanceTelemetry {
  readonly current: number;
  readonly max: number;
  readonly min: number;
}

export interface DotMatrixFrameTelemetryInput {
  readonly active: boolean;
  readonly ambientImpulses: number;
  readonly clickImpulses: number;
  readonly deltaSeconds: number;
  readonly droppedSeconds: number;
  readonly fixedSteps: number;
  readonly nowMs: number;
  readonly queuedImpulses: number;
  readonly r3fPerformance: DotMatrixR3FPerformanceTelemetry;
  readonly renderer: WebGLRenderer;
  readonly viewport: DotMatrixViewportTelemetry;
  readonly waveGrid: DotMatrixWaveGridTelemetry;
}

export type DotMatrixLoadStatus = "ready" | "degraded" | "skipped";
export type DotMatrixSkippedReason = "prefers_reduced_motion";
export type DotMatrixDegradedReason =
  | "compile_error"
  | "float_render_target_unsupported"
  | "resource_init_failed";

interface DotMatrixTelemetryOptions {
  readonly rendererBackend: "none" | "webgl2" | "webgl2_no_float_render_target";
  readonly sampleRate: number;
  readonly shaderHash: string;
  readonly startedAtMs: number;
  readonly viewport: DotMatrixViewportTelemetry;
}

interface FrameWindow {
  activeMs: number;
  ambientImpulses: number;
  clickImpulses: number;
  droppedSeconds: number;
  fixedSteps: number;
  frameCount: number;
  frameMs: Float64Array;
  maxFixedSteps: number;
  queuedImpulsesMax: number;
  wallMs: number;
}

export interface DotMatrixFrameWindowSummary {
  readonly activeRatio: number;
  readonly ambientImpulses: number;
  readonly budget16Misses: number;
  readonly budget33Misses: number;
  readonly clickImpulses: number;
  readonly droppedSeconds: number;
  readonly fixedSteps: number;
  readonly fpsMean: number;
  readonly frameCount: number;
  readonly frameMsMax: number;
  readonly frameMsMean: number;
  readonly frameMsP50: number;
  readonly frameMsP95: number;
  readonly frameMsP99: number;
  readonly longFrames50: number;
  readonly maxFixedSteps: number;
  readonly queuedImpulsesMax: number;
}

interface DotMatrixPerfSnapshot {
  readonly active: boolean;
  readonly fps: number;
  readonly frameMs: number;
  readonly frameMsP95: number;
  readonly frames: number;
  readonly lastLoadStatus: DotMatrixLoadStatus;
  readonly r3fPerformanceCurrent: number;
  readonly rendererCalls: number;
  readonly rendererTriangles: number;
  readonly steps: number;
  readonly updatedAtMs: number;
  readonly waveGrid: string;
}

type DotMatrixPerfPanel = {
  readonly ownerId: number;
  readonly snapshot: DotMatrixPerfSnapshot;
  readonly element: HTMLDivElement;
};

interface DotMatrixDevTools {
  readonly dispose: () => void;
  readonly recordFrame: (
    input: DotMatrixFrameTelemetryInput,
    summarize: () => DotMatrixFrameWindowSummary,
  ) => void;
  readonly recordLoad: (status: DotMatrixLoadStatus) => void;
}

const DOT_MATRIX_PRODUCTION_SAMPLE_RATE = 0.05;
const DOT_MATRIX_FRAME_WINDOW_MS = 10_000;
const DOT_MATRIX_MIN_PARTIAL_WINDOW_MS = 2_000;
const DOT_MATRIX_MAX_WINDOWS = 3;
const DOT_MATRIX_MAX_FRAMES_PER_WINDOW = 3_000;
const DOT_MATRIX_DEV_TOOLS_REFRESH_MS = 250;
const CORRELATION_COOKIE_NAME = "verself_correlation_id";

declare global {
  interface Window {
    __verselfDotMatrixPerf?: {
      snapshot: () => DotMatrixPerfSnapshot;
    };
    __verselfDotMatrixPerfPanel?: DotMatrixPerfPanel;
  }
}

export function createDotMatrixTelemetry(
  options: Omit<DotMatrixTelemetryOptions, "sampleRate"> & {
    readonly sampleRate?: number;
  },
): DotMatrixTelemetry {
  return new DotMatrixTelemetry({
    ...options,
    sampleRate: options.sampleRate ?? DOT_MATRIX_PRODUCTION_SAMPLE_RATE,
  });
}

export function emitDotMatrixSkippedLoad(
  reason: DotMatrixSkippedReason,
  viewport: DotMatrixViewportTelemetry,
): void {
  initBrowserTelemetry();
  emitLoadSpan({
    attrs: {},
    rendererBackend: "none",
    reason,
    perfSampleRate: DOT_MATRIX_PRODUCTION_SAMPLE_RATE,
    shaderHash: "",
    startedAtMs: performance.now(),
    status: "skipped",
    timeToFirstFrameMs: 0,
    viewport,
    waveGrid: { height: 0, width: 0 },
  });
}

export class DotMatrixTelemetry {
  private readonly devTools: DotMatrixDevTools;
  private readonly rendererBackend: DotMatrixTelemetryOptions["rendererBackend"];
  private readonly sampleRate: number;
  private readonly sampled: boolean;
  private readonly shaderHash: string;
  private readonly startedAtMs: number;
  private readonly visibilityHandler: () => void;
  private currentWindow = createFrameWindow();
  private disposed = false;
  private lastFrameInput: DotMatrixFrameTelemetryInput | undefined;
  private loadSent = false;
  private windowsEmitted = 0;

  constructor(options: DotMatrixTelemetryOptions) {
    initBrowserTelemetry();
    this.rendererBackend = options.rendererBackend;
    this.sampleRate = clampRate(options.sampleRate);
    this.sampled = import.meta.env.DEV || shouldSampleSession(this.sampleRate);
    this.shaderHash = options.shaderHash;
    this.startedAtMs = options.startedAtMs;
    this.devTools = createDotMatrixDevTools(options.viewport);
    this.visibilityHandler = () => {
      if (document.visibilityState === "hidden") {
        this.flushWindow("visibility_hidden");
      }
    };
    document.addEventListener("visibilitychange", this.visibilityHandler);
  }

  markReady(input: {
    readonly nowMs: number;
    readonly viewport: DotMatrixViewportTelemetry;
    readonly waveGrid: DotMatrixWaveGridTelemetry;
  }): void {
    if (this.loadSent) return;
    this.loadSent = true;
    this.devTools.recordLoad("ready");
    emitLoadSpan({
      attrs: {},
      rendererBackend: this.rendererBackend,
      reason: "",
      perfSampleRate: this.sampleRate,
      shaderHash: this.shaderHash,
      startedAtMs: this.startedAtMs,
      status: "ready",
      timeToFirstFrameMs: input.nowMs - this.startedAtMs,
      viewport: input.viewport,
      waveGrid: input.waveGrid,
    });
  }

  markDegraded(input: {
    readonly error?: unknown;
    readonly nowMs: number;
    readonly reason: DotMatrixDegradedReason;
    readonly viewport: DotMatrixViewportTelemetry;
    readonly waveGrid: DotMatrixWaveGridTelemetry;
  }): void {
    const attrs = errorAttrs(input.error);
    if (this.loadSent) {
      this.devTools.recordLoad("degraded");
      emitSpan("verself.landing_dot_matrix.runtime_degraded", {
        ...attrs,
        reason: input.reason,
        "route.path": currentRoutePath(),
        "shader.hash": this.shaderHash,
      });
      return;
    }

    this.loadSent = true;
    this.devTools.recordLoad("degraded");
    emitLoadSpan({
      attrs,
      rendererBackend: this.rendererBackend,
      reason: input.reason,
      perfSampleRate: this.sampleRate,
      shaderHash: this.shaderHash,
      startedAtMs: this.startedAtMs,
      status: "degraded",
      timeToFirstFrameMs: input.nowMs - this.startedAtMs,
      viewport: input.viewport,
      waveGrid: input.waveGrid,
    });
  }

  recordFrame(input: DotMatrixFrameTelemetryInput): void {
    if (this.disposed) return;
    this.lastFrameInput = input;

    if (this.sampled && this.windowsEmitted < DOT_MATRIX_MAX_WINDOWS) {
      recordFrame(this.currentWindow, input);
    }
    this.devTools.recordFrame(input, () => summarizeFrameWindow(this.currentWindow));
    if (!this.sampled || this.windowsEmitted >= DOT_MATRIX_MAX_WINDOWS) return;

    if (this.currentWindow.activeMs >= DOT_MATRIX_FRAME_WINDOW_MS) {
      this.emitPerfSample("window_complete", input);
    }
  }

  dispose(): void {
    if (this.disposed) return;
    this.disposed = true;
    this.flushWindow("dispose");
    document.removeEventListener("visibilitychange", this.visibilityHandler);
    this.devTools.dispose();
  }

  private flushWindow(reason: string): void {
    if (
      !this.sampled ||
      this.windowsEmitted >= DOT_MATRIX_MAX_WINDOWS ||
      this.currentWindow.activeMs < DOT_MATRIX_MIN_PARTIAL_WINDOW_MS
    ) {
      return;
    }
    this.emitPerfSample(reason, this.lastFrameInput);
  }

  private emitPerfSample(reason: string, input: DotMatrixFrameTelemetryInput | undefined): void {
    const summary = summarizeFrameWindow(this.currentWindow);
    if (summary.frameCount === 0) return;
    this.windowsEmitted += 1;
    emitSpan("verself.landing_dot_matrix.perf_sample", {
      active_ratio: formatRatio(summary.activeRatio),
      ambient_impulses: String(summary.ambientImpulses),
      budget_16_7_miss_count: String(summary.budget16Misses),
      budget_33_3_miss_count: String(summary.budget33Misses),
      click_impulses: String(summary.clickImpulses),
      dropped_seconds: formatSeconds(summary.droppedSeconds),
      fixed_steps_max_per_frame: String(summary.maxFixedSteps),
      fixed_steps_total: String(summary.fixedSteps),
      fps_mean: formatNumber(summary.fpsMean),
      frame_count: String(summary.frameCount),
      frame_ms_max: formatNumber(summary.frameMsMax),
      frame_ms_mean: formatNumber(summary.frameMsMean),
      frame_ms_p50: formatNumber(summary.frameMsP50),
      frame_ms_p95: formatNumber(summary.frameMsP95),
      frame_ms_p99: formatNumber(summary.frameMsP99),
      long_frame_50_count: String(summary.longFrames50),
      queued_impulses_max: String(summary.queuedImpulsesMax),
      reason,
      renderer_backend: this.rendererBackend,
      renderer_calls: String(input?.renderer.info.render.calls ?? 0),
      renderer_triangles: String(input?.renderer.info.render.triangles ?? 0),
      "route.path": currentRoutePath(),
      r3f_performance_current: formatNumber(input?.r3fPerformance.current ?? 0),
      r3f_performance_max: formatNumber(input?.r3fPerformance.max ?? 0),
      r3f_performance_min: formatNumber(input?.r3fPerformance.min ?? 0),
      sample_rate: formatRatio(this.sampleRate),
      "shader.hash": this.shaderHash,
      viewport_class: viewportClass(input?.viewport ?? { dpr: 1, h: 0, w: 0 }),
      wave_grid: waveGridLabel(input?.waveGrid ?? { height: 0, width: 0 }),
      window_ms: String(Math.round(this.currentWindow.activeMs)),
    });
    this.currentWindow = createFrameWindow();
  }
}

export function summarizeFrameDeltas(
  frameMs: readonly number[],
  activeMs: number,
  wallMs: number,
): Pick<
  DotMatrixFrameWindowSummary,
  | "activeRatio"
  | "budget16Misses"
  | "budget33Misses"
  | "fpsMean"
  | "frameCount"
  | "frameMsMax"
  | "frameMsMean"
  | "frameMsP50"
  | "frameMsP95"
  | "frameMsP99"
  | "longFrames50"
> {
  if (frameMs.length === 0 || activeMs <= 0) {
    return {
      activeRatio: 0,
      budget16Misses: 0,
      budget33Misses: 0,
      fpsMean: 0,
      frameCount: 0,
      frameMsMax: 0,
      frameMsMean: 0,
      frameMsP50: 0,
      frameMsP95: 0,
      frameMsP99: 0,
      longFrames50: 0,
    };
  }

  const sorted = [...frameMs].sort((a, b) => a - b);
  const sum = sorted.reduce((acc, value) => acc + value, 0);
  return {
    activeRatio: wallMs <= 0 ? 1 : activeMs / wallMs,
    budget16Misses: sorted.filter((value) => value > 16.7).length,
    budget33Misses: sorted.filter((value) => value > 33.3).length,
    fpsMean: (frameMs.length / activeMs) * 1000,
    frameCount: frameMs.length,
    frameMsMax: sorted[sorted.length - 1] ?? 0,
    frameMsMean: sum / sorted.length,
    frameMsP50: quantile(sorted, 0.5),
    frameMsP95: quantile(sorted, 0.95),
    frameMsP99: quantile(sorted, 0.99),
    longFrames50: sorted.filter((value) => value > 50).length,
  };
}

function createFrameWindow(): FrameWindow {
  return {
    activeMs: 0,
    ambientImpulses: 0,
    clickImpulses: 0,
    droppedSeconds: 0,
    fixedSteps: 0,
    frameCount: 0,
    frameMs: new Float64Array(DOT_MATRIX_MAX_FRAMES_PER_WINDOW),
    maxFixedSteps: 0,
    queuedImpulsesMax: 0,
    wallMs: 0,
  };
}

function recordFrame(window: FrameWindow, input: DotMatrixFrameTelemetryInput): void {
  const deltaMs = Math.max(input.deltaSeconds, 0) * 1000;
  window.wallMs += deltaMs;
  if (!input.active) return;

  window.activeMs += deltaMs;
  window.ambientImpulses += input.ambientImpulses;
  window.clickImpulses += input.clickImpulses;
  window.droppedSeconds += input.droppedSeconds;
  window.fixedSteps += input.fixedSteps;
  window.maxFixedSteps = Math.max(window.maxFixedSteps, input.fixedSteps);
  window.queuedImpulsesMax = Math.max(window.queuedImpulsesMax, input.queuedImpulses);
  if (window.frameCount < window.frameMs.length) {
    window.frameMs[window.frameCount] = deltaMs;
    window.frameCount += 1;
  }
}

function summarizeFrameWindow(window: FrameWindow): DotMatrixFrameWindowSummary {
  const frameMs = Array.from(window.frameMs.subarray(0, window.frameCount));
  const summary = summarizeFrameDeltas(frameMs, window.activeMs, window.wallMs);
  return {
    ...summary,
    ambientImpulses: window.ambientImpulses,
    clickImpulses: window.clickImpulses,
    droppedSeconds: window.droppedSeconds,
    fixedSteps: window.fixedSteps,
    maxFixedSteps: window.maxFixedSteps,
    queuedImpulsesMax: window.queuedImpulsesMax,
  };
}

function emitLoadSpan(input: {
  readonly attrs: Record<string, string>;
  readonly perfSampleRate: number;
  readonly reason: string;
  readonly rendererBackend: string;
  readonly shaderHash: string;
  readonly startedAtMs: number;
  readonly status: DotMatrixLoadStatus;
  readonly timeToFirstFrameMs: number;
  readonly viewport: DotMatrixViewportTelemetry;
  readonly waveGrid: DotMatrixWaveGridTelemetry;
}): void {
  emitSpan("verself.landing_dot_matrix.load", {
    ...input.attrs,
    device_pixel_ratio_bucket: dprBucket(input.viewport.dpr),
    load_status: input.status,
    load_sample_rate: "1.00",
    perf_sample_rate: formatRatio(input.perfSampleRate),
    reason: input.reason,
    renderer_backend: input.rendererBackend,
    "route.path": currentRoutePath(),
    "shader.hash": input.shaderHash,
    time_to_first_frame_ms: String(Math.max(0, Math.round(input.timeToFirstFrameMs))),
    viewport_class: viewportClass(input.viewport),
    "viewport.h": String(Math.round(input.viewport.h)),
    "viewport.w": String(Math.round(input.viewport.w)),
    wave_grid: waveGridLabel(input.waveGrid),
  });
}

function createDotMatrixDevTools(initialViewport: DotMatrixViewportTelemetry): DotMatrixDevTools {
  if (!import.meta.env.DEV || typeof document === "undefined" || !isDevelopmentModeEnabled()) {
    return noopDevTools();
  }

  let lastRefreshMs = 0;
  let snapshot: DotMatrixPerfSnapshot = {
    active: false,
    fps: 0,
    frameMs: 0,
    frameMsP95: 0,
    frames: 0,
    lastLoadStatus: "skipped",
    r3fPerformanceCurrent: 0,
    rendererCalls: 0,
    rendererTriangles: 0,
    steps: 0,
    updatedAtMs: performance.now(),
    waveGrid: waveGridLabel({ height: 0, width: 0 }),
  };
  const el = document.createElement("div");
  const ownerId = ++DOT_MATRIX_PANEL_OWNER_COUNTER;
  el.dataset.dotMatrixPerf = "";
  Object.assign(el.style, {
    background: "rgba(8, 12, 14, 0.82)",
    border: "1px solid rgba(255, 255, 255, 0.14)",
    borderRadius: "6px",
    bottom: "10px",
    color: "rgb(228, 235, 232)",
    font: "11px/1.35 ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace",
    left: "10px",
    maxWidth: "280px",
    padding: "8px 10px",
    pointerEvents: "none",
    position: "fixed",
    whiteSpace: "pre",
    zIndex: "2147483647",
  });
  document.body.append(el);
  window.__verselfDotMatrixPerfPanel?.element?.remove();
  window.__verselfDotMatrixPerfPanel = {
    ownerId,
    snapshot,
    element: el,
  };

  const api = {
    snapshot: () => snapshot,
  };
  const render = () => {
    el.textContent = [
      "dot-matrix perf",
      `load ${snapshot.lastLoadStatus}`,
      `fps ${formatNumber(snapshot.fps)}  frame ${formatNumber(snapshot.frameMs)}ms  p95 ${formatNumber(snapshot.frameMsP95)}ms`,
      `frames ${snapshot.frames}  active ${snapshot.active ? "yes" : "no"}  steps ${snapshot.steps}`,
      `r3f ${formatNumber(snapshot.r3fPerformanceCurrent)}  calls ${snapshot.rendererCalls}  tris ${snapshot.rendererTriangles}`,
      `grid ${snapshot.waveGrid}  viewport ${viewportClass(initialViewport)}`,
    ].join("\n");
  };
  render();
  window.__verselfDotMatrixPerf = api;

  return {
    dispose() {
      if (window.__verselfDotMatrixPerfPanel?.ownerId === ownerId) {
        window.__verselfDotMatrixPerfPanel.element.remove();
        delete window.__verselfDotMatrixPerfPanel;
      }
      if (window.__verselfDotMatrixPerf === api) {
        delete window.__verselfDotMatrixPerf;
      }
    },
    recordFrame(input, summarize) {
      if (input.nowMs - lastRefreshMs < DOT_MATRIX_DEV_TOOLS_REFRESH_MS) return;
      lastRefreshMs = input.nowMs;
      const summary = summarize();
      snapshot = {
        active: input.active,
        fps: input.deltaSeconds > 0 ? 1 / input.deltaSeconds : 0,
        frameMs: input.deltaSeconds * 1000,
        frameMsP95: summary.frameMsP95,
        frames: summary.frameCount,
        lastLoadStatus: snapshot.lastLoadStatus,
        r3fPerformanceCurrent: input.r3fPerformance.current,
        rendererCalls: input.renderer.info.render.calls,
        rendererTriangles: input.renderer.info.render.triangles,
        steps: input.fixedSteps,
        updatedAtMs: input.nowMs,
        waveGrid: waveGridLabel(input.waveGrid),
      };
      render();
    },
    recordLoad(status) {
      snapshot = { ...snapshot, lastLoadStatus: status, updatedAtMs: performance.now() };
      render();
    },
  };
}

let DOT_MATRIX_PANEL_OWNER_COUNTER = 0;

function noopDevTools(): DotMatrixDevTools {
  return {
    dispose() {},
    recordFrame() {},
    recordLoad() {},
  };
}

function quantile(sorted: readonly number[], q: number): number {
  if (sorted.length === 0) return 0;
  const index = Math.min(sorted.length - 1, Math.max(0, Math.ceil(sorted.length * q) - 1));
  return sorted[index] ?? 0;
}

function shouldSampleSession(rate: number): boolean {
  if (rate <= 0) return false;
  if (rate >= 1) return true;
  return hashToUnitInterval(readCorrelationSeed()) < rate;
}

function readCorrelationSeed(): string {
  if (typeof document === "undefined") return cryptoSeed();
  for (const part of document.cookie.split(";")) {
    const index = part.indexOf("=");
    if (index === -1) continue;
    if (part.slice(0, index).trim() === CORRELATION_COOKIE_NAME) {
      const value = part.slice(index + 1).trim();
      if (value !== "") return value;
    }
  }
  return cryptoSeed();
}

function cryptoSeed(): string {
  return globalThis.crypto?.randomUUID?.() ?? String(Math.random());
}

function hashToUnitInterval(value: string): number {
  let hash = 2166136261;
  for (let index = 0; index < value.length; index += 1) {
    hash ^= value.charCodeAt(index);
    hash = Math.imul(hash, 16777619);
  }
  return (hash >>> 0) / 0xffffffff;
}

function currentRoutePath(): string {
  if (typeof window === "undefined") return "";
  return window.location.pathname;
}

function viewportClass(viewport: DotMatrixViewportTelemetry): string {
  const width = viewport.w < 480 ? "phone" : viewport.w < 900 ? "tablet" : "desktop";
  const height = viewport.h < 720 ? "short" : "tall";
  return `${width}_${height}`;
}

function dprBucket(dpr: number): string {
  if (dpr < 1.25) return "1x";
  if (dpr < 1.75) return "1_5x";
  if (dpr < 2.5) return "2x";
  return "3x";
}

function waveGridLabel(grid: DotMatrixWaveGridTelemetry): string {
  return `${grid.width}x${grid.height}`;
}

function formatNumber(value: number): string {
  if (!Number.isFinite(value)) return "0";
  return value.toFixed(2);
}

function formatRatio(value: number): string {
  return formatNumber(Math.min(1, Math.max(0, value)));
}

function formatSeconds(value: number): string {
  return formatNumber(Math.max(0, value));
}

function clampRate(value: number): number {
  if (!Number.isFinite(value)) return DOT_MATRIX_PRODUCTION_SAMPLE_RATE;
  return Math.min(1, Math.max(0, value));
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
