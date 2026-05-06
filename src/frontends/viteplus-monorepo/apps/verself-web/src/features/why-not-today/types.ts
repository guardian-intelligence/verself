export type RendererBackend = "webgl2" | "none";

export type DegradedReason =
  | "reduced_motion"
  | "no_renderer"
  | "renderer_init_failed"
  | "compile_error"
  | "context_lost";

export interface SceneViewport {
  readonly w: number;
  readonly h: number;
  readonly dpr: number;
}

export interface SceneFrame {
  readonly viewport: SceneViewport;
}
