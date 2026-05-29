import { assertVoice, formatViolation, type VoiceViolation } from "~/brand/voice.ts";
import { WINGS_PADDED_VIEWBOX, WINGS_PATH_D } from "@verself/brand";

// OG card generator. Produces treatment-specific 1200×630 SVG cards.
// Workshop keeps the Iron/Flare house card, Newsroom uses an Argent page with
// one bounded Flare stripe, and Letters uses Paper with quiet graph ruling.
// Every dynamic string is run through assertVoice() before emission; a voice
// failure surfaces as a tagged error so the route can 5xx loudly.

const WIDTH = 1200;
const HEIGHT = 630;
const IRON = "#0e0e0e";
const INK = "#0b0b0b";
const ARGENT = "#FFFFFF";
const PAPER = "#fff4dc";
const FLARE = "#ccff00";
const SEPIA = "#5b3a26";
const MUTED = "rgba(245,245,245,0.6)";
const STONE_STRONG = "rgba(11,11,11,0.9)";
const STONE = "rgba(11,11,11,0.62)";

export type OGTreatment = "workshop" | "newsroom" | "letters";

export interface OGSpec {
  readonly treatment?: OGTreatment;
  readonly slug: string;
  readonly title: string; // Fraunces headline
  readonly flare: string; // The one loud word — must appear in title
  readonly kicker?: string;
  readonly subtitle?: string;
  readonly bodyExcerpt?: string;
  readonly footerLeft: string; // e.g. "guardianintelligence.org"
  readonly footerRight: string; // e.g. "Seattle · 2026"
}

export type OGBuildError =
  | {
      readonly kind: "voice_violation";
      readonly violations: readonly VoiceViolation[];
    }
  | {
      readonly kind: "flare_not_in_title";
      readonly flare: string;
      readonly title: string;
    };

export type OGBuildResult =
  | {
      readonly ok: true;
      readonly svg: string;
      readonly contentHash: string;
    }
  | {
      readonly ok: false;
      readonly error: OGBuildError;
    };

function xmlEscape(s: string): string {
  return s
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&apos;");
}

function nonEmpty(value: string | undefined): value is string {
  return typeof value === "string" && value.length > 0;
}

function wrapText(
  text: string,
  maxChars: number,
  maxLines: number,
  options: { readonly ellipsis?: boolean } = {},
): readonly string[] {
  const ellipsis = options.ellipsis ?? true;
  const words = text.split(/\s+/).filter(Boolean);
  const lines: string[] = [];
  let current = "";

  for (const word of words) {
    const next = current ? `${current} ${word}` : word;
    if (next.length <= maxChars) {
      current = next;
      continue;
    }
    if (current) {
      lines.push(current);
      current = word;
    } else {
      lines.push(word);
    }
    if (lines.length === maxLines) break;
  }

  if (current && lines.length < maxLines) lines.push(current);
  if (ellipsis && lines.length === maxLines && words.join(" ").length > lines.join(" ").length) {
    const last = lines[maxLines - 1];
    if (last) lines[maxLines - 1] = `${last.replace(/[.,;:!?]$/, "")}...`;
  }
  return lines;
}

function textLines(lines: readonly string[], x: number, lineHeight: number): string {
  return lines
    .map((line, index) =>
      index === 0
        ? `<tspan x="${x}">${xmlEscape(line)}</tspan>`
        : `<tspan x="${x}" dy="${lineHeight}">${xmlEscape(line)}</tspan>`,
    )
    .join("");
}

