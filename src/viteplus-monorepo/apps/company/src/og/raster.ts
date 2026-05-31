import { mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { Resvg, type ResvgRenderOptions } from "@resvg/resvg-js";
import { FRAUNCES_B64, GEIST_B64, LETTERS_BODY_B64, LETTERS_BODY_FILE } from "virtual:og-fonts";

// SVG→PNG for OG cards. X, Facebook, LinkedIn, Slack, iMessage, and Discord
// all reject SVG card images, so the /og route must emit a raster.
//
// resvg needs the brand faces (no system fonts on the box). The bytes are baked
// into the bundle as base64 by the `og-fonts` plugin, then staged to a temp dir
// and loaded by PATH. Footgun: resvg-js 2.6.2's `fontBuffers` option silently
// drops every face past the first couple (so the card body font never loaded);
// `fontFiles` is the reliable path. Done once at module load.
const FONT_DIR = mkdtempSync(join(tmpdir(), "verself-og-fonts-"));
function stage(name: string, b64: string): string {
  const path = join(FONT_DIR, name);
  writeFileSync(path, Buffer.from(b64, "base64"));
  return path;
}
const FONT_FILES = [
  stage("fraunces.woff2", FRAUNCES_B64),
  stage("geist.ttf", GEIST_B64),
  stage(LETTERS_BODY_FILE, LETTERS_BODY_B64),
];

type ResvgFontOptions = NonNullable<ResvgRenderOptions["font"]>;
const font = {
  fontFiles: FONT_FILES,
  loadSystemFonts: false,
  defaultFontFamily: "Fraunces",
} satisfies ResvgFontOptions;

export function rasterizeOGCard(svg: string): Uint8Array {
  const resvg = new Resvg(svg, { fitTo: { mode: "width", value: 1200 }, font });
  // Uint8Array.from gives an ArrayBuffer-backed view that satisfies BodyInit;
  // a Node Buffer's ArrayBufferLike typing does not.
  return Uint8Array.from(resvg.render().asPng());
}
