export type RendererBackend = "webgl2" | "none";

export type DegradedReason =
  | "reduced_motion"
  | "no_renderer"
  | "renderer_init_failed"
  | "compile_error"
  | "context_lost";

export interface FirstLightViewport {
  readonly w: number;
  readonly h: number;
  readonly dpr: number;
}

export interface FirstLightFrame {
  readonly viewport: FirstLightViewport;
}

export interface FrameMetrics {
  readonly p50: number;
  readonly p99: number;
  readonly samples: number;
}
