import { createFileRoute } from "@tanstack/react-router";
import { LETTERS_META, sortedLetters } from "~/content/letters";
import { SITE_URL } from "~/lib/head";

// Dynamic sitemap. Routes are declared once here so (a) the list is the
// single source of truth that the crawler, the voice lint, and the humans
// agree on, and (b) retired paths vanish the moment the route is removed
// rather than living on in a stale static XML. Letters are enumerated from
// the content catalog so every post is indexable from the day it ships.
//
// No external dep: the sitemap format is ~10 lines of XML and a sitemap
// library (with plugins, schemas, priorities) is more surface than a 10-URL
// marketing site should import. If the list grows past ~1,000 URLs or starts
// needing lastmod/changefreq, revisit with the `sitemap` npm package.

const STATIC_PATHS: readonly string[] = [
  "/",
  "/company",
  "/solutions",
  "/letters",
  "/design",
  "/press",
  "/careers",
  "/changelog",
  "/contact",
];

const SITEMAP_HEADERS = {
  "content-type": "application/xml; charset=utf-8",
  "cache-control": "public, max-age=600, s-maxage=600",
} as const;

function buildSitemap(): string {
  const letterPaths = sortedLetters().map((letter) => ({
    loc: `${LETTERS_META.siteURL}/letters/${letter.slug}`,
    lastmod: letter.publishedAt,
  }));
  const staticUrls = STATIC_PATHS.map((p) => ({
    loc: p === "/" ? `${SITE_URL}/` : `${SITE_URL}${p}`,
    lastmod: undefined as string | undefined,
  }));
  const entries = [...staticUrls, ...letterPaths]
    .map(({ loc, lastmod }) => {
      const inner = lastmod
        ? `<loc>${loc}</loc><lastmod>${lastmod}</lastmod>`
        : `<loc>${loc}</loc>`;
      return `  <url>${inner}</url>`;
    })
    .join("\n");

  return `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
${entries}
</urlset>
`;
}

export const Route = createFileRoute("/sitemap.xml")({
  server: {
    handlers: {
      HEAD: () => new Response(null, { status: 200, headers: SITEMAP_HEADERS }),
      GET: () => {
        const xml = buildSitemap();
        return new Response(xml, { status: 200, headers: SITEMAP_HEADERS });
      },
    },
  },
});
