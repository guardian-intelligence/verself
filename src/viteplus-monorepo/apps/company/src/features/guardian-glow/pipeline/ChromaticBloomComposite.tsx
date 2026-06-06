import { useMemo } from "react";
import { BlendFunction, Effect } from "postprocessing";
import { Uniform } from "three";
import { guardianGlowStageCode, type GuardianGlowSettings } from "../guardian-glow-config";

const chromaticBloomCompositeShader = /* glsl */ `
  uniform float uAberrationPx;
  uniform float uBloomIntensity;
  uniform float uBlurRadius;
  uniform float uGradientGate;
  uniform float uKnee;
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

  vec3 chromaticBloomAt(vec2 uv) {
    vec2 redOffset = texelSize.xy * uAberrationPx * vec2(-0.72, 0.46);
    vec2 blueOffset = texelSize.xy * uAberrationPx * vec2(0.9, -0.58);
    float redBloom = bloomAt(uv + redOffset);
    float greenBloom = bloomAt(uv);
    float blueBloom = bloomAt(uv + blueOffset);
    return vec3(redBloom * 0.92, greenBloom * 0.84, blueBloom * 1.18);
  }

  void mainImage(const in vec4 inputColor, const in vec2 uv, out vec4 outputColor) {
    vec3 beauty = inputColor.rgb;
    float highlight = extractAt(uv);

    if (uStage < 4.5) {
      outputColor = vec4(vec3(highlight * 1.8), inputColor.a);
      return;
    }

    float centralBloom = bloomAt(uv) * uBloomIntensity;

    if (uStage < 5.5) {
      outputColor = vec4(vec3(centralBloom), inputColor.a);
      return;
    }

    float edgeGate = smoothstep(uGradientGate * 0.18, max(uGradientGate, 0.001), gradientAt(uv));
    vec3 dispersedBloom = chromaticBloomAt(uv) * edgeGate * uBloomIntensity;

    if (uStage < 6.5) {
      outputColor = vec4(dispersedBloom, inputColor.a);
      return;
    }

    outputColor = vec4(beauty + dispersedBloom, inputColor.a);
  }
`;

class ChromaticBloomCompositeEffectImpl extends Effect {
  constructor(settings: GuardianGlowSettings) {
    super("ChromaticBloomComposite", chromaticBloomCompositeShader, {
      blendFunction: BlendFunction.NORMAL,
      uniforms: new Map([
        ["uAberrationPx", new Uniform(settings.aberration)],
        ["uBloomIntensity", new Uniform(settings.bloom)],
        ["uBlurRadius", new Uniform(settings.blur)],
        ["uGradientGate", new Uniform(settings.gradient)],
        ["uKnee", new Uniform(settings.knee)],
        ["uStage", new Uniform(guardianGlowStageCode(settings.stage))],
        ["uThreshold", new Uniform(settings.threshold)],
      ]),
    });
  }
}

type ChromaticBloomCompositeProps = {
  readonly settings: GuardianGlowSettings;
};

export function ChromaticBloomComposite({ settings }: ChromaticBloomCompositeProps) {
  const effect = useMemo(() => new ChromaticBloomCompositeEffectImpl(settings), [settings]);

  return <primitive object={effect} />;
}
