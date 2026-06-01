import { useFrame } from "@react-three/fiber";
import { useMemo, useRef, type ReactNode } from "react";
import { Group, MathUtils, PerspectiveCamera, Vector3 } from "three";
import { WORDMARK_CANONICAL_SIZE, wordmarkTypefaceScreenCalibration } from "./wordmark-config";
import {
  resolveWordmarkCapHeightPx,
  resolveWordmarkScreenWidthPx,
  type WordmarkScreenScaleSource,
  type WordmarkScreenWidthBasis,
} from "./wordmark-screen-metrics";

type WordmarkViewportScaleProps = {
  readonly children: ReactNode;
  readonly source?: WordmarkScreenScaleSource;
  readonly viewportWidthPx?: number;
  readonly widthBasis?: WordmarkScreenWidthBasis;
};

export function WordmarkViewportScale({
  children,
  source = "landing-hero",
  viewportWidthPx,
  widthBasis = "container",
}: WordmarkViewportScaleProps) {
  const root = useRef<Group>(null);
  const anchor = useMemo(() => new Vector3(), []);

  useFrame(({ camera, size }) => {
    if (!root.current || !(camera instanceof PerspectiveCamera)) {
      return;
    }

    const canvasWidthPx = size.width;
    const widthPx = viewportWidthPx ?? resolveWordmarkScreenWidthPx(canvasWidthPx, widthBasis);
    const capHeightPx = resolveWordmarkCapHeightPx(widthPx, source, widthBasis);

    root.current.getWorldPosition(anchor);
    const distance = camera.position.distanceTo(anchor);
    const visibleWorldHeight = 2 * Math.tan(MathUtils.degToRad(camera.fov / 2)) * distance;
    const worldCapHeight = (capHeightPx / size.height) * visibleWorldHeight;
    const scalar = (worldCapHeight / WORDMARK_CANONICAL_SIZE) * wordmarkTypefaceScreenCalibration;

    root.current.scale.setScalar(scalar);
  });

  return <group ref={root}>{children}</group>;
}
