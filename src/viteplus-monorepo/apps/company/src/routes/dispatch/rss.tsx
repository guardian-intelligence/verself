import { createFileRoute } from "@tanstack/react-router";
import { DISPATCH_META, sortedPosts } from "~/content/dispatch";

// RSS 2.0. Industry-standard XML surface that any reader can fetch. Generated
// from src/content/dispatch.ts so adding a post = editing the content file.
// The route emits a company.rss.fetch span with the item count so operators
// can see whether readers are actually pulling the feed.

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
  const posts = sortedPosts();
  const latest = posts[0];
  const lastBuildDate = latest ? toRFC822(latest.publishedAt) : new Date().toUTCString();

  const items = posts
    .map((post) => {
      const link = `${DISPATCH_META.siteURL}/dispatch/${post.slug}`;
      const description = post.body.map(escapeXml).join("&#10;&#10;");
      return `    <item>
      <title>${escapeXml(post.title)}</title>
      <link>${link}</link>
      <guid isPermaLink="true">${link}</guid>
      <pubDate>${toRFC822(post.publishedAt)}</pubDate>
      <author>${escapeXml(post.author)}</author>
      <description>${description}</description>
    </item>`;
    })
    .join("\n");

  return `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:atom="http://www.w3.org/2005/Atom">
  <channel>
    <title>${escapeXml(DISPATCH_META.title)}</title>
    <link>${DISPATCH_META.siteURL}/dispatch</link>
    <atom:link href="${DISPATCH_META.siteURL}/dispatch/rss" rel="self" type="application/rss+xml" />
    <description>${escapeXml(DISPATCH_META.description)}</description>
    <language>en-US</language>
    <managingEditor>${escapeXml(DISPATCH_META.editor)}</managingEditor>
    <lastBuildDate>${lastBuildDate}</lastBuildDate>
${items}
  </channel>
</rss>
`;
}

export const Route = createFileRoute("/dispatch/rss")({
  server: {
    handlers: {
      GET: () => {
        const xml = buildFeed();
        return new Response(xml, {
          status: 200,
          headers: {
            "content-type": "application/rss+xml; charset=utf-8",
            "cache-control": "public, max-age=600, s-maxage=600",
          },
        });
      },
    },
  },
});
