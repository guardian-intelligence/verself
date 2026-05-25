import {
  ClampToEdgeWrapping,
  DataTexture,
  LinearFilter,
  RGBAFormat,
  UnsignedByteType,
} from "three";

import { compileWaveFields } from "./fields";
import { clampRect, shapeBounds } from "./shapes";
import type {
  CompiledWaveFields,
  CompiledWaveScene,
  DomainRect,
  WaveFieldRegion,
  WaveRegionKind,
  WaveSceneModel,
} from "./types";

interface WaveSceneTextureSize {
  readonly height: number;
  readonly width: number;
}

export function createCompiledWaveScene(
  initialModel: WaveSceneModel,
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

  let model = initialModel;
  let fields = compileWaveFields(model.regions);
  let simulationDomain = compileRegionBounds(model.regions, "simulation");
  let cameraRect = clampRect(compileRegionBounds(model.regions, "camera"), simulationDomain);

  const writeTexture = () => {
    fields = compileWaveFields(model.regions);
    simulationDomain = compileRegionBounds(model.regions, "simulation");
    cameraRect = clampRect(compileRegionBounds(model.regions, "camera"), simulationDomain);
    const domain = simulationDomain;

    for (let y = 0; y < size.height; y += 1) {
      for (let x = 0; x < size.width; x += 1) {
        const point = {
          x: domain.x + ((x + 0.5) / size.width) * domain.width,
          y: domain.y + ((y + 0.5) / size.height) * domain.height,
        };
        const sample = fields.at(point);
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
      return cameraRect;
    },
    dispose() {
      texture.dispose();
    },
    get fields(): CompiledWaveFields {
      return fields;
    },
    get model(): WaveSceneModel {
      return model;
    },
    get simulationDomain(): DomainRect {
      return simulationDomain;
    },
    texture,
    update(nextModel: WaveSceneModel) {
      model = nextModel;
      writeTexture();
    },
  };
}

function encodeField(value: number): number {
  return Math.round(Math.min(1, Math.max(0, value)) * 255);
}

function compileRegionBounds(
  regions: readonly WaveFieldRegion[],
  field: WaveRegionKind,
): DomainRect {
  const matching = regions.filter((region) => region.field === field && region.weight > 0);
  if (matching.length === 0) {
    throw new Error(`wave scene requires at least one ${field} region`);
  }

  let xMin = Number.POSITIVE_INFINITY;
  let yMin = Number.POSITIVE_INFINITY;
  let xMax = Number.NEGATIVE_INFINITY;
  let yMax = Number.NEGATIVE_INFINITY;

  for (const region of matching) {
    const bounds = shapeBounds(region.shape);
    xMin = Math.min(xMin, bounds.x);
    yMin = Math.min(yMin, bounds.y);
    xMax = Math.max(xMax, bounds.x + bounds.width);
    yMax = Math.max(yMax, bounds.y + bounds.height);
  }

  return {
    height: yMax - yMin,
    width: xMax - xMin,
    x: xMin,
    y: yMin,
  };
}
