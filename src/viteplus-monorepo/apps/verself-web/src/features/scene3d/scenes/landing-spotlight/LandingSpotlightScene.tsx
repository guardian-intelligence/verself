import { shaderMaterial } from "@react-three/drei";
import { extend, useFrame, useThree } from "@react-three/fiber";
import { useMemo, useRef } from "react";
import { AdditiveBlending, Vector2, type ShaderMaterial, type SpotLight } from "three";
import {
  landingSpotlightSources,
  resolveSpotlightState,
  resolveSpotlightWorldPosition,
} from "./spotlight-config";

const SpotlightWashMaterial = shaderMaterial(
  {
    uCenter0: new Vector2(0.5, 0.45),
    uCenter1: new Vector2(0.66, 0.4),
    uPeak0: 0.1,
    uPeak1: 0.08,
    uRadius0: 0.27,
    uRadius1: 0.2,
    uResolution: new Vector2(1, 1),
  },
  /* glsl */ `
    varying vec2 vUv;

    void main() {
      vUv = uv;
      gl_Position = projectionMatrix * modelViewMatrix * vec4(position, 1.0);
    }
  `,
  /* glsl */ `
    uniform vec2 uCenter0;
    uniform vec2 uCenter1;
    uniform float uPeak0;
    uniform float uPeak1;
    uniform float uRadius0;
    uniform float uRadius1;
    varying vec2 vUv;

    float cssSpot(vec2 uv, vec2 center, float peak, float radius) {
      float distanceToCenter = distance(uv, center);
      return peak * (1.0 - smoothstep(0.0, radius, distanceToCenter));
    }

    void main() {
      vec3 wash = vec3(0.0);
      wash += cssSpot(vUv, uCenter0, uPeak0, uRadius0);
      wash += cssSpot(vUv, uCenter1, uPeak1, uRadius1);
      float alpha = clamp(max(max(wash.r, wash.g), wash.b), 0.0, 1.0);
      gl_FragColor = vec4(wash, alpha);
    }
  `,
);

extend({ SpotlightWashMaterial });

declare module "@react-three/fiber" {
  interface ThreeElements {
    spotlightWashMaterial: ThreeElements["shaderMaterial"] & {
      uCenter0?: Vector2;
      uCenter1?: Vector2;
      uPeak0?: number;
      uPeak1?: number;
      uRadius0?: number;
      uRadius1?: number;
      uResolution?: Vector2;
    };
  }
}

export function LandingSpotlightScene() {
  const { viewport, size } = useThree();
  const washRef = useRef<ShaderMaterial>(null);
  const primaryLight = useRef<SpotLight>(null);
  const secondaryLight = useRef<SpotLight>(null);
  const animateRef = useRef(resolveSpotlightAnimation());

  const resolution = useMemo(() => new Vector2(size.width, size.height), [size.height, size.width]);
  const lights = useMemo(
    () =>
      landingSpotlightSources.map((source, index) => {
        const radiusPx = Math.min(viewport.width, viewport.height) * source.radius;
        return {
          angle: Math.atan(radiusPx / source.z),
          key: `${source.x}-${source.y}`,
          ref: index === 0 ? primaryLight : secondaryLight,
          source,
        };
      }),
    [viewport.height, viewport.width],
  );

  useFrame(({ clock }) => {
    const elapsedSeconds = clock.elapsedTime;
    const animate = animateRef.current;
    const wash = washRef.current;

    landingSpotlightSources.forEach((source, index) => {
      const state = resolveSpotlightState(source, elapsedSeconds, animate);
      const worldPosition = resolveSpotlightWorldPosition(
        viewport.width,
        viewport.height,
        state,
        source.z,
      );
      const light = index === 0 ? primaryLight.current : secondaryLight.current;

      if (light) {
        light.position.set(...worldPosition);
        light.intensity = state.peak * 42;
      }

      if (!wash) {
        return;
      }

      if (index === 0) {
        wash.uniforms.uCenter0!.value.set(state.centerUv[0], state.centerUv[1]);
        wash.uniforms.uPeak0!.value = state.peak;
        wash.uniforms.uRadius0!.value = state.radius;
      } else {
        wash.uniforms.uCenter1!.value.set(state.centerUv[0], state.centerUv[1]);
        wash.uniforms.uPeak1!.value = state.peak;
        wash.uniforms.uRadius1!.value = state.radius;
      }
    });

    if (wash) {
      wash.uniforms.uResolution!.value.set(size.width, size.height);
    }
  });

  return (
    <>
      {lights.map(({ angle, key, ref, source }) => (
        <spotLight
          key={key}
          ref={ref}
          angle={angle}
          color="#ffffff"
          decay={source.decay}
          intensity={source.peak * 42}
          penumbra={0.94}
          position={[0, 0, source.z]}
        >
          <object3D attach="target" position={[0, 0, -source.z]} />
        </spotLight>
      ))}
      <mesh position={[0, 0, 0]}>
        <planeGeometry args={[viewport.width, viewport.height]} />
        <spotlightWashMaterial
          ref={washRef}
          blending={AdditiveBlending}
          depthWrite={false}
          transparent
          uResolution={resolution}
        />
      </mesh>
    </>
  );
}

function resolveSpotlightAnimation(): boolean {
  if (typeof window === "undefined") {
    return false;
  }

  return !window.matchMedia("(prefers-reduced-motion: reduce)").matches;
}
