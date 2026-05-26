interface CollectShadowRegionsInput {
  readonly dpr: number;
  readonly host: HTMLElement;
}

export interface ShadowRegion {
  readonly featherPx: number;
  readonly falloffPower: number;
  readonly mode: ShadowRegionMode;
  readonly radiusPx: number;
  readonly rectPx: PixelRect;
  readonly strength: number;
}

export type ShadowRegionMode = "inside" | "outside";

interface ViewportRect {
  readonly bottom: number;
  readonly height: number;
  readonly left: number;
  readonly right: number;
  readonly top: number;
  readonly width: number;
}

interface PixelRect {
  readonly height: number;
  readonly width: number;
  readonly x: number;
  readonly y: number;
}

interface HostCoordinateSpace {
  readonly height: number;
  readonly left: number;
  readonly top: number;
  readonly width: number;
}

interface DomShadowPreset {
  readonly featherPx: number;
  readonly mode: ShadowRegionMode;
  readonly paddingX: number;
  readonly paddingY: number;
  readonly radiusScale: number;
  readonly strength: number;
}

export const DOT_SHADOW_MASK_SELECTOR = "[data-wave-shadow], [data-wave-mask]";

const VIEWPORT_BORDER_FEATHER_PX = 3;
const VIEWPORT_BORDER_FALLOFF_POWER = 0.72;
const VIEWPORT_BORDER_INSET_X_PX = 15;
const VIEWPORT_BORDER_INSET_Y_PX = 15;
const VIEWPORT_BORDER_RADIUS_PX = 5;
const VIEWPORT_BORDER_STRENGTH = 1;

const TEXT_SHADOW_PRESET: DomShadowPreset = {
  featherPx: 16,
  mode: "inside",
  paddingX: 3,
  paddingY: 3,
  radiusScale: 0.42,
  strength: 0.42,
};

const LINK_MASK_PRESET: DomShadowPreset = {
  featherPx: 10,
  mode: "inside",
  paddingX: 4,
  paddingY: 3,
  radiusScale: 0.45,
  strength: 0.56,
};

export function collectShadowRegions(input: CollectShadowRegionsInput): readonly ShadowRegion[] {
  if (typeof document === "undefined") return [];

  const hostSpace = resolveHostCoordinateSpace(input.host);
  const regions: ShadowRegion[] = [createViewportBorderRegion(hostSpace, input.dpr)];
  document.querySelectorAll<HTMLElement>(DOT_SHADOW_MASK_SELECTOR).forEach((element) => {
    const preset = element.hasAttribute("data-wave-mask") ? LINK_MASK_PRESET : TEXT_SHADOW_PRESET;
    for (const rect of elementTextRectsToHostRects(element, hostSpace)) {
      const region = createDomShadowRegion(rect, preset, hostSpace, input.dpr);
      if (region !== undefined) regions.push(region);
    }
  });
  return regions;
}

function createViewportBorderRegion(hostSpace: HostCoordinateSpace, dpr: number): ShadowRegion {
  const scale = Math.max(dpr, 1);
  const insetX = Math.min(VIEWPORT_BORDER_INSET_X_PX, hostSpace.width * 0.25);
  const insetY = Math.min(VIEWPORT_BORDER_INSET_Y_PX, hostSpace.height * 0.25);
  const rect = {
    height: Math.max(hostSpace.height - insetY * 2, 1),
    width: Math.max(hostSpace.width - insetX * 2, 1),
    x: insetX,
    y: insetY,
  };

  return {
    featherPx: VIEWPORT_BORDER_FEATHER_PX * scale,
    falloffPower: VIEWPORT_BORDER_FALLOFF_POWER,
    mode: "outside",
    radiusPx: VIEWPORT_BORDER_RADIUS_PX * scale,
    rectPx: {
      height: rect.height * scale,
      width: rect.width * scale,
      x: rect.x * scale,
      y: rect.y * scale,
    },
    strength: VIEWPORT_BORDER_STRENGTH,
  };
}

function elementTextRectsToHostRects(
  element: HTMLElement,
  hostSpace: HostCoordinateSpace,
): readonly ViewportRect[] {
  const style = window.getComputedStyle(element);
  if (style.visibility === "hidden" || style.display === "none" || style.opacity === "0") {
    return [];
  }

  const rects: ViewportRect[] = [];
  const range = document.createRange();
  try {
    range.selectNodeContents(element);
    for (const rect of range.getClientRects()) {
      const clipped = clipRectToHost(domRectToViewportRect(rect), hostSpace);
      if (clipped.width > 0 && clipped.height > 0) rects.push(clipped);
    }
  } finally {
    range.detach();
  }

  if (rects.length > 0) return rects;

  const fallback = clipRectToHost(
    domRectToViewportRect(element.getBoundingClientRect()),
    hostSpace,
  );
  return fallback.width > 0 && fallback.height > 0 ? [fallback] : [];
}

function createDomShadowRegion(
  rect: ViewportRect,
  preset: DomShadowPreset,
  hostSpace: HostCoordinateSpace,
  dpr: number,
): ShadowRegion | undefined {
  const expanded = clipRectToHost(expandViewportRect(rect, preset.paddingX, preset.paddingY), {
    ...hostSpace,
    left: 0,
    top: 0,
  });
  if (expanded.width <= 0 || expanded.height <= 0) return undefined;
  const scale = Math.max(dpr, 1);

  return {
    featherPx: preset.featherPx * scale,
    falloffPower: 1,
    mode: preset.mode,
    radiusPx: Math.min(expanded.width, expanded.height) * preset.radiusScale * scale,
    rectPx: {
      height: expanded.height * scale,
      width: expanded.width * scale,
      x: expanded.left * scale,
      y: (hostSpace.height - expanded.bottom) * scale,
    },
    strength: preset.strength,
  };
}

function expandViewportRect(rect: ViewportRect, x: number, y: number): ViewportRect {
  const left = rect.left - x;
  const right = rect.right + x;
  const top = rect.top - y;
  const bottom = rect.bottom + y;
  return {
    bottom,
    height: bottom - top,
    left,
    right,
    top,
    width: right - left,
  };
}

function domRectToViewportRect(rect: DOMRect): ViewportRect {
  return {
    bottom: rect.bottom,
    height: rect.height,
    left: rect.left,
    right: rect.right,
    top: rect.top,
    width: rect.width,
  };
}

function resolveHostCoordinateSpace(host: HTMLElement): HostCoordinateSpace {
  const bounds = host.getBoundingClientRect();
  return {
    height: Math.max(bounds.height, 1),
    left: bounds.left,
    top: bounds.top,
    width: Math.max(bounds.width, 1),
  };
}

function clipRectToHost(rect: ViewportRect, host: HostCoordinateSpace): ViewportRect {
  const left = clamp(rect.left - host.left, 0, host.width);
  const right = clamp(rect.right - host.left, 0, host.width);
  const top = clamp(rect.top - host.top, 0, host.height);
  const bottom = clamp(rect.bottom - host.top, 0, host.height);

  return {
    bottom,
    height: Math.max(0, bottom - top),
    left,
    right,
    top,
    width: Math.max(0, right - left),
  };
}

function clamp(value: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, value));
}
