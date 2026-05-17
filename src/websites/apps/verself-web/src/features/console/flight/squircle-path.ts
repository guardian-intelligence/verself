// Continuous-corner (Apple "squircle") SVG path math.
//
// Vendored VERBATIM from figma-squircle@1.1.0 (MIT, © phamfoo) — the canonical
// port of Figma's reverse-engineered Apple corner-smoothing curve:
//   https://www.figma.com/blog/desperately-seeking-squircles/
//   https://github.com/MartinRGB/Figma_Squircles_Approximation
//
// Vendored (not npm-installed) on purpose: this repo's hardened npm
// supply-chain posture has no agent-side lockfile path, and reviewed pure-math
// source with no install scripts / transitive deps is the sanctioned
// mitigation. The three upstream modules (`distribute.ts`, `draw.ts`,
// `index.ts`) are inlined here unchanged.
//
// The full PER-CORNER distribution path is vendored (it was previously
// dropped while every surface used a uniform radius). The card needs
// independent top vs. bottom radii: an iOS Live Activity is the projection of
// a fixed 3-D corner "wheel" onto the lock-screen, so its top corners (tucked
// under the Dynamic Island) and its free bottom corners ride different points
// on that wheel. `distributeAndNormalize` is exactly the math that lets the
// four corners differ while staying on Apple's continuous-corner superellipse,
// so swiping the card up/down stays seamless without ad-hoc radius math.

interface CornerPathParams {
  a: number;
  b: number;
  c: number;
  d: number;
  p: number;
  cornerRadius: number;
  arcSectionLength: number;
}

interface CornerParams {
  cornerRadius: number;
  cornerSmoothing: number;
  preserveSmoothing: boolean;
  roundingAndSmoothingBudget: number;
}

// ── distribute.ts (verbatim) ────────────────────────────────────────────────

interface RoundedRectangle {
  topLeftCornerRadius: number;
  topRightCornerRadius: number;
  bottomRightCornerRadius: number;
  bottomLeftCornerRadius: number;
  width: number;
  height: number;
}

interface NormalizedCorner {
  radius: number;
  roundingAndSmoothingBudget: number;
}

interface NormalizedCorners {
  topLeft: NormalizedCorner;
  topRight: NormalizedCorner;
  bottomLeft: NormalizedCorner;
  bottomRight: NormalizedCorner;
}

type Corner = keyof NormalizedCorners;

type Side = "top" | "left" | "right" | "bottom";

interface Adjacent {
  side: Side;
  corner: Corner;
}

function distributeAndNormalize({
  topLeftCornerRadius,
  topRightCornerRadius,
  bottomRightCornerRadius,
  bottomLeftCornerRadius,
  width,
  height,
}: RoundedRectangle): NormalizedCorners {
  const roundingAndSmoothingBudgetMap: Record<Corner, number> = {
    topLeft: -1,
    topRight: -1,
    bottomLeft: -1,
    bottomRight: -1,
  };

  const cornerRadiusMap: Record<Corner, number> = {
    topLeft: topLeftCornerRadius,
    topRight: topRightCornerRadius,
    bottomLeft: bottomLeftCornerRadius,
    bottomRight: bottomRightCornerRadius,
  };

  Object.entries(cornerRadiusMap)
    // Let the bigger corners choose first
    .sort(([, radius1], [, radius2]) => {
      return radius2 - radius1;
    })
    .forEach(([cornerName, radius]) => {
      const corner = cornerName as Corner;
      const adjacents = adjacentsByCorner[corner];

      // Look at the 2 adjacent sides, figure out how much space we can have on both sides,
      // then take the smaller one
      const budget = Math.min(
        ...adjacents.map((adjacent) => {
          const adjacentCornerRadius = cornerRadiusMap[adjacent.corner];
          if (radius === 0 && adjacentCornerRadius === 0) {
            return 0;
          }

          const adjacentCornerBudget = roundingAndSmoothingBudgetMap[adjacent.corner];

          const sideLength = adjacent.side === "top" || adjacent.side === "bottom" ? width : height;

          // If the adjacent corner's already been given the rounding and smoothing budget,
          // we'll just take the rest
          if (adjacentCornerBudget >= 0) {
            return sideLength - roundingAndSmoothingBudgetMap[adjacent.corner];
          } else {
            return (radius / (radius + adjacentCornerRadius)) * sideLength;
          }
        }),
      );

      roundingAndSmoothingBudgetMap[corner] = budget;
      cornerRadiusMap[corner] = Math.min(radius, budget);
    });

  return {
    topLeft: {
      radius: cornerRadiusMap.topLeft,
      roundingAndSmoothingBudget: roundingAndSmoothingBudgetMap.topLeft,
    },
    topRight: {
      radius: cornerRadiusMap.topRight,
      roundingAndSmoothingBudget: roundingAndSmoothingBudgetMap.topRight,
    },
    bottomLeft: {
      radius: cornerRadiusMap.bottomLeft,
      roundingAndSmoothingBudget: roundingAndSmoothingBudgetMap.bottomLeft,
    },
    bottomRight: {
      radius: cornerRadiusMap.bottomRight,
      roundingAndSmoothingBudget: roundingAndSmoothingBudgetMap.bottomRight,
    },
  };
}

