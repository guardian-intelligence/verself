import { expect, test } from "@playwright/test";

test("landing renders the optical plate without body copy", async ({ page }) => {
  await page.goto("/?visual-test=1");

  await expect(page.getByRole("banner")).toBeVisible();
  await expect(page.locator("main h1")).toHaveCount(0);
  await expect(page.locator("main p")).toHaveCount(0);
  await expect(page.locator("footer")).toHaveCount(0);
  await expect(page.locator("[data-firstlight-fallback]")).toHaveCount(0);

  const canvas = page.locator("canvas");
  await expect(canvas).toHaveCount(1);
  await expect(canvas).toBeVisible();
  await page.waitForTimeout(500);

  const metrics = await page.evaluate(() => {
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
    let maxLuma = 0;
    let risingDiagonalEnergy = 0;
    let fallingDiagonalEnergy = 0;
    let warmCoolSplit = 0;
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
        const nx = x / Math.max(1, w - 1);
        const ny = y / Math.max(1, h - 1);
        const risingDiagonal = Math.abs(nx + ny - 1);
        const fallingDiagonal = Math.abs(nx - ny);
        if (risingDiagonal < 0.1) risingDiagonalEnergy += luma;
        if (fallingDiagonal < 0.1) fallingDiagonalEnergy += luma;
        if (luma > 54) warmCoolSplit += Math.abs(r - b);
      }
    }

    return {
      width: w,
      height: h,
      lit,
      maxLuma,
      risingDiagonalEnergy,
      fallingDiagonalEnergy,
      warmCoolSplit,
    };
  });

  expect(metrics.width).toBeGreaterThan(300);
  expect(metrics.height).toBeGreaterThan(300);
  expect(metrics.lit).toBeGreaterThan(240);
  expect(metrics.maxLuma).toBeGreaterThan(128);
  const dominantDiagonal = Math.max(metrics.risingDiagonalEnergy, metrics.fallingDiagonalEnergy);
  const secondaryDiagonal = Math.min(metrics.risingDiagonalEnergy, metrics.fallingDiagonalEnergy);
  expect(dominantDiagonal).toBeGreaterThan(secondaryDiagonal * 1.2);
  expect(metrics.warmCoolSplit).toBeGreaterThan(2_000);
});
