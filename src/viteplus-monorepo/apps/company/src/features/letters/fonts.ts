// Single source of truth for Letters typography.
//
// One config drives everything that needs the face:
//   - @font-face rules + font variables → lettersTypographyCss(), inlined into
//     the letters critical CSS by routes/letters/route.tsx
//   - per-request font selection         → ?font=<id>, resolveLettersFont() +
//     lettersFontVars() applied to the treatment root in route.tsx
//   - the OG card family + baked bytes    → og/template.ts, vite `og-fonts`
//     plugin, og/raster.ts (always the DEFAULT face — crawlers send no param)
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
  readonly id: string; // ?font=<id>
  readonly label: string;
  readonly family: string;
  readonly stack: string;
  readonly weight: number; // body copy weight
  readonly bodySize: string; // body copy font-size (a CSS length / clamp)
  readonly ogFile: string; // upright face baked into the OG rasteriser
  readonly faces: readonly LetterFontFace[];
}

// Body sizes. Old-style faces (Crimson, Garamond) render small on the body, so
// they take a larger measure to match the others' apparent size.
const BODY_SIZE = "clamp(19px, 1.4vw, 20px)";
const BODY_SIZE_LARGE = "clamp(20px, 1.5vw, 22px)";

const CRIMSON: LettersFont = {
  id: "crimson",
  label: "Crimson Pro",
  family: "Crimson Pro",
  stack: "'Crimson Pro', Georgia, serif",
  weight: 500,
  bodySize: BODY_SIZE_LARGE,
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

// Selectable reading faces. CRIMSON is the default (?font absent/unknown, and
// the OG card). Trial faces below ship an upright 500 only; promote a face to a
// full family (italic + weights) before making it the default.
export const lettersBodyFontOptions: readonly LettersFont[] = [
  CRIMSON,
  {
    id: "lora",
    label: "Lora",
    family: "Lora",
    stack: "'Lora', Georgia, serif",
    weight: 500,
    bodySize: BODY_SIZE,
    ogFile: "Lora-Variable.woff2",
    faces: [
      { file: "Lora-Variable.woff2", style: "normal", weight: "400 700", variable: true },
      { file: "Lora-Italic-Variable.woff2", style: "italic", weight: "400 700", variable: true },
    ],
  },
  {
    id: "newsreader",
    label: "Newsreader",
    family: "Newsreader",
    stack: "'Newsreader', Georgia, serif",
    weight: 500,
    bodySize: BODY_SIZE,
    ogFile: "Newsreader-500.woff2",
    faces: [{ file: "Newsreader-500.woff2", style: "normal", weight: "500", variable: false }],
  },
  {
    id: "source-serif",
    label: "Source Serif 4",
    family: "Source Serif 4",
    stack: "'Source Serif 4', Georgia, serif",
    weight: 500,
    bodySize: BODY_SIZE,
    ogFile: "SourceSerif4-500.woff2",
    faces: [{ file: "SourceSerif4-500.woff2", style: "normal", weight: "500", variable: false }],
  },
  {
    id: "spectral",
    label: "Spectral",
    family: "Spectral",
    stack: "'Spectral', Georgia, serif",
    weight: 500,
    bodySize: BODY_SIZE,
    ogFile: "Spectral-500.woff2",
    faces: [{ file: "Spectral-500.woff2", style: "normal", weight: "500", variable: false }],
  },
  {
    id: "eb-garamond",
    label: "EB Garamond",
    family: "EB Garamond",
    stack: "'EB Garamond', Georgia, serif",
    weight: 500,
    bodySize: BODY_SIZE_LARGE,
    ogFile: "EBGaramond-500.woff2",
    faces: [{ file: "EBGaramond-500.woff2", style: "normal", weight: "500", variable: false }],
  },
  {
    id: "literata",
    label: "Literata",
    family: "Literata",
    stack: "'Literata', Georgia, serif",
    weight: 500,
    bodySize: BODY_SIZE,
    ogFile: "Literata-500.woff2",
    faces: [{ file: "Literata-500.woff2", style: "normal", weight: "500", variable: false }],
  },
  {
    id: "bitter",
    label: "Bitter",
    family: "Bitter",
    stack: "'Bitter', Georgia, serif",
    weight: 500,
    bodySize: BODY_SIZE,
    ogFile: "Bitter-500.woff2",
    faces: [{ file: "Bitter-500.woff2", style: "normal", weight: "500", variable: false }],
  },
];

// Default face — used for the OG card and when ?font is missing/unknown.
export const lettersBodyFont = CRIMSON;

export function resolveLettersFont(id: string | undefined): LettersFont {
  return lettersBodyFontOptions.find((o) => o.id === id) ?? lettersBodyFont;
}

// CSS custom properties the shell + typography read, for a given face. Applied
// inline on the treatment root so ?font swaps the face without a reload.
export function lettersFontVars(font: LettersFont): Record<string, string> {
  return {
    "--font-display": font.stack,
    "--treatment-display-font": font.stack,
    "--treatment-body-font": font.stack,
    "--letters-body-weight": String(font.weight),
    "--letters-body-size": font.bodySize,
  };
}

// The dispatch sign-off hand. Only loaded/used on the letters surface.
export const lettersSignatureFont = {
  family: "Pinyon Script",
  stack: "'Pinyon Script', cursive",
  initials: "S",
  signer: "Shovon Hasan",
  faces: [
    { file: "PinyonScript-Regular.woff2", style: "normal", weight: "400", variable: false },
  ] satisfies readonly LetterFontFace[],
} as const;

function faceCss(family: string, f: LetterFontFace): string {
  const format = f.variable ? "woff2-variations" : "woff2";
  return `@font-face{font-family:"${family}";src:url("/fonts/${f.file}") format("${format}");font-weight:${f.weight};font-style:${f.style};font-display:swap;}`;
}

// CSS for the letters treatment: @font-face for every selectable face (so
// ?font swaps instantly), the default font variables, and the body
// size/weight. Concatenated into the inlined critical CSS so first paint has
// the face.
export function lettersTypographyCss(): string {
  const faces = [
    ...lettersBodyFontOptions.flatMap((font) => font.faces.map((f) => faceCss(font.family, f))),
    ...lettersSignatureFont.faces.map((f) => faceCss(lettersSignatureFont.family, f)),
  ].join("");
  const defaults = Object.entries(lettersFontVars(lettersBodyFont))
    .map(([k, v]) => `${k}:${v};`)
    .join("");
  const vars = `[data-treatment="letters"]{${defaults}}`;
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
