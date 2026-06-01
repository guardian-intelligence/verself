import { Environment } from "@react-three/drei";
import { VerselfGlassWordmark } from "../../wordmark/verself-glass-wordmark";
import { firstLightFocusPoint } from "./first-light-config";

export function FirstLightWorld() {
  return (
    <>
      <Environment preset="warehouse" />
      <VerselfGlassWordmark
        float
        position={[
          firstLightFocusPoint[0],
          firstLightFocusPoint[1] + 0.42,
          firstLightFocusPoint[2] - 0.8,
        ]}
        rotation={[0.08, -0.24, 0.02]}
        screenWidthBasis="viewport"
      />
    </>
  );
}