const adjacentsByCorner: Record<Corner, Array<Adjacent>> = {
  topLeft: [
    { corner: "topRight", side: "top" },
    { corner: "bottomLeft", side: "left" },
  ],
  topRight: [
    { corner: "topLeft", side: "top" },
    { corner: "bottomRight", side: "right" },
  ],
  bottomLeft: [
    { corner: "bottomRight", side: "bottom" },
    { corner: "topLeft", side: "left" },
  ],
  bottomRight: [
    { corner: "bottomLeft", side: "bottom" },
    { corner: "topRight", side: "right" },
  ],
};

// ── draw.ts (verbatim) ──────────────────────────────────────────────────────

function toRadians(degrees: number): number {
  return (degrees * Math.PI) / 180;
}

function getPathParamsForCorner({
  cornerRadius,
  cornerSmoothing,
  preserveSmoothing,
  roundingAndSmoothingBudget,
}: CornerParams): CornerPathParams {
  // From figure 12.2 in the article
  // p = (1 + cornerSmoothing) * q
  // in this case q = R because theta = 90deg
  let p = (1 + cornerSmoothing) * cornerRadius;

  // When there's not enough space left (p > roundingAndSmoothingBudget), there are 2 options:
  //
  // 1. What figma's currently doing: limit the smoothing value to make sure p <= roundingAndSmoothingBudget
  // But what this means is that at some point when cornerRadius is large enough,
  // increasing the smoothing value wouldn't do anything
  //
  // 2. Keep the original smoothing value and use it to calculate the bezier curve normally,
  // then adjust the control points to achieve similar curvature profile
  //
  // preserveSmoothing is a new option I added
  //
  // If preserveSmoothing is on then we'll just keep using the original smoothing value
  // and adjust the bezier curve later
  if (!preserveSmoothing) {
    const maxCornerSmoothing = roundingAndSmoothingBudget / cornerRadius - 1;
    cornerSmoothing = Math.min(cornerSmoothing, maxCornerSmoothing);
    p = Math.min(p, roundingAndSmoothingBudget);
  }

  // In a normal rounded rectangle (cornerSmoothing = 0), this is 90
  // The larger the smoothing, the smaller the arc
  const arcMeasure = 90 * (1 - cornerSmoothing);
  const arcSectionLength = Math.sin(toRadians(arcMeasure / 2)) * cornerRadius * Math.sqrt(2);

  // In the article this is the distance between 2 control points: P3 and P4
  const angleAlpha = (90 - arcMeasure) / 2;
  const p3ToP4Distance = cornerRadius * Math.tan(toRadians(angleAlpha / 2));

  // a, b, c and d are from figure 11.1 in the article
  const angleBeta = 45 * cornerSmoothing;
  const c = p3ToP4Distance * Math.cos(toRadians(angleBeta));
  const d = c * Math.tan(toRadians(angleBeta));

  let b = (p - arcSectionLength - c - d) / 3;
  let a = 2 * b;

  // Adjust the P1 and P2 control points if there's not enough space left
  if (preserveSmoothing && p > roundingAndSmoothingBudget) {
    const p1ToP3MaxDistance = roundingAndSmoothingBudget - d - arcSectionLength - c;

    // Try to maintain some distance between P1 and P2 so the curve wouldn't look weird
    const minA = p1ToP3MaxDistance / 6;
    const maxB = p1ToP3MaxDistance - minA;

    b = Math.min(b, maxB);
    a = p1ToP3MaxDistance - b;
    p = Math.min(p, roundingAndSmoothingBudget);
  }

  return { a, b, c, d, p, arcSectionLength, cornerRadius };
}

