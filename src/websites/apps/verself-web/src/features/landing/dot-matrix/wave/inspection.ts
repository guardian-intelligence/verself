import type { WaveTextureStats } from "./types";

export interface DotMatrixWaveInspectionRecord {
  readonly ambientFrontLengths: readonly number[];
  readonly ambientFrontImpulses: number;
  readonly ambientImpulseOrigins: ReadonlyArray<{
    readonly x: number;
    readonly y: number;
  }>;
  readonly ambientImpulses: number;
  readonly clickImpulses: number;
  readonly droppedSeconds: number;
  readonly frameDeltaSeconds: number;
  readonly queuedImpulses: number;
  readonly simulationSeconds: number;
  readonly steps: number;
  readonly textureStats?: WaveTextureStats;
  readonly timeSeconds: number;
  readonly travelSpeedScale: number;
}

export interface DotMatrixWaveInspectionSnapshot {
  readonly records: ReadonlyArray<DotMatrixWaveInspectionRecord>;
}

declare global {
  interface Window {
    __dotMatrixWaveInspection?: {
      readonly snapshot: () => DotMatrixWaveInspectionSnapshot;
    };
  }
}

export function createDotMatrixWaveInspection(maxRecords: number) {
  const records: DotMatrixWaveInspectionRecord[] = [];
  const api = {
    snapshot(): DotMatrixWaveInspectionSnapshot {
      return {
        records: [...records],
      };
    },
  };

  return {
    install() {
      window.__dotMatrixWaveInspection = api;
    },
    record(record: DotMatrixWaveInspectionRecord) {
      records.push(record);
      while (records.length > maxRecords) {
        records.shift();
      }
    },
    uninstall() {
      if (window.__dotMatrixWaveInspection === api) {
        delete window.__dotMatrixWaveInspection;
      }
    },
  };
}
