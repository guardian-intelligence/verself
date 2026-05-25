export const DOT_MATRIX_DOT_SPACING_CSS_PX = 7.75;
export const DOT_MATRIX_RENDER_DPR_CAP = 1.5;

export type DotMatrixDegradedReason =
  | "compile_error"
  | "context_lost"
  | "float_render_target_unsupported"
  | "no_renderer"
  | "reduced_motion"
  | "renderer_init_failed";

export type DotMatrixRendererBackend = "none" | "webgl2" | "webgl2_no_float_render_target";

export interface DotMatrixFrame {
  readonly viewport: {
    readonly dpr: number;
    readonly h: number;
    readonly w: number;
  };
}