interface SVGPathInput {
  width: number;
  height: number;
  topRightPathParams: CornerPathParams;
  bottomRightPathParams: CornerPathParams;
  bottomLeftPathParams: CornerPathParams;
  topLeftPathParams: CornerPathParams;
}

function getSVGPathFromPathParams({
  width,
  height,
  topLeftPathParams,
  topRightPathParams,
  bottomLeftPathParams,
  bottomRightPathParams,
}: SVGPathInput): string {
  return `
    M ${width - topRightPathParams.p} 0
    ${drawTopRightPath(topRightPathParams)}
    L ${width} ${height - bottomRightPathParams.p}
    ${drawBottomRightPath(bottomRightPathParams)}
    L ${bottomLeftPathParams.p} ${height}
    ${drawBottomLeftPath(bottomLeftPathParams)}
    L 0 ${topLeftPathParams.p}
    ${drawTopLeftPath(topLeftPathParams)}
    Z
  `
    .replace(/[\t\s\n]+/g, " ")
    .trim();
}

function drawTopRightPath({ cornerRadius, a, b, c, d, p, arcSectionLength }: CornerPathParams) {
  if (cornerRadius) {
    return rounded`
    c ${a} 0 ${a + b} 0 ${a + b + c} ${d}
    a ${cornerRadius} ${cornerRadius} 0 0 1 ${arcSectionLength} ${arcSectionLength}
    c ${d} ${c}
        ${d} ${b + c}
        ${d} ${a + b + c}`;
  } else {
    return rounded`l ${p} 0`;
  }
}

function drawBottomRightPath({ cornerRadius, a, b, c, d, p, arcSectionLength }: CornerPathParams) {
  if (cornerRadius) {
    return rounded`
    c 0 ${a}
      0 ${a + b}
      ${-d} ${a + b + c}
    a ${cornerRadius} ${cornerRadius} 0 0 1 -${arcSectionLength} ${arcSectionLength}
    c ${-c} ${d}
      ${-(b + c)} ${d}
      ${-(a + b + c)} ${d}`;
  } else {
    return rounded`l 0 ${p}`;
  }
}

function drawBottomLeftPath({ cornerRadius, a, b, c, d, p, arcSectionLength }: CornerPathParams) {
  if (cornerRadius) {
    return rounded`
    c ${-a} 0
      ${-(a + b)} 0
      ${-(a + b + c)} ${-d}
    a ${cornerRadius} ${cornerRadius} 0 0 1 -${arcSectionLength} -${arcSectionLength}
    c ${-d} ${-c}
      ${-d} ${-(b + c)}
      ${-d} ${-(a + b + c)}`;
  } else {
    return rounded`l ${-p} 0`;
  }
}