function brandMark({
  x,
  y,
  markFill,
  wordmarkFill,
  section,
  variant = "argent",
  chip = false,
}: {
  readonly x: number;
  readonly y: number;
  readonly markFill: string;
  readonly wordmarkFill: string;
  readonly section?: string;
  readonly variant?: "argent" | "chip" | "emboss";
  readonly chip?: boolean;
}): string {
  const resolvedVariant = chip ? "chip" : variant;
  const size = resolvedVariant === "chip" ? 34 : 56;
  const wordmarkX = x + size + (chip ? 14 : 16);
  const wordmarkY = y + (chip ? 24 : 38);
  const sectionText = section ? ` · ${section.toUpperCase()}` : "";
  const mark =
    resolvedVariant === "chip"
      ? `<svg x="${x}" y="${y}" width="${size}" height="${size}" viewBox="${WINGS_PADDED_VIEWBOX}" xmlns="http://www.w3.org/2000/svg">
      <rect x="30.01" y="31.08" width="291.14" height="291.14" rx="32" ry="32" fill="${markFill}"/>
      <path d="${WINGS_PATH_D}" fill="${ARGENT}" fill-rule="evenodd"/>
    </svg>`
      : resolvedVariant === "emboss"
        ? `<svg x="${x}" y="${y}" width="${size}" height="${size}" viewBox="${WINGS_PADDED_VIEWBOX}" xmlns="http://www.w3.org/2000/svg">
      <circle cx="176" cy="177" r="126" fill="${markFill}"/>
      <path d="${WINGS_PATH_D}" fill="${ARGENT}" fill-rule="evenodd"/>
    </svg>`
        : `<svg x="${x}" y="${y}" width="${size}" height="${size}" viewBox="${WINGS_PADDED_VIEWBOX}" xmlns="http://www.w3.org/2000/svg">
      <path d="${WINGS_PATH_D}" fill="${markFill}" fill-rule="evenodd"/>
    </svg>`;

  return `<g>
    ${mark}
    <text x="${wordmarkX}" y="${wordmarkY}" font-family="'Geist', 'Inter', sans-serif" font-size="${chip ? 18 : 22}" font-weight="600" fill="${wordmarkFill}" letter-spacing="${chip ? 4.2 : 2.6}">GUARDIAN${sectionText}</text>
  </g>`;
}

// Stable, deterministic short hash of the card payload. Used as the
// og.content_hash span attribute so the canary can correlate a rendered card
// with the slug's content version.
function shortHash(input: string): string {
  let h = 2166136261;
  for (let i = 0; i < input.length; i += 1) {
    h ^= input.charCodeAt(i);
    h = Math.imul(h, 16777619);
  }
  return (h >>> 0).toString(16).padStart(8, "0");
}

export function buildOGCard(spec: OGSpec): OGBuildResult {
  const voice = assertVoice(
    [spec.title, spec.kicker, spec.subtitle, spec.bodyExcerpt, spec.footerLeft, spec.footerRight]
      .filter(nonEmpty)
      .join("\n"),
    `og:${spec.slug}`,
  );
  if (!voice.ok) {
    return { ok: false, error: { kind: "voice_violation", violations: voice.violations } };
  }

  const treatment = spec.treatment ?? "workshop";
  if (treatment === "workshop" && !spec.title.includes(spec.flare)) {
    return {
      ok: false,
      error: { kind: "flare_not_in_title", flare: spec.flare, title: spec.title },
    };
  }

  const result =
    treatment === "letters"
      ? buildLettersCard(spec)
      : treatment === "newsroom"
        ? buildNewsroomCard(spec)
        : buildWorkshopCard(spec);

  return { ok: true, svg: result, contentHash: shortHash(result) };
}

