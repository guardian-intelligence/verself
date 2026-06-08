import { useMemo } from "react";
import { useFrame } from "@react-three/fiber";
import { BlendFunction, Effect } from "postprocessing";
import { Uniform, Vector2, Vector3 } from "three";
import { guardianGlowStageCode, type GuardianGlowSettings } from "../guardian-glow-config";

const chromaticBloomCompositeShader = /* glsl */ `
  uniform float uAberrationPx;
  uniform float uBloomIntensity;
  uniform float uBlurRadius;
  uniform float uGradientGate;
  uniform float uKnee;
  uniform vec2 uEclipseUv;
  uniform float uRayIntensity;
  uniform float uRayLength;
  uniform float uStage;
  uniform float uThreshold;

  float sceneLuma(vec3 color) {
    return dot(max(color, vec3(0.0)), vec3(0.2126, 0.7152, 0.0722));
  }

  vec3 readScene(vec2 uv) {
    vec2 safeUv = clamp(uv, texelSize.xy * 0.5, vec2(1.0) - texelSize.xy * 0.5);
    return texture2D(inputBuffer, safeUv).rgb;
  }

  float gradientAt(vec2 uv) {
    vec2 stepUv = texelSize.xy;
    float left = sceneLuma(readScene(uv - vec2(stepUv.x, 0.0)));
    float right = sceneLuma(readScene(uv + vec2(stepUv.x, 0.0)));
    float down = sceneLuma(readScene(uv - vec2(0.0, stepUv.y)));
    float up = sceneLuma(readScene(uv + vec2(0.0, stepUv.y)));
    return length(vec2(right - left, up - down));
  }

  float extractBrightAt(vec2 uv) {
    float y = sceneLuma(readScene(uv));
    float knee = max(uKnee, 0.001);
    float soft = smoothstep(uThreshold - knee, uThreshold + knee, y);
    float energy = max(y - uThreshold, 0.0) / max(uThreshold + knee, 0.001);
    return soft * energy;
  }

  float extractAt(vec2 uv) {
    float gradientGate = smoothstep(uGradientGate * 0.18, max(uGradientGate, 0.001), gradientAt(uv));
    return extractBrightAt(uv) * mix(0.18, 1.0, gradientGate);
  }

  float blurExtract(vec2 uv, float radius) {
    vec2 r = texelSize.xy * radius;
    float sum = extractBrightAt(uv) * 0.24;
    sum += extractBrightAt(uv + vec2( r.x,  0.0)) * 0.13;
    sum += extractBrightAt(uv + vec2(-r.x,  0.0)) * 0.13;
    sum += extractBrightAt(uv + vec2( 0.0,  r.y)) * 0.13;
    sum += extractBrightAt(uv + vec2( 0.0, -r.y)) * 0.13;
    sum += extractBrightAt(uv + vec2( r.x * 0.72,  r.y * 0.72)) * 0.06;
    sum += extractBrightAt(uv + vec2(-r.x * 0.72,  r.y * 0.72)) * 0.06;
    sum += extractBrightAt(uv + vec2( r.x * 0.72, -r.y * 0.72)) * 0.06;
    sum += extractBrightAt(uv + vec2(-r.x * 0.72, -r.y * 0.72)) * 0.06;
    return sum;
  }

  float bloomAt(vec2 uv) {
    float radius = max(uBlurRadius, 0.5);
    float inner = blurExtract(uv, radius);
    float outer = blurExtract(uv, radius * 2.85);
    return inner * 0.68 + outer * 0.32;
  }

  float softRaySeedAt(vec2 uv) {
    return blurExtract(uv, max(uBlurRadius * 1.1, 8.0));
  }

  vec3 chromaticBloomAt(vec2 uv) {
    vec2 redOffset = texelSize.xy * uAberrationPx * vec2(-0.72, 0.46);
    vec2 blueOffset = texelSize.xy * uAberrationPx * vec2(0.9, -0.58);
    float redBloom = bloomAt(uv + redOffset);
    float greenBloom = bloomAt(uv);
    float blueBloom = bloomAt(uv + blueOffset);
    return vec3(redBloom * 0.92, greenBloom * 0.84, blueBloom * 1.18);
  }

  float radialRaysAt(vec2 uv) {
    vec2 delta = uv - uEclipseUv;
    float distanceFromSource = length(delta);
    vec2 direction = delta / max(distanceFromSource, 0.0001);
    float stepPixels = max(uBlurRadius * (0.72 + clamp(uRayLength, 0.01, 1.8) * 3.2), 4.0);
    float sum = 0.0;
    float weightSum = 0.0;

    for (int i = 0; i < 8; i += 1) {
      float t = float(i) / 7.0;
      vec2 sampleUv = uv - direction * texelSize.xy * stepPixels * float(i);
      float sampleDistance = length(sampleUv - uEclipseUv);
      float seedBand =
        smoothstep(0.03, 0.12, sampleDistance) *
        (1.0 - smoothstep(0.42, 0.82, sampleDistance));
      float sampleWeight = pow(1.0 - t, 1.8) * seedBand;
      float seed = extractBrightAt(sampleUv);

      sum += seed * sampleWeight;
      weightSum += sampleWeight;
    }

    float shaft = sum / max(weightSum, 0.001);
    float localCorona = softRaySeedAt(uv) * (1.0 - smoothstep(0.48, 0.92, distanceFromSource));
    float nearSourceGate = smoothstep(0.012, 0.08, distanceFromSource);
    float farSourceGate = 1.0 - smoothstep(0.64, 1.02, distanceFromSource);
    float scatter = pow(max(shaft, 0.0), 1.1) * 0.12 + pow(max(localCorona, 0.0), 0.86) * 0.52;
    return scatter * nearSourceGate * farSourceGate * uRayIntensity;
  }

  vec3 radialRayColor(float ray) {
    return ray * vec3(1.02, 0.92, 0.72);
  }

  void mainImage(const in vec4 inputColor, const in vec2 uv, out vec4 outputColor) {
    vec3 beauty = inputColor.rgb;
    float highlight = extractAt(uv);
    float ray = radialRaysAt(uv);
    vec3 rayColor = radialRayColor(ray);

    if (uStage < 5.5) {
      outputColor = vec4(vec3(highlight * 1.8), inputColor.a);
      return;
    }

    if (uStage < 6.5) {
      outputColor = vec4(rayColor, inputColor.a);
      return;
    }

    float centralBloom = bloomAt(uv) * uBloomIntensity;

    if (uStage < 7.5) {
      outputColor = vec4(vec3(centralBloom) + rayColor, inputColor.a);
      return;
    }

    float edgeGate = smoothstep(uGradientGate * 0.18, max(uGradientGate, 0.001), gradientAt(uv));
    vec3 dispersedBloom = chromaticBloomAt(uv) * edgeGate * uBloomIntensity;

    if (uStage < 8.5) {
      outputColor = vec4(rayColor + dispersedBloom, inputColor.a);
      return;
    }

    outputColor = vec4(beauty + rayColor + dispersedBloom, inputColor.a);
  }
`;

