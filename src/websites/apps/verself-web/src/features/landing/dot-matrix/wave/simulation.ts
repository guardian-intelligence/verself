import {
  GLSL3,
  HalfFloatType,
  Mesh,
  NearestFilter,
  OrthographicCamera,
  PlaneGeometry,
  RGBAFormat,
  Scene,
  ShaderMaterial,
  type Texture,
  Vector2,
  Vector4,
  WebGLRenderer,
  WebGLRenderTarget,
} from "three";

import {
  dotMatrixWaveFragmentShader,
  dotMatrixWaveVertexShader,
} from "../shader/wave-simulation.generated";
import {
  DOT_MATRIX_MAX_WAVE_IMPULSES,
  DOT_MATRIX_WAVE_SIMULATION_CONFIG,
  type DotMatrixWaveSimulation,
  type WaveSimulationConfig,
  type WaveSimulationStep,
  type WaveTextureStats,
} from "./types";

interface WaveTargets {
  readonly height: number;
  readonly read: WebGLRenderTarget;
  readonly width: number;
  readonly write: WebGLRenderTarget;
}

export function createDotMatrixWaveSimulation(
  renderer: WebGLRenderer,
  camera: OrthographicCamera,
  geometry: PlaneGeometry,
  fieldTexture: Texture,
  config: WaveSimulationConfig = DOT_MATRIX_WAVE_SIMULATION_CONFIG,
): DotMatrixWaveSimulation {
  const scene = new Scene();
  let targets = createWaveTargets(160, 90);
  const impulseUniforms = Array.from(
    { length: DOT_MATRIX_MAX_WAVE_IMPULSES },
    () => new Vector4(0.5, 0.5, 0, 0),
  );
  const impulseShapeUniforms = Array.from(
    { length: DOT_MATRIX_MAX_WAVE_IMPULSES },
    () => new Vector4(1, 0, 0, 0),
  );
  const uniforms = {
    uActive: { value: 1 },
    uAmbient: { value: 0 },
    uDamping: { value: config.damping },
    uDelta: { value: 1 / 60 },
    uFieldState: { value: fieldTexture },
    uImpulseCount: { value: 0 },
    uImpulseShapes: { value: impulseShapeUniforms },
    uImpulses: { value: impulseUniforms },
    uState: { value: targets.read.texture },
    uTexel: { value: new Vector2(1 / targets.width, 1 / targets.height) },
    uWaveSpeed: { value: config.waveSpeed },
  };
  const material = new ShaderMaterial({
    depthTest: false,
    depthWrite: false,
    fragmentShader: dotMatrixWaveFragmentShader,
    glslVersion: GLSL3,
    uniforms,
    vertexShader: dotMatrixWaveVertexShader,
  });
  const plane = new Mesh(geometry, material);
  plane.frustumCulled = false;
  scene.add(plane);

  clearWaveTarget(renderer, targets.read);
  clearWaveTarget(renderer, targets.write);

  return {
    dispose() {
      scene.remove(plane);
      material.dispose();
      targets.read.dispose();
      targets.write.dispose();
    },
    inspect() {
      return inspectWaveTarget(renderer, targets.read, targets.width, targets.height);
    },
    resize(viewportWidth: number, viewportHeight: number) {
      const nextSize = resolveWaveSimulationSize(viewportWidth, viewportHeight, config);
      if (nextSize.width === targets.width && nextSize.height === targets.height) {
        return;
      }

      targets.read.dispose();
      targets.write.dispose();
      targets = createWaveTargets(nextSize.width, nextSize.height);
      clearWaveTarget(renderer, targets.read);
      clearWaveTarget(renderer, targets.write);
      uniforms.uState.value = targets.read.texture;
      uniforms.uTexel.value.set(1 / targets.width, 1 / targets.height);
    },
    setFieldTexture(texture: Texture) {
      uniforms.uFieldState.value = texture;
    },
    step(input: WaveSimulationStep) {
      const impulseCount = Math.min(input.impulses.length, DOT_MATRIX_MAX_WAVE_IMPULSES);
      for (let index = 0; index < DOT_MATRIX_MAX_WAVE_IMPULSES; index += 1) {
        const impulse = input.impulses[index];
        impulseUniforms[index]?.set(
          impulse?.x ?? 0.5,
          impulse?.y ?? 0.5,
          impulse?.radius ?? 0,
          impulse?.strength ?? 0,
        );
        impulseShapeUniforms[index]?.set(
          impulse?.normalX ?? 1,
          impulse?.normalY ?? 0,
          impulse?.frontLength ?? impulse?.radius ?? 0,
          impulse?.kind === "front" ? 1 : 0,
        );
      }

      uniforms.uActive.value = input.active ? 1 : 0;
      uniforms.uAmbient.value = input.ambient;
      uniforms.uDelta.value = input.deltaSeconds;
      uniforms.uImpulseCount.value = impulseCount;
      uniforms.uState.value = targets.read.texture;

      const previousTarget = renderer.getRenderTarget();
      renderer.setRenderTarget(targets.write);
      renderer.render(scene, camera);
      renderer.setRenderTarget(previousTarget);

      targets = {
        height: targets.height,
        read: targets.write,
        width: targets.width,
        write: targets.read,
      };

      return targets.read.texture;
    },
    texture() {
      return targets.read.texture;
    },
  };
}

