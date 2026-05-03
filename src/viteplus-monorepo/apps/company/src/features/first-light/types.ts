export type RendererBackend = "webgl2" | "none";

export type DegradedReason =
  | "reduced_motion"
  | "no_renderer"
  | "renderer_init_failed"
  | "compile_error"
  | "context_lost";

export interface FirstLightRect {
  readonly x: number;
  readonly y: number;
  readonly w: number;
  readonly h: number;
}

export interface FirstLightViewport {
  readonly w: number;
  readonly h: number;
  readonly dpr: number;
}

export interface FirstLightGeometry {
  readonly viewport: FirstLightViewport;
  readonly trail: FirstLightRect;
  readonly wings: FirstLightRect;
}

export interface ArrivalFrameMetrics {
  readonly p50: number;
  readonly p99: number;
  readonly samples: number;
}
