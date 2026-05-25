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

export type WaveFieldKind = "absorption" | "readability" | "spawn" | "visibility";
export type WaveRegionKind = WaveFieldKind | "camera" | "simulation";

export type WaveFieldCombine = "add" | "max" | "multiply" | "subtract";

export type WaveFieldShape =
  | {
      readonly kind: "ellipse";
      readonly center: Vec2;
      readonly radius: Vec2;
    }
  | {
      readonly kind: "polygon";
      readonly points: readonly Vec2[];
    }
  | {
      readonly kind: "rect";
      readonly rect: DomainRect;
    }
  | {
      readonly kind: "roundedRect";
      readonly radius: number;
      readonly rect: DomainRect;
    };

export interface WaveFieldRegion {
  readonly combine: WaveFieldCombine;
  readonly feather: number;
  readonly field: WaveRegionKind;
  readonly id: string;
  readonly shape: WaveFieldShape;
  readonly weight: number;
}

export interface WaveSceneModel {
  readonly regions: readonly WaveFieldRegion[];
}

export interface WaveFieldSample {
  readonly absorption: number;
  readonly readability: number;
  readonly spawn: number;
  readonly visibility: number;
}

export type WaveFieldMask = (point: Vec2) => number;

export interface CompiledWaveFields {
  readonly absorptionAt: WaveFieldMask;
  readonly at: (point: Vec2) => WaveFieldSample;
  readonly readabilityAt: WaveFieldMask;
  readonly spawnAt: WaveFieldMask;
  readonly visibilityAt: WaveFieldMask;
}

export interface CompiledWaveScene {
  readonly cameraRect: DomainRect;
  readonly dispose: () => void;
  readonly fields: CompiledWaveFields;
  readonly model: WaveSceneModel;
  readonly simulationDomain: DomainRect;
  readonly texture: DataTexture;
  readonly update: (model: WaveSceneModel) => void;
}
