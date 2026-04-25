import { assertVoice, formatViolation, type VoiceViolation } from "~/brand/voice.ts";
import { WINGS_PATH_D } from "@verself/brand";

// OG card generator. Produces a 1200×630 SVG matching /design §14 — Iron
// ground, Argent wings, Fraunces headline with one loud word in Flare, plain
// footer. Every dynamic string is run through assertVoice() before emission;
// a voice failure surfaces as a tagged error so the route can 5xx loudly
// (output_contract: "Failures should be loud; signals should be followed to
// address root causes").

const WIDTH = 1200;
const HEIGHT = 630;
const IRON = "#0E0E0E";
const ARGENT = "#FFFFFF";
const FLARE = "#CCFF00";
const MUTED = "rgba(245,245,245,0.6)";

export interface OGSpec {
  readonly slug: string;
  readonly title: string; // Fraunces headline
  readonly flare: string; // The one loud word — must appear in title
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
    `${spec.title}\n${spec.footerLeft}\n${spec.footerRight}`,
    `og:${spec.slug}`,
  );
  if (!voice.ok) {
    return { ok: false, error: { kind: "voice_violation", violations: voice.violations } };
  }

  if (!spec.title.includes(spec.flare)) {
    return {
      ok: false,
      error: { kind: "flare_not_in_title", flare: spec.flare, title: spec.title },
    };
  }

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
  const svg = `<?xml version="1.0" encoding="UTF-8"?>
<svg xmlns="http://www.w3.org/2000/svg" width="${WIDTH}" height="${HEIGHT}" viewBox="0 0 ${WIDTH} ${HEIGHT}">
  <rect width="${WIDTH}" height="${HEIGHT}" fill="${IRON}"/>
  <g transform="translate(56, 56)">
    <svg width="56" height="56" viewBox="0 0 60 60" xmlns="http://www.w3.org/2000/svg">
      <path d="${WINGS_PATH_D}" fill="${ARGENT}" fill-rule="evenodd"/>
    </svg>
    <text x="72" y="38" font-family="'Geist', 'Inter', sans-serif" font-size="22" font-weight="500" fill="${ARGENT}" letter-spacing="2.6">GUARDIAN</text>
  </g>
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

  return { ok: true, svg, contentHash: shortHash(svg) };
}

export function formatOGError(error: OGBuildError): string {
  switch (error.kind) {
    case "voice_violation":
      return `voice_violation: ${error.violations.map(formatViolation).join("; ")}`;
    case "flare_not_in_title":
      return `flare_not_in_title: "${error.flare}" is not a substring of title "${error.title}"`;
  }
}
