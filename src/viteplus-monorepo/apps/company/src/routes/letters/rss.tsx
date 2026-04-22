import { createFileRoute } from "@tanstack/react-router";
import { LETTERS_META, sortedLetters } from "~/content/letters";

// RSS 2.0. Industry-standard XML surface that any reader can fetch. Generated
// from src/content/letters.ts so adding a letter = editing the content file.

function escapeXml(s: string): string {
  return s
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&apos;");
}

function toRFC822(isoDate: string): string {
  const d = new Date(`${isoDate}T12:00:00Z`);
  return d.toUTCString();
}

function buildFeed(): string {
  const letters = sortedLetters();
  const latest = letters[0];
  const lastBuildDate = latest ? toRFC822(latest.publishedAt) : new Date().toUTCString();

  const items = letters
    .map((letter) => {
      const link = `${LETTERS_META.siteURL}/letters/${letter.slug}`;
      const description = letter.body.map(escapeXml).join("&#10;&#10;");
      return `    <item>
      <title>${escapeXml(letter.title)}</title>
      <link>${link}</link>
      <guid isPermaLink="true">${link}</guid>
      <pubDate>${toRFC822(letter.publishedAt)}</pubDate>
      <author>${escapeXml(letter.author)}</author>
      <description>${description}</description>
    </item>`;
    })
    .join("\n");

  return `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:atom="http://www.w3.org/2005/Atom">
  <channel>
    <title>${escapeXml(LETTERS_META.title)}</title>
    <link>${LETTERS_META.siteURL}/letters</link>
    <atom:link href="${LETTERS_META.siteURL}/letters/rss" rel="self" type="application/rss+xml" />
    <description>${escapeXml(LETTERS_META.description)}</description>
    <language>en-US</language>
    <managingEditor>${escapeXml(LETTERS_META.editor)}</managingEditor>
    <lastBuildDate>${lastBuildDate}</lastBuildDate>
${items}
  </channel>
</rss>
`;
}

const RSS_HEADERS = {
  "content-type": "application/rss+xml; charset=utf-8",
  "cache-control": "public, max-age=600, s-maxage=600",
} as const;

export const Route = createFileRoute("/letters/rss")({
  server: {
    handlers: {
      HEAD: () => new Response(null, { status: 200, headers: RSS_HEADERS }),
      GET: () => {
        const xml = buildFeed();
        return new Response(xml, { status: 200, headers: RSS_HEADERS });
      },
    },
  },
});