class ChromaticBloomCompositeEffectImpl extends Effect {
  constructor(settings: GuardianGlowSettings) {
    super("ChromaticBloomComposite", chromaticBloomCompositeShader, {
      blendFunction: BlendFunction.NORMAL,
      uniforms: new Map<string, Uniform>([
        ["uAberrationPx", new Uniform(settings.aberration)],
        ["uBloomIntensity", new Uniform(settings.bloom)],
        ["uBlurRadius", new Uniform(settings.blur)],
        ["uEclipseUv", new Uniform(new Vector2(0.5, 0.5))],
        ["uGradientGate", new Uniform(settings.gradient)],
        ["uKnee", new Uniform(settings.knee)],
        ["uRayIntensity", new Uniform(settings.rayIntensity)],
        ["uRayLength", new Uniform(settings.rayLength)],
        ["uStage", new Uniform(guardianGlowStageCode(settings.stage))],
        ["uThreshold", new Uniform(settings.threshold)],
      ]),
    });
  }
}

type ChromaticBloomCompositeProps = {
  readonly eclipseSourceWorldPosition: readonly [number, number, number];
  readonly settings: GuardianGlowSettings;
};

export function ChromaticBloomComposite({
  eclipseSourceWorldPosition,
  settings,
}: ChromaticBloomCompositeProps) {
  const effect = useMemo(() => new ChromaticBloomCompositeEffectImpl(settings), [settings]);
  const sourceWorldPosition = useMemo(
    () => new Vector3(...eclipseSourceWorldPosition),
    [eclipseSourceWorldPosition],
  );
  const projectedSourcePosition = useMemo(() => new Vector3(), []);

  useFrame(({ camera }) => {
    const sourceUv = effect.uniforms.get("uEclipseUv")?.value;
    if (!(sourceUv instanceof Vector2)) {
      return;
    }

    projectedSourcePosition.copy(sourceWorldPosition).project(camera);
    sourceUv.set(
      projectedSourcePosition.x * 0.5 + 0.5,
      projectedSourcePosition.y * 0.5 + 0.5,
    );
  });

  return <primitive object={effect} />;
}
