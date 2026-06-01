import { Caustics, Center, Float, Text3D } from "@react-three/drei";
import type { ThreeElements } from "@react-three/fiber";
import { GlassTransmissionMaterial } from "./glass-transmission-material";
import {
  WORDMARK_TEXT,
  wordmarkCaustics,
  wordmarkGeometry,
  wordmarkTypeface,
} from "./wordmark-config";
import { WordmarkViewportScale } from "./wordmark-viewport-scale";
import type {
  WordmarkScreenScaleSource,
  WordmarkScreenWidthBasis,
} from "./wordmark-screen-metrics";

type VerselfGlassWordmarkProps = ThreeElements["group"] & {
  readonly float?: boolean;
  readonly screenScale?: WordmarkScreenScaleSource | false;
  readonly screenWidthBasis?: WordmarkScreenWidthBasis;
};

export function VerselfGlassWordmark({
  float = false,
  screenScale = "landing-hero",
  screenWidthBasis = "container",
  ...groupProps
}: VerselfGlassWordmarkProps) {
  const wordmark = (
    <Center disableY top>
      <Text3D {...wordmarkGeometry} castShadow font={wordmarkTypeface} receiveShadow>
        {WORDMARK_TEXT}
        <GlassTransmissionMaterial />
      </Text3D>
    </Center>
  );

  const body = (
    <group {...groupProps}>
      <Caustics {...wordmarkCaustics} causticsOnly={false}>
        {float ? (
          <Float floatIntensity={0.22} rotationIntensity={0.08} speed={1.4}>
            {wordmark}
          </Float>
        ) : (
          wordmark
        )}
      </Caustics>
    </group>
  );

  if (screenScale === false) {
    return body;
  }

  return (
    <WordmarkViewportScale source={screenScale} widthBasis={screenWidthBasis}>
      {body}
    </WordmarkViewportScale>
  );
}
