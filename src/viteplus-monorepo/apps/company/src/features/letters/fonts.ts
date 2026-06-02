// Single source of truth for Letters typography.
//
// Change the reading face here and it propagates to everything that needs it:
//   - @font-face rules + font variables → lettersTypographyCss(), inlined into
//     the letters critical CSS by routes/letters/route.tsx
//   - the OG card family + baked bytes   → og/template.ts, vite `og-fonts`
//     plugin, og/raster.ts
//   - the dispatch signature             → routes/letters/$slug.tsx
//
// woff2 files live in public/fonts (self-hosted; CSP is font-src 'self').

export interface LetterFontFace {
  readonly file: string;
  readonly style: "normal" | "italic";
  // CSS font-weight descriptor: a range like "400 700" for a variable face, or
  // a single value like "500" for a static instance.
  readonly weight: string;
  readonly variable: boolean;
}

export interface LettersFont {
  readonly family: string;
  readonly stack: string;
  readonly weight: number; // body copy weight
  readonly bodySize: string; // body copy font-size (a CSS length / clamp)
  readonly ogFile: string; // upright face baked into the OG rasteriser
  readonly faces: readonly LetterFontFace[];
}

// The reading face. Crimson Pro runs small on the body, so it takes a larger
// measure than a typical text face would.
export const lettersBodyFont: LettersFont = {
  family: "Crimson Pro",
  stack: "'Crimson Pro', Georgia, serif",
  weight: 500,
  bodySize: "clamp(20px, 1.5vw, 22px)",
  ogFile: "CrimsonPro-OG.ttf",
  faces: [
    { file: "CrimsonPro-Variable.woff2", style: "normal", weight: "400 700", variable: true },
    {
      file: "CrimsonPro-Italic-Variable.woff2",
      style: "italic",
      weight: "400 700",
      variable: true,
    },
  ],
};

// The date hand. The dispatch sign-off itself is the traced SVG in
// handwritten-signature.tsx; this face stays for the handwritten date.
export const lettersSignatureFont = {
  family: "Pinyon Script",
  stack: "'Pinyon Script', cursive",
  faces: [
    { file: "PinyonScript-Regular.woff2", style: "normal", weight: "400", variable: false },
  ] satisfies readonly LetterFontFace[],
} as const;

function faceCss(family: string, f: LetterFontFace): string {
  const format = f.variable ? "woff2-variations" : "woff2";
  return `@font-face{font-family:"${family}";src:url("/fonts/${f.file}") format("${format}");font-weight:${f.weight};font-style:${f.style};font-display:swap;}`;
}

// CSS for the letters treatment: @font-face rules, the font variables the shell
// + typography read, and the body size/weight. Concatenated into the inlined
// critical CSS so first paint already has the face.
export function lettersTypographyCss(): string {
  const faces = [
    ...lettersBodyFont.faces.map((f) => faceCss(lettersBodyFont.family, f)),
    ...lettersSignatureFont.faces.map((f) => faceCss(lettersSignatureFont.family, f)),
  ].join("");
  const vars =
    `[data-treatment="letters"]{` +
    `--font-display:${lettersBodyFont.stack};` +
    `--treatment-display-font:${lettersBodyFont.stack};` +
    `--treatment-body-font:${lettersBodyFont.stack};` +
    `--letters-body-weight:${lettersBodyFont.weight};` +
    `--letters-body-size:${lettersBodyFont.bodySize};}`;
  // Body copy carries the configured size + weight; the salutation/date keep
  // their own. Outranks the prose utilities (unlayered beats Tailwind layers).
  const body =
    `[data-treatment="letters"] [data-letter-body] p,` +
    `[data-treatment="letters"] [data-letter-body] li,` +
    `[data-treatment="letters"] [data-letter-body] blockquote` +
    `{font-weight:var(--letters-body-weight);font-size:var(--letters-body-size);}` +
    // The index preview is the same measure as the body it opens into.
    `[data-treatment="letters"] [data-letter-slot="body"]{font-size:var(--letters-body-size);}`;
  return faces + vars + body;
}
