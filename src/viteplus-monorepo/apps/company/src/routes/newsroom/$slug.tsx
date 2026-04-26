import { createFileRoute, Link, notFound } from "@tanstack/react-router";
import { useEffect } from "react";
import {
  CATEGORY_LABELS,
  NEWSROOM_META,
  newsroomItemBySlug,
  type NewsroomItem,
} from "~/content/newsroom";
import { emitSpan } from "~/lib/telemetry/browser";
import { ogMeta } from "~/lib/head";

// /newsroom/$slug — one bulletin.
//
// Structure: a Flare hero band carrying the kicker, the display-serif
// headline, and the deck — this is the article's one Flare moment, matching
// the "one Flare giant bulletin per page" rule. Below the band, the body
// paragraphs set on the Newsroom Argent ground (Geist, 64ch measure) in
// the Letters register. Brand-model memory: Newsroom = Letters body + Flare
// hero band.
//
// One article = one route. The slug maps to a single NewsroomItem by
// identity; unknown slugs throw notFound() so retired bulletins return 404
// rather than stale HTML.

export const Route = createFileRoute("/newsroom/$slug")({
  component: NewsroomArticle,
  loader: ({ params }) => {
    const item = newsroomItemBySlug(params.slug);
    if (!item) {
      throw notFound();
    }
    return { item };
  },
  head: ({ loaderData }) => {
    const item = loaderData?.item;
    if (!item) {
      return { meta: [{ title: NEWSROOM_META.title }] };
    }
    return {
      meta: ogMeta({
        slug: "newsroom",
        title: `${item.title} — Guardian Newsroom`,
        description: item.deck,
      }),
      links: [{ rel: "canonical", href: `/newsroom/${item.slug}` }],
    };
  },
});

function NewsroomArticle() {
  const { item } = Route.useLoaderData();

  useEffect(() => {
    if (typeof window === "undefined") return;
    emitSpan("company.newsroom_article.view", {
      "article.slug": item.slug,
      "article.published_at": item.publishedAt,
      "article.category": item.category,
    });
  }, [item.slug, item.publishedAt, item.category]);

  return (
    <article data-newsroom-article data-slug={item.slug}>
      <FlareHeroBand item={item} />
      <div className="mx-auto w-full max-w-6xl px-4 pb-16 md:px-6 md:pb-24">
        <Breadcrumb title={item.title} />
        <Byline item={item} />
        <ArticleBody item={item} />
        <ReadNext slug={item.slug} />
      </div>
    </article>
  );
}

// FlareHeroBand — the article's single Flare moment. Full-bleed Flare
// ground with the kicker, display-serif headline, and deck centered. The
// dimensions mirror the index bulletin card (1312:689 aspect at desktop
// widths) so the band reads as a continuation of the broadcast, not a
// second Flare event competing with the first.
function FlareHeroBand({ item }: { item: NewsroomItem }) {
  return (
    <div
      data-newsroom-article-hero
      style={{
        background: "var(--color-flare)",
        color: "var(--color-ink)",
        borderBottom: "1px solid rgba(11, 11, 11, 0.12)",
      }}
    >
      <div
        className="mx-auto flex w-full max-w-6xl flex-col items-center justify-center gap-5 px-4 py-16 text-center md:gap-6 md:px-6 md:py-24"
        style={{ minHeight: "clamp(360px, 36vw, 520px)" }}
      >
        <p
          className="font-mono text-[11px] font-semibold uppercase"
          style={{
            color: "rgba(11, 11, 11, 0.72)",
            letterSpacing: "0.22em",
            fontVariationSettings: '"wght" 600',
            margin: 0,
          }}
        >
          {CATEGORY_LABELS[item.category]} · {item.date}
        </p>
        <h1
          style={{
            fontFamily: "'Fraunces', Georgia, serif",
            fontVariationSettings: '"opsz" 144, "SOFT" 30',
            fontWeight: 400,
            fontSize: "clamp(40px, 6.4vw, 88px)",
            lineHeight: 1.0,
            letterSpacing: "-0.03em",
            color: "var(--color-ink)",
            margin: 0,
            maxWidth: "22ch",
          }}
        >
          {item.title}
        </h1>
        <p
          style={{
            fontFamily: "'Geist', sans-serif",
            fontSize: "clamp(16px, 1.6vw, 20px)",
            lineHeight: 1.5,
            color: "rgba(11, 11, 11, 0.72)",
            margin: 0,
            maxWidth: "56ch",
          }}
        >
          {item.deck}
        </p>
      </div>
    </div>
  );
}

