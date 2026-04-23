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

// /newsroom/$slug — one bulletin, on Argent.
//
// Structurally a press article: breadcrumb, kicker (category + date),
// Fraunces headline, Geist deck, author byline, Geist body paragraphs, a
// read-next hand-off back into the Newsroom index and across to Letters.
// The treatment comes from the /newsroom layout (data-treatment="newsroom"
// via route.tsx), so every token — display font, muted ramp, hairline —
// resolves to the Newsroom scope without this file knowing.
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
    <article
      className="mx-auto w-full max-w-3xl px-4 py-10 md:px-6 md:py-16"
      data-newsroom-article
      data-slug={item.slug}
    >
      <Breadcrumb title={item.title} />
      <ArticleHeader item={item} />
      <ArticleBody item={item} />
      <ReadNext slug={item.slug} />
    </article>
  );
}

function Breadcrumb({ title }: { title: string }) {
  return (
    <nav
      aria-label="Breadcrumb"
      className="font-mono text-[11px] uppercase"
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

function ArticleHeader({ item }: { item: NewsroomItem }) {
  return (
    <header className="mt-8 flex flex-col gap-6">
      <p
        className="font-mono text-[11px] font-semibold uppercase"
        style={{
          color: "var(--treatment-muted-meta)",
          letterSpacing: "0.2em",
          fontVariationSettings: '"wght" 600',
          margin: 0,
        }}
      >
        {CATEGORY_LABELS[item.category]} · {item.date}
      </p>
      <h1
        style={{
          fontFamily: "var(--treatment-display-font)",
          fontVariationSettings: '"opsz" 144, "SOFT" 30',
          fontWeight: 400,
          fontSize: "clamp(36px, 5.6vw, 60px)",
          lineHeight: 1.03,
          letterSpacing: "-0.026em",
          color: "var(--treatment-ink)",
          margin: 0,
          maxWidth: "20ch",
        }}
      >
        {item.title}
      </h1>
      <p
        style={{
          fontFamily: "'Geist', sans-serif",
          fontSize: "clamp(17px, 1.6vw, 20px)",
          lineHeight: 1.5,
          color: "var(--treatment-muted-strong)",
          margin: 0,
          maxWidth: "54ch",
        }}
      >
        {item.deck}
      </p>
      <div
        className="flex items-baseline gap-3 pt-2"
        style={{ borderTop: "1px solid var(--treatment-hairline)" }}
      >
        <span
          style={{
            fontFamily: "'Fraunces', Georgia, serif",
            fontVariationSettings: '"opsz" 72, "SOFT" 20',
            fontWeight: 400,
            fontSize: "18px",
            color: "var(--treatment-ink)",
            paddingTop: "14px",
          }}
        >
          {item.author.name}
        </span>
        <span
          className="font-mono text-[10px] uppercase"
          style={{
            letterSpacing: "0.18em",
            color: "var(--treatment-muted-meta)",
          }}
        >
          {item.author.role}
        </span>
      </div>
    </header>
  );
}

function ArticleBody({ item }: { item: NewsroomItem }) {
  return (
    <div className="mt-10 flex flex-col gap-5" style={{ maxWidth: "64ch" }}>
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
