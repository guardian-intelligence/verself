import { useEffect, useRef } from "react";
import {
  AdditiveBlending,
  GLSL3,
  Mesh,
  PerspectiveCamera,
  PlaneGeometry,
  Scene,
  ShaderMaterial,
  WebGLRenderer,
} from "three";
import type { DegradedReason, SceneFrame } from "../types";

interface CanvasProps {
  readonly active: boolean;
  readonly frame: SceneFrame;
  readonly onDegraded: (reason: DegradedReason, error?: unknown) => void;
}

// ShaderMaterial auto-injects `position`, `uv`, `modelViewMatrix`, and
// `projectionMatrix`. Redeclaring them silently breaks GLSL3 compilation.
const wavefieldVertex = /* glsl */ `
uniform float uTime;
uniform float uMotion;

out float vH;
out vec2 vP;

float wellHeight(vec2 p, float t) {
  // Plane (-1..1, -1..1). After mesh.rotation.x = -PI/2, original local Y maps
  // to world -Z, so local y = -1 sits closest to the camera at world Z = +1.
  // The V apex lives there; the two wells diverge as y grows toward +1.
  float zNorm = clamp((p.y + 1.0) * 0.5, 0.0, 1.0);
  float spread = mix(0.0, 0.92, pow(zNorm, 0.85));
  float sigma = mix(0.16, 0.30, zNorm);
  float depth = mix(1.65, 0.78, zNorm);

  float wellL = exp(-pow((p.x + spread) / sigma, 2.0));
  float wellR = exp(-pow((p.x - spread) / sigma, 2.0));
  float h = -depth * (wellL + wellR);

  float r = length(p - vec2(0.0, -1.0));
  float falloff = exp(-r * 0.55);
  h += 0.085 * sin(r * 13.0 - t * 1.4) * falloff;
  h += 0.04 * sin(r * 6.0 + t * 0.7) * falloff;
  h += 0.025 * sin(p.y * 3.5 + t * 0.35);
  return h;
}

void main() {
  vec2 p = position.xy;
  float t = uTime * uMotion;
  float h = wellHeight(p, t);
  vH = h;
  vP = p;
  // Local Z displacement becomes world Y (up) after the mesh's -PI/2 X rotation.
  gl_Position = projectionMatrix * modelViewMatrix * vec4(position.x, position.y, h, 1.0);
}
`;

const wavefieldFragment = /* glsl */ `
precision highp float;
in float vH;
in vec2 vP;
uniform float uTime;
uniform float uActive;
out vec4 fragColor;

void main() {
  float depthRaw = clamp(-vH, 0.0, 2.4);
  float depthN = smoothstep(0.0, 1.5, depthRaw);
  vec3 violet = vec3(0.42, 0.30, 0.95);
  vec3 cyan   = vec3(0.30, 0.78, 1.10);
  vec3 base   = mix(violet, cyan, depthN);

  // Concentric rings emanating from the V apex (vP = (0, -1)). The mesh
  // displaces each vertex by wellHeight, so rings naturally bend down into
  // the trough and form the gravitational-contour look from the reference.
  float r_apex = length(vP - vec2(0.0, -1.0));
  float ringPhase = r_apex * 9.0 - uTime * 0.55;
  float ring = smoothstep(0.10, 0.0, abs(fract(ringPhase) - 0.5));

  // Cross weave at constant x to give the wireframe-grid feel.
  float xMod = fract(vP.x * 18.0);
  float xBand = smoothstep(0.05, 0.0, abs(xMod - 0.5));

  vec3 col = base * (0.18 + ring * 1.55 + xBand * 0.42);
  col += cyan * smoothstep(0.55, 1.6, depthRaw) * 0.55;

  float vy = smoothstep(1.10, -0.20, vP.y);
  float vx = smoothstep(2.20, 0.10, abs(vP.x));
  float vignette = vx * vy;
  float alpha = uActive * vignette * clamp(0.30 + ring * 1.10 + depthN * 0.50, 0.0, 1.0);
  fragColor = vec4(col, alpha);
}
`;

const orbVertex = /* glsl */ `
out vec2 vUv;
void main() {
  vUv = uv;
  gl_Position = projectionMatrix * modelViewMatrix * vec4(position, 1.0);
}
`;

const orbFragment = /* glsl */ `
precision highp float;
in vec2 vUv;
uniform float uActive;
out vec4 fragColor;
void main() {
  float r = length(vUv - 0.5) * 2.0;
  float core = smoothstep(0.32, 0.0, r);
  float halo = exp(-r * r * 3.4);
  vec3 col = vec3(1.0) * core + vec3(0.78, 0.92, 1.10) * halo;
  fragColor = vec4(col * uActive, clamp(core + halo * 0.65, 0.0, 1.0) * uActive);
}
`;

