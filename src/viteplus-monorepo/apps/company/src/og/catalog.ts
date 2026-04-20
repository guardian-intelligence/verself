import type { OGSpec } from "./template";

// OG card catalog. Keyed by slug. Routes look up their spec by slug and hand
// it to buildOGCard(). Adding a card = appending an entry here with a title
// and a flare word that appears in the title.

export const OG_CATALOG: Record<string, OGSpec> = {
  home: {
    slug: "home",
    title: "We ship the reference architecture every founder needs.",
    flare: "architecture",
    footerLeft: "anveio.com",
    footerRight: "Seattle · 2026",
  },
  design: {
    slug: "design",
    title: "The Guardian Intelligence brand system.",
    flare: "Guardian",
    footerLeft: "anveio.com/design",
    footerRight: "Seattle · 2026",
  },
  letters: {
    slug: "letters",
    title: "Long-form from Guardian Intelligence.",
    flare: "Long-form",
    footerLeft: "anveio.com/letters",
    footerRight: "Seattle · 2026",
  },
  solutions: {
    slug: "solutions",
    title: "One house, one platform.",
    flare: "one platform",
    footerLeft: "anveio.com/solutions",
    footerRight: "Seattle · 2026",
  },
};

export function ogSpecFor(slug: string): OGSpec | undefined {
  return OG_CATALOG[slug];
}
