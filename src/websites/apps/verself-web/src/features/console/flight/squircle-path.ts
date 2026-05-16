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
// mitigation. The algorithm is unchanged; only the per-corner distribution
// path is dropped because every surface here uses a uniform radius.

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

function toRadians(degrees: number): number {
  return (degrees * Math.PI) / 180;
}

function getPathParamsForCorner({
  cornerRadius,
  cornerSmoothing,
  preserveSmoothing,
  roundingAndSmoothingBudget,
}: CornerParams): CornerPathParams {
  let p = (1 + cornerSmoothing) * cornerRadius;

  if (!preserveSmoothing) {
    const maxCornerSmoothing = roundingAndSmoothingBudget / cornerRadius - 1;
    cornerSmoothing = Math.min(cornerSmoothing, maxCornerSmoothing);
    p = Math.min(p, roundingAndSmoothingBudget);
  }

  const arcMeasure = 90 * (1 - cornerSmoothing);
  const arcSectionLength = Math.sin(toRadians(arcMeasure / 2)) * cornerRadius * Math.sqrt(2);

  const angleAlpha = (90 - arcMeasure) / 2;
  const p3ToP4Distance = cornerRadius * Math.tan(toRadians(angleAlpha / 2));

  const angleBeta = 45 * cornerSmoothing;
  const c = p3ToP4Distance * Math.cos(toRadians(angleBeta));
  const d = c * Math.tan(toRadians(angleBeta));

  let b = (p - arcSectionLength - c - d) / 3;
  let a = 2 * b;

  if (preserveSmoothing && p > roundingAndSmoothingBudget) {
    const p1ToP3MaxDistance = roundingAndSmoothingBudget - d - arcSectionLength - c;
    const minA = p1ToP3MaxDistance / 6;
    const maxB = p1ToP3MaxDistance - minA;
    b = Math.min(b, maxB);
    a = p1ToP3MaxDistance - b;
    p = Math.min(p, roundingAndSmoothingBudget);
  }

  return { a, b, c, d, p, arcSectionLength, cornerRadius };
}

function rounded(strings: TemplateStringsArray, ...values: number[]): string {
  return strings.reduce((acc, str, i) => {
    const value = values[i];
    if (typeof value === "number") return acc + str + value.toFixed(4);
    return acc + str + (value ?? "");
  }, "");
}

function drawTopRightPath({ cornerRadius, a, b, c, d, p, arcSectionLength }: CornerPathParams) {
  if (cornerRadius) {
    return rounded`
    c ${a} 0 ${a + b} 0 ${a + b + c} ${d}
    a ${cornerRadius} ${cornerRadius} 0 0 1 ${arcSectionLength} ${arcSectionLength}
    c ${d} ${c}
        ${d} ${b + c}
        ${d} ${a + b + c}`;
  }
  return rounded`l ${p} 0`;
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
  }
  return rounded`l 0 ${p}`;
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
  }
  return rounded`l ${-p} 0`;
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
  }
  return rounded`l 0 ${-p}`;
}

// Uniform-radius squircle path in the element's px box. Equivalent to
// figma-squircle getSvgPath() with all four corner radii equal.
export function squircleSvgPath({
  width,
  height,
  cornerRadius,
  cornerSmoothing,
  preserveSmoothing = false,
}: {
  width: number;
  height: number;
  cornerRadius: number;
  cornerSmoothing: number;
  preserveSmoothing?: boolean;
}): string {
  const roundingAndSmoothingBudget = Math.min(width, height) / 2;
  const r = Math.min(cornerRadius, roundingAndSmoothingBudget);
  const c = getPathParamsForCorner({
    cornerRadius: r,
    cornerSmoothing,
    preserveSmoothing,
    roundingAndSmoothingBudget,
  });
  return `
    M ${width - c.p} 0
    ${drawTopRightPath(c)}
    L ${width} ${height - c.p}
    ${drawBottomRightPath(c)}
    L ${c.p} ${height}
    ${drawBottomLeftPath(c)}
    L 0 ${c.p}
    ${drawTopLeftPath(c)}
    Z
  `
    .replace(/[\t\s\n]+/g, " ")
    .trim();
}
