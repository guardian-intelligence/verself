import { expect, test, type Page } from "@playwright/test";

interface CanvasMetrics {
  readonly width: number;
  readonly height: number;
  readonly lit: number;
  readonly dark: number;
  readonly maxLuma: number;
  readonly risingDiagonalEnergy: number;
  readonly fallingDiagonalEnergy: number;
  readonly warmCoolSplit: number;
  readonly colorSpread: number;
}

async function readCanvasMetrics(page: Page): Promise<CanvasMetrics> {
  const canvas = page.locator("canvas");
  await expect(canvas).toHaveCount(1);
  await expect(canvas).toBeVisible();
  await page.waitForTimeout(500);

  return page.evaluate(() => {
    const canvas = document.querySelector("canvas");
    if (!(canvas instanceof HTMLCanvasElement)) {
      throw new Error("landing canvas missing");
    }
    const gl = canvas.getContext("webgl2");
    if (!gl) {
      throw new Error("landing canvas is not WebGL2");
    }
    const w = gl.drawingBufferWidth;
    const h = gl.drawingBufferHeight;
    const pixels = new Uint8Array(w * h * 4);
    gl.readPixels(0, 0, w, h, gl.RGBA, gl.UNSIGNED_BYTE, pixels);

    let lit = 0;
    let dark = 0;
    let maxLuma = 0;
    let risingDiagonalEnergy = 0;
    let fallingDiagonalEnergy = 0;
    let warmCoolSplit = 0;
    let colorSpread = 0;
    const stride = 5;
    for (let y = 0; y < h; y += stride) {
      for (let x = 0; x < w; x += stride) {
        const offset = (y * w + x) * 4;
        const r = pixels[offset] ?? 0;
        const g = pixels[offset + 1] ?? 0;
        const b = pixels[offset + 2] ?? 0;
        const luma = 0.2126 * r + 0.7152 * g + 0.0722 * b;
        maxLuma = Math.max(maxLuma, luma);
        if (luma > 18) lit += 1;
        if (luma < 7) dark += 1;
        const nx = x / Math.max(1, w - 1);
        const ny = y / Math.max(1, h - 1);
        const risingDiagonal = Math.abs(nx + ny - 1);
        const fallingDiagonal = Math.abs(nx - ny);
        if (risingDiagonal < 0.1) risingDiagonalEnergy += luma;
        if (fallingDiagonal < 0.1) fallingDiagonalEnergy += luma;
        if (luma > 54) warmCoolSplit += Math.abs(r - b);
        colorSpread += Math.max(r, g, b) - Math.min(r, g, b);
      }
    }

    return {
      width: w,
      height: h,
      lit,
      dark,
      maxLuma,
      risingDiagonalEnergy,
      fallingDiagonalEnergy,
      warmCoolSplit,
      colorSpread,
    };
  });
}

test("landing renders the optical plate without body copy", async ({ page }) => {
  await page.goto("/?visual-test=1");

  await expect(page.getByRole("banner")).toBeVisible();
  await expect(page.locator("main h1")).toHaveCount(0);
  await expect(page.locator("main p")).toHaveCount(0);
  await expect(page.locator("footer")).toHaveCount(0);
  await expect(page.locator("[data-firstlight-fallback]")).toHaveCount(0);

  const metrics = await readCanvasMetrics(page);

  expect(metrics.width).toBeGreaterThan(300);
  expect(metrics.height).toBeGreaterThan(300);
  expect(metrics.lit).toBeGreaterThan(240);
  expect(metrics.maxLuma).toBeGreaterThan(128);
  const dominantDiagonal = Math.max(metrics.risingDiagonalEnergy, metrics.fallingDiagonalEnergy);
  const secondaryDiagonal = Math.min(metrics.risingDiagonalEnergy, metrics.fallingDiagonalEnergy);
  expect(dominantDiagonal).toBeGreaterThan(secondaryDiagonal * 1.08);
  expect(metrics.warmCoolSplit).toBeGreaterThan(2_000);
});

test("landing exposes deterministic celestial compositor debug modes", async ({ page }) => {
  await page.goto("/?visual-test=shape");
  const shape = await readCanvasMetrics(page);
  expect(shape.dark).toBeGreaterThan(120);
  expect(shape.lit).toBeGreaterThan(220);
  expect(shape.colorSpread).toBeGreaterThan(8_000);

  await page.goto("/?visual-test=lens");
  const lens = await readCanvasMetrics(page);
  expect(lens.lit).toBeGreaterThan(180);
  expect(lens.maxLuma).toBeGreaterThan(36);
  expect(lens.colorSpread).toBeGreaterThan(shape.colorSpread * 0.45);

  await page.goto("/?visual-test=disk");
  const disk = await readCanvasMetrics(page);
  expect(disk.dark).toBeGreaterThan(120);
  expect(disk.lit).toBeGreaterThan(90);
  expect(disk.warmCoolSplit).toBeGreaterThan(1_000);
});