export function WhyNotTodayCanvas({ active, frame, onDegraded }: CanvasProps) {
  const hostRef = useRef<HTMLDivElement>(null);
  const activeRef = useRef(active);
  const frameRef = useRef(frame);
  const onDegradedRef = useRef(onDegraded);

  useEffect(() => {
    activeRef.current = active;
  }, [active]);
  useEffect(() => {
    frameRef.current = frame;
  }, [frame]);
  useEffect(() => {
    onDegradedRef.current = onDegraded;
  }, [onDegraded]);

  useEffect(() => {
    const host = hostRef.current;
    if (!host) return;

    let renderer: WebGLRenderer;
    try {
      renderer = new WebGLRenderer({
        alpha: true,
        antialias: true,
        powerPreference: "high-performance",
      });
    } catch (error) {
      onDegradedRef.current("renderer_init_failed", error);
      return;
    }
    renderer.setClearColor(0x000000, 0);
    renderer.setPixelRatio(Math.min(frameRef.current.viewport.dpr, 2));
    renderer.domElement.style.position = "absolute";
    renderer.domElement.style.inset = "0";
    renderer.domElement.style.width = "100%";
    renderer.domElement.style.height = "100%";
    renderer.domElement.style.pointerEvents = "none";
    host.append(renderer.domElement);

    const scene = new Scene();
    const camera = new PerspectiveCamera(56, 1, 0.1, 50);
    camera.position.set(0, 1.55, 3.2);
    camera.lookAt(0, -0.3, 0.1);

    const wavefieldUniforms = {
      uTime: { value: 0 },
      uActive: { value: activeRef.current ? 1 : 0 },
      uMotion: { value: 1 },
    };
    const wavefieldMaterial = new ShaderMaterial({
      vertexShader: wavefieldVertex,
      fragmentShader: wavefieldFragment,
      uniforms: wavefieldUniforms,
      glslVersion: GLSL3,
      transparent: true,
      depthTest: false,
      depthWrite: false,
    });
    const wavefieldGeometry = new PlaneGeometry(8.0, 6.4, 320, 260);
    const wavefield = new Mesh(wavefieldGeometry, wavefieldMaterial);
    wavefield.rotation.x = -Math.PI / 2;
    wavefield.frustumCulled = false;
    scene.add(wavefield);

    const orbUniforms = {
      uActive: { value: activeRef.current ? 1 : 0 },
    };
    const orbMaterial = new ShaderMaterial({
      vertexShader: orbVertex,
      fragmentShader: orbFragment,
      uniforms: orbUniforms,
      glslVersion: GLSL3,
      transparent: true,
      depthTest: false,
      depthWrite: false,
      blending: AdditiveBlending,
    });
    const orbGeometry = new PlaneGeometry(0.55, 0.55);
    const orb = new Mesh(orbGeometry, orbMaterial);
    orb.position.set(0, 0.22, 0.95);
    orb.frustumCulled = false;
    scene.add(orb);

    let animationFrame = 0;
    let lastFrame = performance.now();
    let degraded = false;

    const markDegraded = (reason: DegradedReason, error?: unknown) => {
      if (degraded) return;
      degraded = true;
      if (animationFrame) window.cancelAnimationFrame(animationFrame);
      onDegradedRef.current(reason, error);
    };

    const handleContextLost = (event: Event) => {
      event.preventDefault();
      markDegraded("context_lost");
    };
    renderer.domElement.addEventListener("webglcontextlost", handleContextLost);

    const syncGeometry = () => {
      const current = frameRef.current;
      const dpr = Math.min(current.viewport.dpr, 2);
      renderer.setSize(current.viewport.w, current.viewport.h, false);
      renderer.setPixelRatio(dpr);
      camera.aspect = current.viewport.w / Math.max(current.viewport.h, 1);
      camera.updateProjectionMatrix();
      // Billboard the orb so it always faces the camera.
      orb.lookAt(camera.position);
    };

    const render = (now: number) => {
      if (degraded) return;
      syncGeometry();
      const isActive = activeRef.current ? 1 : 0;
      wavefieldUniforms.uActive.value = isActive;
      orbUniforms.uActive.value = isActive;
      if (activeRef.current) {
        const deltaMs = Math.min(now - lastFrame, 50);
        wavefieldUniforms.uTime.value += deltaMs / 1000;
      }
      lastFrame = now;
      try {
        renderer.render(scene, camera);
      } catch (error) {
        markDegraded("compile_error", error);
        return;
      }
      animationFrame = window.requestAnimationFrame(render);
    };

    syncGeometry();
    animationFrame = window.requestAnimationFrame(render);

    return () => {
      window.cancelAnimationFrame(animationFrame);
      renderer.domElement.removeEventListener("webglcontextlost", handleContextLost);
      scene.remove(wavefield);
      scene.remove(orb);
      wavefieldGeometry.dispose();
      wavefieldMaterial.dispose();
      orbGeometry.dispose();
      orbMaterial.dispose();
      renderer.dispose();
      renderer.domElement.remove();
    };
  }, []);

  return <div ref={hostRef} className="absolute inset-0" />;
}
