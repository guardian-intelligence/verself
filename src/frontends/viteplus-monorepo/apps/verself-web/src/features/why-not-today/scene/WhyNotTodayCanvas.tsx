import { useEffect, useRef } from "react";
import {
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
uniform float uReveal;

out float vH;
out vec2 vP;
out vec2 vQ;

float vTerrain(vec2 p, float t) {
  // Plane is parameterized in (-1..1, -1..1). After the mesh's -PI/2 X
  // rotation, local +y maps to world -z (away from camera) and local -y
  // maps to world +z (closest to the camera). The trough opens toward the
  // camera: deepest at the back (local y = +1), wider and shallower as y
  // decreases toward the foreground.
  float zNorm = clamp((1.0 - p.y) * 0.5, 0.0, 1.0);

  // Single soft trough — a wide quadratic basin rather than two sharp
  // Gaussians. Width grows as the trough comes forward, so its silhouette
  // reads as a single converging valley instead of a double-V notch.
  float width = mix(0.32, 1.20, pow(zNorm, 0.55));
  float depth = mix(0.95, 0.45, zNorm);
  float trough = -depth * exp(-pow(p.x / width, 2.0));

  // Rolling terrain that scrolls toward the camera (decreasing local y over
  // time), so the surface reads as a flyover rather than waves pulsing in
  // place. Multi-octave layered sines stay cheap and tile cleanly.
  vec2 q = vec2(p.x, p.y + t * 0.32);
  float terrain =
      0.18 * sin(q.x * 3.1 + q.y * 1.9)
    + 0.10 * sin(q.x * 6.4 - q.y * 3.7 + 1.3)
    + 0.05 * sin(q.x * 12.0 + q.y * 7.5 - 0.6);

  // Suppress terrain inside the trough so the central valley stays clean.
  float troughMask = exp(-pow(p.x / (width * 1.25), 2.0));
  terrain *= (1.0 - troughMask * 0.75);

  return trough + terrain;
}

void main() {
  vec2 p = position.xy;
  float t = uTime * uMotion;
  float h = vTerrain(p, t);
  vH = h * uReveal;
  vP = p;
  // Scrolled coordinate exposed to the fragment shader so contour stripes
  // running across the direction of motion advance with the terrain.
  vQ = vec2(p.x, p.y + t * 0.32);

  float revealH = h * uReveal;
  gl_Position = projectionMatrix * modelViewMatrix * vec4(position.x, position.y, revealH, 1.0);
}
`;

const wavefieldFragment = /* glsl */ `
precision highp float;
in float vH;
in vec2 vP;
in vec2 vQ;
uniform float uActive;
uniform float uReveal;
out vec4 fragColor;

void main() {
  float depthRaw = clamp(-vH, 0.0, 2.0);
  float depthN = smoothstep(0.0, 1.3, depthRaw);
  vec3 violet = vec3(0.42, 0.30, 0.95);
  vec3 cyan   = vec3(0.30, 0.78, 1.10);
  vec3 base   = mix(violet, cyan, depthN);

  // Iso-height contour lines: bands of constant elevation. Around the
  // single trough they curl into concentric rings naturally; out on the
  // flanking terrain they trace the rolling slopes.
  float contourPhase = vH * 7.0;
  float contour = abs(fract(contourPhase) - 0.5);
  float contourLine = 1.0 - smoothstep(0.0, 0.08, contour);

  // Lateral grid: lines parallel to the direction of motion. Anchored to
  // local x so they don't slide as terrain scrolls, giving the eye a
  // stationary reference frame.
  float xMod = fract(vP.x * 12.0);
  float xBand = smoothstep(0.05, 0.0, abs(xMod - 0.5));

  // Transverse grid: lines perpendicular to motion, scrolling in vQ so the
  // grid sweeps toward the camera and reinforces the flyover feel.
  float yMod = fract(vQ.y * 5.5);
  float yBand = smoothstep(0.05, 0.0, abs(yMod - 0.5));

  vec3 col = base * (0.18 + contourLine * 1.30 + xBand * 0.45 + yBand * 0.40);
  col += cyan * smoothstep(0.55, 1.4, depthRaw) * 0.45;

  // Vignette: full intensity at the front (bottom of screen, vP.y = -1) and
  // fading to black at the back (top of screen, vP.y = +1) so the trough
  // dissolves into the horizon instead of cutting off at the plane edge.
  float vy = 1.0 - smoothstep(-0.30, 1.05, vP.y);
  float vx = smoothstep(2.20, 0.10, abs(vP.x));
  float vignette = vx * vy;

  // Grow-in curtain — wipes from the front of the plane (bottom of screen)
  // toward the back as uReveal advances, so the surface appears to rise
  // into frame from below rather than fade in flat.
  float curtainEdge = mix(-1.4, 1.4, uReveal);
  float curtain = 1.0 - smoothstep(curtainEdge - 0.10, curtainEdge + 0.10, vP.y);

  float alpha = uActive * vignette * curtain *
                clamp(0.28 + contourLine * 0.95 + depthN * 0.40, 0.0, 1.0);
  fragColor = vec4(col, alpha);
}
`;

const REVEAL_DURATION_MS = 1600;

function easeOutCubic(t: number): number {
  const clamped = Math.min(Math.max(t, 0), 1);
  return 1 - Math.pow(1 - clamped, 3);
}

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
    // Camera tilt is the dominant lever for where the trough's converging
    // apex lands in screen space. Pitched downward enough that the apex
    // falls in the upper third of the viewport (just under the headline),
    // and the front of the trough fills the bottom of the viewport.
    const camera = new PerspectiveCamera(54, 1, 0.1, 50);
    camera.position.set(0, 1.45, 3.4);
    camera.lookAt(0, -1.85, -1.4);

    const wavefieldUniforms = {
      uTime: { value: 0 },
      uActive: { value: activeRef.current ? 1 : 0 },
      uMotion: { value: 1 },
      uReveal: { value: 0 },
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

    let animationFrame = 0;
    let lastFrame = performance.now();
    const startedAt = performance.now();
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
    };

    const render = (now: number) => {
      if (degraded) return;
      syncGeometry();
      const isActive = activeRef.current ? 1 : 0;
      wavefieldUniforms.uActive.value = isActive;
      wavefieldUniforms.uReveal.value = easeOutCubic((now - startedAt) / REVEAL_DURATION_MS);
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
      wavefieldGeometry.dispose();
      wavefieldMaterial.dispose();
      renderer.dispose();
      renderer.domElement.remove();
    };
  }, []);

  return <div ref={hostRef} className="absolute inset-0" />;
}
