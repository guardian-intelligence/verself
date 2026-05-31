import {
  resolveLandingHeroWordmarkFontSizeFromContainerPx,
  resolveLandingHeroWordmarkFontSizePx,
} from "./landing-hero-wordmark-spec";

export type WordmarkScreenScaleSource = "landing-hero";

export type WordmarkScreenWidthBasis = "container" | "viewport";

export function resolveWordmarkScreenWidthPx(
  canvasWidthPx: number,
  widthBasis: WordmarkScreenWidthBasis,
): number {
  if (widthBasis === "container") {
    return canvasWidthPx;
  }

  if (typeof window === "undefined") {
    return canvasWidthPx;
  }

  return window.innerWidth;
}

/** Target cap height in CSS px for the active screen-scale source. */
export function resolveWordmarkCapHeightPx(
  widthPx: number,
  source: WordmarkScreenScaleSource,
  widthBasis: WordmarkScreenWidthBasis,
): number {
  if (source !== "landing-hero") {
    return 0;
  }

  return widthBasis === "container"
    ? resolveLandingHeroWordmarkFontSizeFromContainerPx(widthPx)
    : resolveLandingHeroWordmarkFontSizePx(widthPx);
}

/**
 * Linear perspective mapping: CSS cap height → uniform world scale for Text3D
 * authored at `meshCapHeightUnits` (Text3D `size`).
 */
export function resolveWordmarkWorldScale(
  capHeightPx: number,
  canvasHeightPx: number,
  cameraFovDeg: number,
  cameraDistance: number,
  meshCapHeightUnits: number,
  typefaceScreenCalibration: number,
): number {
  const visibleWorldHeight = 2 * Math.tan((cameraFovDeg * Math.PI) / 180 / 2) * cameraDistance;
  const worldCapHeight = (capHeightPx / canvasHeightPx) * visibleWorldHeight;

  return (worldCapHeight / meshCapHeightUnits) * typefaceScreenCalibration;
}
