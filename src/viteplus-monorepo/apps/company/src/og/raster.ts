import { Resvg, type ResvgRenderOptions } from "@resvg/resvg-js";
import { FRAUNCES_WOFF2_B64, GEIST_WOFF2_B64 } from "virtual:og-fonts";

// SVG→PNG for OG cards. X, Facebook, LinkedIn, Slack, iMessage, and Discord
// all reject SVG card images, so the /og route must emit a raster. resvg
// renders the SVG with the brand faces loaded as buffers (no system Fraunces
// exists on the box); the woff2 bytes are baked into the bundle by the
// `company:og-fonts` plugin. Buffers are decoded once at module load.
const FONT_BUFFERS = [
  Buffer.from(FRAUNCES_WOFF2_B64, "base64"),
  Buffer.from(GEIST_WOFF2_B64, "base64"),
];

// resvg-js 2.6.2 honours font.fontBuffers at runtime (added upstream in 2.4.0)
// but its shipped d.ts omits the field; widen the font type rather than stage
// the woff2 to a temp file just to satisfy the typed `fontFiles` path.
type ResvgFontOptions = NonNullable<ResvgRenderOptions["font"]>;
const font = {
  fontBuffers: FONT_BUFFERS,
  loadSystemFonts: false,
  defaultFontFamily: "Fraunces",
} as ResvgFontOptions;

export function rasterizeOGCard(svg: string): Uint8Array {
  const resvg = new Resvg(svg, { fitTo: { mode: "width", value: 1200 }, font });
  // Uint8Array.from gives an ArrayBuffer-backed view that satisfies BodyInit;
  // a Node Buffer's ArrayBufferLike typing does not.
  return Uint8Array.from(resvg.render().asPng());
}
