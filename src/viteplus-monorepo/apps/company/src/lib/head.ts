// Centralized head helpers. Every route runs its title/description/og/twitter
// meta through ogMeta() so (a) no route ships without a social card and (b)
// the og:image URL is absolute — Facebook, LinkedIn, and some aggregators
// silently drop relative og:image values. The slug must exist in og/catalog.ts;
// the dynamic /og/$slug endpoint rasterises the card to PNG at request time —
// card crawlers (X, Facebook, LinkedIn, Slack) reject SVG image payloads.

const SITE_URL = "https://guardianintelligence.org";

export interface OGMetaInput {
  readonly slug: string;
  readonly title: string;
  readonly description: string;
}

export interface MetaTag {
  readonly title?: string;
  readonly name?: string;
  readonly property?: string;
  readonly content?: string;
}

export function ogMeta(input: OGMetaInput): MetaTag[] {
  const imageURL = `${SITE_URL}/og/${input.slug}`;
  return [
    { title: input.title },
    { name: "description", content: input.description },
    { property: "og:title", content: input.title },
    { property: "og:description", content: input.description },
    { property: "og:image", content: imageURL },
    { property: "og:image:type", content: "image/png" },
    { property: "og:image:width", content: "1200" },
    { property: "og:image:height", content: "630" },
    { name: "twitter:card", content: "summary_large_image" },
    { name: "twitter:image", content: imageURL },
  ];
}

export { SITE_URL };
