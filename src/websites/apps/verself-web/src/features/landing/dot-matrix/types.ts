export type DotMatrixDegradedReason =
  | "compile_error"
  | "context_lost"
  | "no_renderer"
  | "reduced_motion"
  | "renderer_init_failed";

export type DotMatrixRendererBackend = "none" | "webgl2";

export interface DotMatrixFrame {
  readonly viewport: {
    readonly dpr: number;
    readonly h: number;
    readonly w: number;
  };
}
