import { Canvas } from "@react-three/fiber";
import { NoToneMapping } from "three";
import { LandingSpotlightScene } from "./LandingSpotlightScene";

export function LandingSpotlightCanvas() {
  return (
    <Canvas
      orthographic
      camera={{ far: 10, near: 0.1, position: [0, 0, 1], zoom: 1 }}
      dpr={[1, 1.5]}
      frameloop="always"
      gl={{
        alpha: true,
        antialias: false,
        powerPreference: "high-performance",
        toneMapping: NoToneMapping,
      }}
      onCreated={({ gl }) => {
        gl.setClearColor(0x000000, 0);
      }}
      style={{ height: "100%", pointerEvents: "none", width: "100%" }}
    >
      <LandingSpotlightScene />
    </Canvas>
  );
}
