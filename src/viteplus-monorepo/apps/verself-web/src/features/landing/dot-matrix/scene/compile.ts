import {
  ClampToEdgeWrapping,
  DataTexture,
  LinearFilter,
  RGBAFormat,
  UnsignedByteType,
} from "three";

import type { CompiledWaveScene, DomainRect, LandingWaveScene } from "./types";

interface WaveSceneTextureSize {
  readonly height: number;
  readonly width: number;
}

const SIMULATION_DOMAIN: DomainRect = {
  height: 1,
  width: 1,
  x: 0,
  y: 0,
};

export function createCompiledWaveScene(
  initialScene: LandingWaveScene,
  size: WaveSceneTextureSize,
): CompiledWaveScene {
  const data = new Uint8Array(size.width * size.height * 4);
  const texture = new DataTexture(data, size.width, size.height, RGBAFormat, UnsignedByteType);
  texture.name = "dot-matrix-wave-fields";
  texture.generateMipmaps = false;
  texture.magFilter = LinearFilter;
  texture.minFilter = LinearFilter;
  texture.wrapS = ClampToEdgeWrapping;
  texture.wrapT = ClampToEdgeWrapping;

  let scene = initialScene;

  const writeTexture = () => {
    for (let y = 0; y < size.height; y += 1) {
      for (let x = 0; x < size.width; x += 1) {
        const point = {
          x: SIMULATION_DOMAIN.x + ((x + 0.5) / size.width) * SIMULATION_DOMAIN.width,
          y: SIMULATION_DOMAIN.y + ((y + 0.5) / size.height) * SIMULATION_DOMAIN.height,
        };
        const sample = scene.fields.at(point);
        const index = (y * size.width + x) * 4;
        data[index] = encodeField(sample.visibility);
        data[index + 1] = encodeField(sample.spawn);
        data[index + 2] = encodeField(sample.absorption);
        data[index + 3] = encodeField(sample.readability);
      }
    }

    texture.needsUpdate = true;
  };

  writeTexture();

  return {
    get cameraRect(): DomainRect {
      return scene.cameraRect;
    },
    dispose() {
      texture.dispose();
    },
    get fields() {
      return scene.fields;
    },
    texture,
    update(nextScene: LandingWaveScene) {
      scene = nextScene;
      writeTexture();
    },
  };
}

function encodeField(value: number): number {
  return Math.round(Math.min(1, Math.max(0, value)) * 255);
}