function buildWorkshopCard(spec: OGSpec): string {
  // Split the title around the Flare word so we can colour just that span.
  const index = spec.title.indexOf(spec.flare);
  const before = spec.title.slice(0, index);
  const flareWord = spec.title.slice(index, index + spec.flare.length);
  const after = spec.title.slice(index + spec.flare.length);

  // The wings sit at 56px wide in the top-left, scaled from the shared brand
  // mark path. Fraunces is reserved for the body headline (where the Flare
  // word takes the acid accent); the masthead wordmark sets in tracked
  // uppercase Geist to match the same GUARDIAN treatment the HTML chrome
  // ships. Social platforms rasterise server-side and may not ship our WOFF2
  // — the face stacks fall through to Inter / Georgia so the card degrades
  // legibly rather than losing the lockup silhouette.
  return `<?xml version="1.0" encoding="UTF-8"?>
<svg xmlns="http://www.w3.org/2000/svg" width="${WIDTH}" height="${HEIGHT}" viewBox="0 0 ${WIDTH} ${HEIGHT}">
  <rect width="${WIDTH}" height="${HEIGHT}" fill="${IRON}"/>
  ${brandMark({ x: 56, y: 56, markFill: ARGENT, wordmarkFill: ARGENT })}
  <g transform="translate(56, 260)">
    <text font-family="'Fraunces', Georgia, serif" font-size="72" font-weight="400" fill="${ARGENT}" letter-spacing="-0.025em">
      <tspan x="0" dy="0">${xmlEscape(before)}<tspan fill="${FLARE}">${xmlEscape(flareWord)}</tspan>${xmlEscape(after)}</tspan>
    </text>
  </g>
  <g transform="translate(56, ${HEIGHT - 56})">
    <text font-family="'Geist', 'Inter', sans-serif" font-size="16" fill="${MUTED}">${xmlEscape(spec.footerLeft)}</text>
    <text x="${WIDTH - 112}" font-family="'Geist', 'Inter', sans-serif" font-size="16" fill="${MUTED}" text-anchor="end">${xmlEscape(spec.footerRight)}</text>
  </g>
</svg>
`;
}

function buildNewsroomCard(spec: OGSpec): string {
  const titleLines = wrapText(spec.title, 18, 2);
  const subtitleLines = spec.subtitle ? wrapText(spec.subtitle, 54, 2) : [];

  return `<?xml version="1.0" encoding="UTF-8"?>
<svg xmlns="http://www.w3.org/2000/svg" width="${WIDTH}" height="${HEIGHT}" viewBox="0 0 ${WIDTH} ${HEIGHT}">
  <rect width="${WIDTH}" height="${HEIGHT}" fill="${ARGENT}"/>
  <rect x="56" y="56" width="1088" height="518" rx="28" fill="${ARGENT}" stroke="rgba(11,11,11,0.12)" stroke-width="2"/>
  <path d="M84 56H1116C1131.46 56 1144 68.54 1144 84V190H56V84C56 68.54 68.54 56 84 56Z" fill="${FLARE}"/>
  ${brandMark({ x: 96, y: 96, markFill: INK, wordmarkFill: INK, section: "Newsroom", variant: "emboss" })}
  <text x="96" y="252" font-family="'Geist', 'Inter', sans-serif" font-size="18" font-weight="600" fill="${STONE}" letter-spacing="3.2">${xmlEscape(spec.kicker ?? "NEWSROOM")}</text>
  <text x="96" y="320" font-family="'Fraunces', Georgia, serif" font-size="64" font-weight="400" fill="${INK}" letter-spacing="0">
    ${textLines(titleLines, 96, 68)}
  </text>
  ${
    subtitleLines.length
      ? `<text x="96" y="490" font-family="'Geist', 'Inter', sans-serif" font-size="23" font-weight="400" fill="${STONE}" letter-spacing="0">
    ${textLines(subtitleLines, 96, 32)}
  </text>`
      : ""
  }
  <text x="96" y="542" font-family="'Geist', 'Inter', sans-serif" font-size="16" fill="${STONE}">${xmlEscape(spec.footerLeft)}</text>
  <text x="1104" y="542" font-family="'Geist', 'Inter', sans-serif" font-size="16" fill="${STONE}" text-anchor="end">${xmlEscape(spec.footerRight)}</text>
</svg>
`;
}

