import { letterBySlug, sortedLetters } from "~/content/letters";
import type { OGSpec } from "./template";

// OG card catalog. Keyed by slug. Every public route looks up its spec by
// slug and hands it to buildOGCard(). Adding a card = appending an entry here
// with a title and a flare word that appears in the title. The voice lint
// runs on every title before render, so banned words fail loudly at request
// time instead of reaching the share preview.
//
// Letters are NOT enumerated here — they synthesize an OGSpec from the
// frontmatter at lookup time via the "letter/<slug>" namespace. Add a .md
// file under src/content/letters/ and the OG endpoint serves a per-letter
// card with no catalog edit.

export const OG_CATALOG: Record<string, OGSpec> = {
  home: {
    slug: "home",
    title: "We ship the reference architecture every founder needs.",
    flare: "architecture",
    footerLeft: "guardianintelligence.org",
    footerRight: "Seattle · 2026",
  },
  design: {
    slug: "design",
    title: "The Guardian brand system.",
    flare: "Guardian",
    footerLeft: "guardianintelligence.org/design",
    footerRight: "Seattle · 2026",
  },
  letters: {
    slug: "letters",
    title: "Long-form from Guardian.",
    flare: "Long-form",
    footerLeft: "guardianintelligence.org/letters",
    footerRight: "Seattle · 2026",
  },
  newsroom: {
    slug: "newsroom",
    title: "Bulletins from Guardian.",
    flare: "Bulletins",
    footerLeft: "guardianintelligence.org/newsroom",
    footerRight: "Seattle · 2026",
  },
  solutions: {
    slug: "solutions",
    title: "One house, one platform.",
    flare: "one platform",
    footerLeft: "guardianintelligence.org/solutions",
    footerRight: "Seattle · 2026",
  },
  company: {
    slug: "company",
    title: "An American applied intelligence firm.",
    flare: "firm",
    footerLeft: "guardianintelligence.org/company",
    footerRight: "Seattle · 2026",
  },
  careers: {
    slug: "careers",
    title: "We hire slowly.",
    flare: "slowly",
    footerLeft: "guardianintelligence.org/careers",
    footerRight: "Seattle · 2026",
  },
  contact: {
    slug: "contact",
    title: "We answer every note.",
    flare: "every note",
    footerLeft: "guardianintelligence.org/contact",
    footerRight: "Seattle · 2026",
  },
  press: {
    slug: "press",
    title: "The brand, on the record.",
    flare: "on the record",
    footerLeft: "guardianintelligence.org/press",
    footerRight: "Seattle · 2026",
  },
  changelog: {
    slug: "changelog",
    title: "What shipped, when.",
    flare: "shipped",
    footerLeft: "guardianintelligence.org/changelog",
    footerRight: "Seattle · 2026",
  },
};

const LETTER_SLUG_PREFIX = "letter/";

export function ogSpecFor(slug: string): OGSpec | undefined {
  if (slug.startsWith(LETTER_SLUG_PREFIX)) {
    const letterSlug = slug.slice(LETTER_SLUG_PREFIX.length);
    const letter = letterBySlug(letterSlug);
    if (!letter) return undefined;
    return {
      slug,
      title: letter.title,
      flare: letter.flare,
      footerLeft: `guardianintelligence.org/letters/${letter.slug}`,
      footerRight: "Seattle · 2026",
    };
  }
  return OG_CATALOG[slug];
}

// Public slug list. The sitemap + any other route enumerator consumes this
// instead of hard-coding strings. Letter cards are appended dynamically so
// new letters get their unfurl card without touching this file.
export const OG_SLUGS: readonly string[] = [
  ...Object.keys(OG_CATALOG),
  ...sortedLetters().map((letter) => `${LETTER_SLUG_PREFIX}${letter.slug}`),
];
