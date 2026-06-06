import { Canvas } from "@react-three/fiber";
import { NoToneMapping, SRGBColorSpace } from "three";
import { resolveGuardianGlowRuntime, type GuardianGlowSettings } from "../guardian-glow-config";
import { GuardianComposer } from "../pipeline/GuardianComposer";
import { FpsProbe } from "../telemetry/FpsProbe";
import { GuardianGlowScene } from "./GuardianGlowScene";

type GuardianGlowCanvasProps = {
  readonly settings: GuardianGlowSettings;
};

export function GuardianGlowCanvas({ settings }: GuardianGlowCanvasProps) {
  const runtime = resolveGuardianGlowRuntime(settings);

  return (
    <Canvas
      orthographic
      camera={{ far: 30, near: 0.1, position: [0, 0, 8], zoom: 118 }}
      dpr={runtime.dpr}
      frameloop="always"
      gl={{
        alpha: false,
        antialias: true,
        powerPreference: "high-performance",
        toneMapping: NoToneMapping,
      }}
      onCreated={({ camera, gl }) => {
        camera.layers.enable(0);
        gl.outputColorSpace = SRGBColorSpace;
        gl.setClearColor(0x000000, 1);
      }}
      style={{ height: "100%", pointerEvents: "none", width: "100%" }}
    >
      <FpsProbe />
      <GuardianGlowScene runtime={runtime} settings={settings} />
      <GuardianComposer runtime={runtime} settings={settings} />
    </Canvas>
  );
}
