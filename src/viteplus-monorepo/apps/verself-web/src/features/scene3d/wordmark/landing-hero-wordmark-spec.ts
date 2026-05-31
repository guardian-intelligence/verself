// Mirrors the landing hero h1 in marketing-page.tsx so 3D wordmark scale tracks
// the same container-query typography without duplicating magic numbers in R3F.

export const landingIntroContainerSpec = {
  maxWidthRem: 32,
  widthVw: 86,
} as const;

export const landingHeroWordmarkSpec = {
  fontSizeCqw: 10.2,
} as const;

export function resolveLandingIntroContainerWidthPx(viewportWidthPx: number): number {
  const vwWidth = viewportWidthPx * (landingIntroContainerSpec.widthVw / 100);
  const maxWidthPx = landingIntroContainerSpec.maxWidthRem * 16;
  return Math.min(vwWidth, maxWidthPx);
}

/** CSS font-size in px when the intro container width is already known. */
export function resolveLandingHeroWordmarkFontSizeFromContainerPx(
  containerWidthPx: number,
): number {
  return containerWidthPx * (landingHeroWordmarkSpec.fontSizeCqw / 100);
}

/** CSS font-size in px for the landing hero "Verself" h1 at a given viewport width. */
export function resolveLandingHeroWordmarkFontSizePx(viewportWidthPx: number): number {
  return resolveLandingHeroWordmarkFontSizeFromContainerPx(
    resolveLandingIntroContainerWidthPx(viewportWidthPx),
  );
}
