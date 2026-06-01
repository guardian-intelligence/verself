import { Environment } from "@react-three/drei";
import { VerselfGlassWordmark } from "../../wordmark/verself-glass-wordmark";
import type { WordmarkScreenWidthBasis } from "../../wordmark/wordmark-screen-metrics";
import { LockedCamera } from "../wordmark-lab/LockedCamera";
import { wordmarkLabPlacement } from "../wordmark-lab/wordmark-lab-config";

type HeroWordmarkSceneProps = {
  readonly float?: boolean;
  readonly screenWidthBasis?: WordmarkScreenWidthBasis;
};

export function HeroWordmarkScene({
  float = false,
  screenWidthBasis = "container",
}: HeroWordmarkSceneProps) {
  return (
    <>
      <LockedCamera />
      <ambientLight color="#8ea8d8" intensity={0.12} />
      <directionalLight
        castShadow
        color="#fff0d8"
        intensity={2.4}
        position={[2.4, 3.8, 4.6]}
        shadow-bias={-0.00012}
        shadow-mapSize={[1024, 1024]}
      />
      <directionalLight color="#6f8ec8" intensity={0.55} position={[-2.8, 1.2, 3.2]} />
      <pointLight
        color="#ffb878"
        decay={2}
        distance={12}
        intensity={3.2}
        position={[1.8, 2.8, 4.8]}
      />
      <Environment preset="warehouse" />
      <VerselfGlassWordmark
        {...wordmarkLabPlacement}
        float={float}
        screenWidthBasis={screenWidthBasis}
      />
    </>
  );
}
