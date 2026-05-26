import type { Texture } from "three";

export const DOT_MATRIX_MAX_WAVE_IMPULSES = 6;
export const DOT_MATRIX_WAVE_FIXED_STEP_SECONDS = 1 / 60;
export const DOT_MATRIX_WAVE_TRAVEL_SPEED_SCALE = 2.8;

export interface WaveImpulse {
  readonly frontLength?: number;
  readonly kind?: "front" | "point";
  readonly normalX?: number;
  readonly normalY?: number;
  readonly radius: number;
  readonly strength: number;
  readonly x: number;
  readonly y: number;
}

export interface WaveSimulationConfig {
  readonly damping: number;
  readonly gridLongEdge: number;
  readonly maxGridEdge: number;
  readonly minGridEdge: number;
  readonly waveSpeed: number;
}

export interface WaveSimulationStep {
  readonly active: boolean;
  readonly ambient: number;
  readonly deltaSeconds: number;
  readonly impulses: ReadonlyArray<WaveImpulse>;
}

export interface DotMatrixWaveSimulation {
  readonly dispose: () => void;
  readonly resize: (viewportWidth: number, viewportHeight: number) => void;
  readonly setFieldTexture: (texture: Texture) => void;
  readonly step: (input: WaveSimulationStep) => Texture;
  readonly texture: () => Texture;
}

export const DOT_MATRIX_WAVE_SIMULATION_CONFIG = {
  damping: 0.9935,
  gridLongEdge: 192,
  maxGridEdge: 240,
  minGridEdge: 90,
  waveSpeed: 0.3,
} satisfies WaveSimulationConfig;