function buildLettersCard(spec: OGSpec): string {
  const salutationLines = wrapText(spec.title, 26, 2);
  const bodyY = 342 + (salutationLines.length - 1) * 78;
  const bodyText = spec.bodyExcerpt ?? spec.subtitle ?? "";
  // Cap the excerpt to lines that clear the footer rule (y=532). A two-line
  // salutation drops bodyY by 78px; at the old fixed 5 lines the tail rendered
  // over the rule once rasterised. The last baseline plus its descender must
  // sit above the rule: bodyY + (n-1)·39 + ~8 < 532.
  const maxBodyLines = Math.max(2, Math.min(5, Math.floor(1 + (524 - bodyY) / 39)));
  const bodyLines = bodyText ? wrapText(bodyText, 52, maxBodyLines, { ellipsis: false }) : [];
  const bodyFadeY = bodyY - 34;

  return `<?xml version="1.0" encoding="UTF-8"?>
<svg xmlns="http://www.w3.org/2000/svg" width="${WIDTH}" height="${HEIGHT}" viewBox="0 0 ${WIDTH} ${HEIGHT}">
  <defs>
    <linearGradient id="letterBodyFadeGradient" x1="0" y1="${bodyFadeY}" x2="0" y2="${bodyFadeY + 190}" gradientUnits="userSpaceOnUse">
      <stop offset="0" stop-color="#fff"/>
      <stop offset="0.7" stop-color="#fff"/>
      <stop offset="1" stop-color="#000"/>
    </linearGradient>
    <mask id="letterBodyFade" maskUnits="userSpaceOnUse" x="72" y="${bodyFadeY}" width="1056" height="206">
      <rect x="72" y="${bodyFadeY}" width="1056" height="206" fill="url(#letterBodyFadeGradient)"/>
    </mask>
    <pattern id="minor" width="28" height="28" patternUnits="userSpaceOnUse">
      <path d="M27.5 0V28M0 27.5H28" stroke="${INK}" stroke-width="1" opacity="0.055" fill="none"/>
    </pattern>
    <pattern id="major" width="140" height="140" patternUnits="userSpaceOnUse">
      <path d="M139.5 0V140M0 139.5H140" stroke="${INK}" stroke-width="1.2" opacity="0.085" fill="none"/>
    </pattern>
  </defs>
  <rect width="${WIDTH}" height="${HEIGHT}" fill="${PAPER}"/>
  <rect width="${WIDTH}" height="${HEIGHT}" fill="url(#minor)"/>
  <rect width="${WIDTH}" height="${HEIGHT}" fill="url(#major)"/>
  <rect width="${WIDTH}" height="${HEIGHT}" fill="rgba(255,255,255,0.28)"/>
  ${brandMark({ x: 72, y: 56, markFill: INK, wordmarkFill: INK, section: "Letters", chip: true })}
  <text x="1128" y="80" font-family="'Geist', 'Inter', sans-serif" font-size="18" font-weight="500" fill="${STONE}" text-anchor="end" letter-spacing="0">${xmlEscape(spec.kicker ?? "Letters")}</text>
  <text x="72" y="258" font-family="'Fraunces', Georgia, serif" font-size="78" font-weight="400" fill="${INK}" letter-spacing="0" style="font-variation-settings:'opsz' 18, 'SOFT' 0;">
    ${textLines(salutationLines, 72, 78)}
  </text>
  ${
    bodyLines.length
      ? `<text x="72" y="${bodyY}" font-family="'Fraunces', Georgia, serif" font-size="28" font-weight="400" fill="${STONE_STRONG}" letter-spacing="0" mask="url(#letterBodyFade)" style="font-variation-settings:'opsz' 18, 'SOFT' 0;">
    ${textLines(bodyLines, 72, 39)}
  </text>`
      : ""
  }
  <line x1="72" y1="532" x2="1128" y2="532" stroke="${SEPIA}" stroke-width="2" opacity="0.46"/>
  <text x="72" y="568" font-family="'Geist', 'Inter', sans-serif" font-size="16" fill="${STONE}" letter-spacing="0">${xmlEscape(spec.footerLeft)}</text>
  <text x="1128" y="568" font-family="'Geist', 'Inter', sans-serif" font-size="16" fill="${STONE}" text-anchor="end" letter-spacing="0">${xmlEscape(spec.footerRight)}</text>
</svg>
`;
}

export function formatOGError(error: OGBuildError): string {
  switch (error.kind) {
    case "voice_violation":
      return `voice_violation: ${error.violations.map(formatViolation).join("; ")}`;
    case "flare_not_in_title":
      return `flare_not_in_title: "${error.flare}" is not a substring of title "${error.title}"`;
  }
}
