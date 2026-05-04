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

export type FirstLightDebugMode = "composite" | "shape" | "lens" | "disk";

export interface CelestialShapeProfile {
  readonly center: readonly [number, number];
  readonly radius: number;
  readonly aspect: number;
  readonly rotation: number;
  readonly exponent: number;
  readonly pinch: number;
  readonly ripple: number;
  readonly rippleFrequency: number;
  readonly softness: number;
}

export interface CelestialLensProfile {
  readonly strength: number;
  readonly radius: number;
  readonly falloff: number;
  readonly ringWidth: number;
  readonly ringIntensity: number;
}

export interface CelestialDiskProfile {
  readonly tilt: number;
  readonly innerRadius: number;
  readonly outerRadius: number;
  readonly intensity: number;
  readonly warmth: number;
  readonly spin: number;
  readonly anisotropy: number;
}

export interface CelestialRenderProfile {
  readonly seed: number;
  readonly motionScale: number;
  readonly shape: CelestialShapeProfile;
  readonly lens: CelestialLensProfile;
  readonly disk: CelestialDiskProfile;
}

export interface FrameMetrics {
  readonly p50: number;
  readonly p99: number;
  readonly samples: number;
}