function drawTopLeftPath({ cornerRadius, a, b, c, d, p, arcSectionLength }: CornerPathParams) {
  if (cornerRadius) {
    return rounded`
    c 0 ${-a}
      0 ${-(a + b)}
      ${d} ${-(a + b + c)}
    a ${cornerRadius} ${cornerRadius} 0 0 1 ${arcSectionLength} -${arcSectionLength}
    c ${c} ${-d}
      ${b + c} ${-d}
      ${a + b + c} ${-d}`;
  } else {
    return rounded`l 0 ${-p}`;
  }
}

function rounded(strings: TemplateStringsArray, ...values: number[]): string {
  return strings.reduce((acc, str, i) => {
    const value = values[i];
    if (typeof value === "number") {
      return acc + str + value.toFixed(4);
    } else {
      return acc + str + (value ?? "");
    }
  }, "");
}

// ── index.ts (verbatim) ─────────────────────────────────────────────────────

export interface FigmaSquircleParams {
  cornerRadius?: number;
  topLeftCornerRadius?: number;
  topRightCornerRadius?: number;
  bottomRightCornerRadius?: number;
  bottomLeftCornerRadius?: number;
  cornerSmoothing: number;
  width: number;
  height: number;
  preserveSmoothing?: boolean;
}

export function getSvgPath({
  cornerRadius = 0,
  topLeftCornerRadius,
  topRightCornerRadius,
  bottomRightCornerRadius,
  bottomLeftCornerRadius,
  cornerSmoothing,
  width,
  height,
  preserveSmoothing = false,
}: FigmaSquircleParams): string {
  topLeftCornerRadius = topLeftCornerRadius ?? cornerRadius;
  topRightCornerRadius = topRightCornerRadius ?? cornerRadius;
  bottomLeftCornerRadius = bottomLeftCornerRadius ?? cornerRadius;
  bottomRightCornerRadius = bottomRightCornerRadius ?? cornerRadius;

  if (
    topLeftCornerRadius === topRightCornerRadius &&
    topRightCornerRadius === bottomRightCornerRadius &&
    bottomRightCornerRadius === bottomLeftCornerRadius &&
    bottomLeftCornerRadius === topLeftCornerRadius
  ) {
    const roundingAndSmoothingBudget = Math.min(width, height) / 2;
    const cornerRadius = Math.min(topLeftCornerRadius, roundingAndSmoothingBudget);

    const pathParams = getPathParamsForCorner({
      cornerRadius,
      cornerSmoothing,
      preserveSmoothing,
      roundingAndSmoothingBudget,
    });

    return getSVGPathFromPathParams({
      width,
      height,
      topLeftPathParams: pathParams,
      topRightPathParams: pathParams,
      bottomLeftPathParams: pathParams,
      bottomRightPathParams: pathParams,
    });
  }

  const { topLeft, topRight, bottomLeft, bottomRight } = distributeAndNormalize({
    topLeftCornerRadius,
    topRightCornerRadius,
    bottomRightCornerRadius,
    bottomLeftCornerRadius,
    width,
    height,
  });

  return getSVGPathFromPathParams({
    width,
    height,
    topLeftPathParams: getPathParamsForCorner({
      cornerSmoothing,
      preserveSmoothing,
      cornerRadius: topLeft.radius,
      roundingAndSmoothingBudget: topLeft.roundingAndSmoothingBudget,
    }),
    topRightPathParams: getPathParamsForCorner({
      cornerSmoothing,
      preserveSmoothing,
      cornerRadius: topRight.radius,
      roundingAndSmoothingBudget: topRight.roundingAndSmoothingBudget,
    }),
    bottomRightPathParams: getPathParamsForCorner({
      cornerSmoothing,
      preserveSmoothing,
      cornerRadius: bottomRight.radius,
      roundingAndSmoothingBudget: bottomRight.roundingAndSmoothingBudget,
    }),
    bottomLeftPathParams: getPathParamsForCorner({
      cornerSmoothing,
      preserveSmoothing,
      cornerRadius: bottomLeft.radius,
      roundingAndSmoothingBudget: bottomLeft.roundingAndSmoothingBudget,
    }),
  });
}