function createWaveTargets(width: number, height: number): WaveTargets {
  const options = {
    depthBuffer: false,
    format: RGBAFormat,
    generateMipmaps: false,
    magFilter: NearestFilter,
    minFilter: NearestFilter,
    stencilBuffer: false,
    type: HalfFloatType,
  };
  const read = new WebGLRenderTarget(width, height, options);
  const write = new WebGLRenderTarget(width, height, options);
  read.texture.name = "dot-matrix-wave-read";
  write.texture.name = "dot-matrix-wave-write";
  return { height, read, width, write };
}

function clearWaveTarget(renderer: WebGLRenderer, target: WebGLRenderTarget): void {
  const previousTarget = renderer.getRenderTarget();
  renderer.setRenderTarget(target);
  renderer.clear();
  renderer.setRenderTarget(previousTarget);
}

function resolveWaveSimulationSize(
  viewportWidth: number,
  viewportHeight: number,
  config: WaveSimulationConfig,
): { readonly height: number; readonly width: number } {
  const aspect = Math.max(viewportWidth, 1) / Math.max(viewportHeight, 1);
  const size =
    aspect >= 1
      ? {
          height: Math.round(config.gridLongEdge / aspect),
          width: config.gridLongEdge,
        }
      : {
          height: config.gridLongEdge,
          width: Math.round(config.gridLongEdge * aspect),
        };

  return {
    height: clampInteger(size.height, config.minGridEdge, config.maxGridEdge),
    width: clampInteger(size.width, config.minGridEdge, config.maxGridEdge),
  };
}

function inspectWaveTarget(
  renderer: WebGLRenderer,
  target: WebGLRenderTarget,
  width: number,
  height: number,
): WaveTextureStats | undefined {
  const pixels = new Uint16Array(width * height * 4);

  try {
    renderer.readRenderTargetPixels(target, 0, 0, width, height, pixels);
  } catch {
    return undefined;
  }

  let heightTotal = 0;
  let velocityTotal = 0;
  let maxAbsHeight = 0;
  let maxAbsVelocity = 0;
  const sampleCount = width * height;

  for (let index = 0; index < pixels.length; index += 4) {
    const heightValue = halfFloatToNumber(pixels[index] ?? 0);
    const previousHeightValue = halfFloatToNumber(pixels[index + 1] ?? 0);
    const absHeight = Math.abs(heightValue);
    const absVelocity = Math.abs(heightValue - previousHeightValue);
    heightTotal += absHeight;
    velocityTotal += absVelocity;
    maxAbsHeight = Math.max(maxAbsHeight, absHeight);
    maxAbsVelocity = Math.max(maxAbsVelocity, absVelocity);
  }

  return {
    averageAbsHeight: heightTotal / sampleCount,
    averageAbsVelocity: velocityTotal / sampleCount,
    height,
    maxAbsHeight,
    maxAbsVelocity,
    sampleCount,
    width,
  };
}

function halfFloatToNumber(value: number): number {
  const sign = value & 0x8000 ? -1 : 1;
  const exponent = (value >> 10) & 0x1f;
  const fraction = value & 0x03ff;

  if (exponent === 0) {
    return sign * 2 ** -14 * (fraction / 1024);
  }
  if (exponent === 0x1f) {
    return fraction === 0 ? sign * Infinity : Number.NaN;
  }
  return sign * 2 ** (exponent - 15) * (1 + fraction / 1024);
}

function clampInteger(value: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, value));
}