function Breadcrumb({ title }: { title: string }) {
  return (
    <nav
      aria-label="Breadcrumb"
      className="mt-10 font-mono text-[11px] uppercase md:mt-14"
      style={{
        color: "var(--treatment-muted-meta)",
        letterSpacing: "0.16em",
      }}
    >
      <ol className="flex flex-wrap items-center gap-x-2">
        <li>
          <Link
            to="/newsroom"
            className="transition-colors hover:underline hover:underline-offset-4"
            style={{ color: "var(--treatment-muted)" }}
          >
            Newsroom
          </Link>
        </li>
        <li aria-hidden="true" style={{ color: "var(--treatment-muted-faint)" }}>
          →
        </li>
        <li
          aria-current="page"
          style={{
            color: "var(--treatment-muted-meta)",
            maxWidth: "48ch",
            overflow: "hidden",
            textOverflow: "ellipsis",
            whiteSpace: "nowrap",
          }}
        >
          {title}
        </li>
      </ol>
    </nav>
  );
}

// Byline — avatar tile + name/role stack. The Ramp /blog/news pattern: a
// 40px circular crop on the left, name in the page's display register on
// the upper line, role in muted meta type below. The tile sits as a sibling
// of the article body (not on a top rule), so the byline reads as the
// person attached to the dispatch rather than chrome above the article.
function Byline({ item }: { item: NewsroomItem }) {
  const { author } = item;
  return (
    <div
      className="mt-10 flex items-center gap-3 md:mt-14"
      data-newsroom-byline
    >
      {author.avatar ? (
        <img
          src={author.avatar}
          alt=""
          width={40}
          height={40}
          loading="lazy"
          decoding="async"
          style={{
            width: "40px",
            height: "40px",
            borderRadius: "9999px",
            objectFit: "cover",
            flexShrink: 0,
          }}
        />
      ) : null}
      <div className="flex flex-col">
        <span
          style={{
            fontFamily: "'Geist', sans-serif",
            fontWeight: 500,
            fontSize: "14px",
            lineHeight: 1.2,
            color: "var(--treatment-ink)",
          }}
        >
          {author.name}
        </span>
        <span
          style={{
            fontFamily: "'Geist', sans-serif",
            fontWeight: 400,
            fontSize: "13px",
            lineHeight: 1.3,
            color: "var(--treatment-muted-meta)",
          }}
        >
          {author.role}
        </span>
      </div>
    </div>
  );
}

function ArticleBody({ item }: { item: NewsroomItem }) {
  return (
    <div className="mt-8 flex flex-col gap-5" style={{ maxWidth: "64ch" }}>
      {item.body.map((paragraph, idx) => (
        <p
          key={idx}
          style={{
            fontFamily: "'Geist', sans-serif",
            fontWeight: 400,
            fontSize: "clamp(16px, 1.4vw, 18px)",
            lineHeight: 1.6,
            color: "var(--treatment-muted-strong)",
            margin: 0,
          }}
        >
          {paragraph}
        </p>
      ))}
    </div>
  );
}

function ReadNext({ slug }: { slug: string }) {
  return (
    <footer
      className="mt-16 flex flex-col gap-4 pt-8"
      style={{ borderTop: "1px solid var(--treatment-hairline)" }}
    >
      <p
        className="font-mono text-[10px] font-semibold uppercase"
        style={{
          color: "var(--treatment-muted-meta)",
          letterSpacing: "0.2em",
          fontVariationSettings: '"wght" 600',
          margin: 0,
        }}
      >
        Read next
      </p>
      <div className="flex flex-wrap items-baseline gap-x-8 gap-y-2 text-sm">
        <Link
          to="/newsroom"
          data-newsroom-back
          onClick={() =>
            emitSpan("company.newsroom_article.back_click", {
              "article.slug": slug,
            })
          }
          className="transition-colors hover:underline hover:underline-offset-4"
          style={{
            fontFamily: "'Geist', sans-serif",
            color: "var(--treatment-ink)",
            fontWeight: 500,
          }}
        >
          ← Back to the Newsroom
        </Link>
        <Link
          to="/letters"
          className="transition-colors hover:underline hover:underline-offset-4"
          style={{
            fontFamily: "'Geist', sans-serif",
            color: "var(--treatment-muted)",
          }}
        >
          Letters →
        </Link>
      </div>
    </footer>
  );
}
