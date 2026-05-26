import type { DataTexture } from "three";

export interface Vec2 {
  readonly x: number;
  readonly y: number;
}

export interface DomainRect {
  readonly height: number;
  readonly width: number;
  readonly x: number;
  readonly y: number;
}

export interface WaveFieldSample {
  readonly readability: number;
  readonly spawn: number;
  readonly visibility: number;
}

export type WaveFieldMask = (point: Vec2) => number;

export interface CompiledWaveFields {
  readonly at: (point: Vec2) => WaveFieldSample;
  readonly readabilityAt: WaveFieldMask;
  readonly spawnAt: WaveFieldMask;
  readonly visibilityAt: WaveFieldMask;
}

export interface LandingWaveScene {
  readonly cameraRect: DomainRect;
  readonly fields: CompiledWaveFields;
}

export interface CompiledWaveScene {
  readonly cameraRect: DomainRect;
  readonly dispose: () => void;
  readonly fields: CompiledWaveFields;
  readonly texture: DataTexture;
  readonly update: (scene: LandingWaveScene) => void;
}
