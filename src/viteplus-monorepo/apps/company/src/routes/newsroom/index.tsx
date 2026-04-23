import { createFileRoute, Link } from "@tanstack/react-router";
import { useCallback, useEffect } from "react";
import {
  CATEGORY_LABELS,
  NEWSROOM_META,
  currentNewsroomItem,
  newsroomCtaHref,
  type NewsroomItem,
} from "~/content/newsroom";
import { emitSpan } from "~/lib/telemetry/browser";
import { ogMeta } from "~/lib/head";

// /newsroom — the press-room index.
//
// Structural debt to Ramp's /blog/news: a calm masthead, then one giant
// bulletin banner, then a metadata tile linking into the article. Our palette
// swap: Flare ground with Ink type, nothing else. Archive grid and subscribe
// band are retired until we have a second bulletin and a newsletter service
// — running them empty read as decoration, and Flare is an event, not
// decoration. One Flare giant bulletin per page.

export const Route = createFileRoute("/newsroom/")({
  component: NewsroomIndex,
  head: () => ({
    meta: ogMeta({
      slug: "newsroom",
      title: NEWSROOM_META.title,
      description: NEWSROOM_META.description,
    }),
    links: [{ rel: "canonical", href: "/newsroom" }],
  }),
});

function NewsroomIndex() {
  const item = currentNewsroomItem();

  useEffect(() => {
    if (typeof window === "undefined") return;
    emitSpan("newsroom.index.view", {
      has_item: item ? "true" : "false",
      featured_slug: item?.slug ?? "",
    });
  }, [item]);

  const onBulletinClick = useCallback(() => {
    if (!item) return;
    emitSpan("newsroom.index.bulletin_click", {
      slug: item.slug,
      destination: newsroomCtaHref(item),
    });
  }, [item]);

  return (
    <div className="mx-auto w-full max-w-6xl px-4 py-10 md:px-6 md:py-14">
      <Masthead />
      <section aria-label="Featured bulletin" className="mt-10 md:mt-14">
        {item ? <GiantBulletin item={item} onClick={onBulletinClick} /> : <EmptyBulletin />}
      </section>
      {item ? <BulletinMeta item={item} /> : null}
    </div>
  );
}

function Masthead() {
  return (
    <header className="flex flex-col gap-4">
      <p
        className="font-mono text-[11px] font-semibold uppercase tracking-[0.2em]"
        style={{
          color: "var(--treatment-muted-meta)",
          fontVariationSettings: '"wght" 600',
          margin: 0,
        }}
      >
        Newsroom
      </p>
      <h1
        style={{
          fontFamily: "var(--treatment-display-font)",
          fontVariationSettings: '"opsz" 144, "SOFT" 30',
          fontWeight: 400,
          fontSize: "clamp(44px, 6vw, 72px)",
          lineHeight: 1.02,
          letterSpacing: "-0.028em",
          color: "var(--treatment-ink)",
          margin: 0,
        }}
      >
        Bulletins from the house.
      </h1>
      <p
        className="max-w-2xl"
        style={{
          fontFamily: "'Geist', sans-serif",
          fontSize: "clamp(15px, 1.4vw, 17px)",
          lineHeight: 1.55,
          color: "var(--treatment-muted-strong)",
          margin: 0,
        }}
      >
        Milestones, announcements, and public notes from Guardian Intelligence. Short, dated, on the
        record.
      </p>
    </header>
  );
}

// GiantBulletin — the one Flare giant bulletin per page. Ramp's
// "Ramp is coming to Europe" banner is the structural reference: a wide
// rounded rectangle that carries a single centered display-serif headline
// and links into the article. Their version paints a blue dotted Europe
// silhouette over the banner; we paint plain Flare with Ink type. Same
// dimensions, same proportions, no pattern.
//
// Aspect ratio locks to 1312:689 (≈1.905:1) at desktop widths — matching the
// Ramp banner exactly. A clamp-based min-height floors the card on narrow
// screens so the headline keeps its breathing room when the aspect ratio
// would otherwise collapse the banner to a sliver. The whole card is one
// click target; the span fires on mouseup so telemetry lands before the
// route change flushes the batch span processor.
function GiantBulletin({ item, onClick }: { item: NewsroomItem; onClick: () => void }) {
  return (
    <Link
      to={newsroomCtaHref(item)}
      aria-label={`Read bulletin: ${item.title}`}
      data-newsroom-bulletin
      data-slug={item.slug}
      onClick={onClick}
      className="group relative flex items-center justify-center overflow-hidden transition-transform hover:-translate-y-0.5 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-offset-2"
      style={{
        background: "var(--color-flare)",
        color: "var(--color-ink)",
        borderRadius: "24px",
        border: "1px solid rgba(11, 11, 11, 0.08)",
        aspectRatio: "1312 / 689",
        minHeight: "clamp(320px, 38vw, 560px)",
        padding: "clamp(28px, 4vw, 72px)",
        textDecoration: "none",
      }}
    >
      <span
        className="absolute left-5 top-5 font-mono text-[11px] font-semibold uppercase md:left-7 md:top-7"
        style={{
          letterSpacing: "0.22em",
          color: "rgba(11, 11, 11, 0.72)",
          fontVariationSettings: '"wght" 600',
        }}
      >
        {CATEGORY_LABELS[item.category]} · {item.date}
      </span>
      <h2
        style={{
          fontFamily: "'Fraunces', Georgia, serif",
          fontVariationSettings: '"opsz" 144, "SOFT" 30',
          fontWeight: 400,
          fontSize: "clamp(40px, 7vw, 104px)",
          lineHeight: 0.98,
          letterSpacing: "-0.03em",
          color: "var(--color-ink)",
          textAlign: "center",
          maxWidth: "18ch",
          margin: 0,
          padding: "0 0.25em",
        }}
      >
        {item.title}
      </h2>
      <span
        aria-hidden="true"
        className="absolute bottom-5 right-5 font-mono text-[11px] font-semibold uppercase opacity-0 transition-opacity group-hover:opacity-100 md:bottom-7 md:right-7"
        style={{
          letterSpacing: "0.22em",
          color: "rgba(11, 11, 11, 0.72)",
          fontVariationSettings: '"wght" 600',
        }}
      >
        Read →
      </span>
    </Link>
  );
}

// EmptyBulletin — the no-news variant. Still one Flare giant bulletin on
// the page (the rule), but the headline admits the wire is quiet rather
// than posing as an article. Same shape, same height, no metadata row.
function EmptyBulletin() {
  return (
    <div
      data-newsroom-bulletin-empty
      className="flex items-center justify-center"
      style={{
        background: "var(--color-flare)",
        color: "var(--color-ink)",
        borderRadius: "24px",
        border: "1px solid rgba(11, 11, 11, 0.08)",
        aspectRatio: "1312 / 689",
        minHeight: "clamp(320px, 38vw, 560px)",
        padding: "clamp(28px, 4vw, 72px)",
      }}
    >
      <h2
        style={{
          fontFamily: "'Fraunces', Georgia, serif",
          fontVariationSettings: '"opsz" 144, "SOFT" 30',
          fontWeight: 400,
          fontSize: "clamp(40px, 7vw, 88px)",
          lineHeight: 1.0,
          letterSpacing: "-0.03em",
          color: "var(--color-ink)",
          textAlign: "center",
          maxWidth: "18ch",
          margin: 0,
        }}
      >
        Quiet on the wire.
      </h2>
    </div>
  );
}

// BulletinMeta — the metadata strip under the banner. Ramp embeds the
// title, author, and deck inside the same link; we render it as a quiet
// sibling of the bulletin so the giant card stays the one broadcast moment
// and this row reads as press-release boilerplate.
function BulletinMeta({ item }: { item: NewsroomItem }) {
  return (
    <div
      data-newsroom-bulletin-meta
      className="mt-6 grid grid-cols-1 gap-x-10 gap-y-4 md:mt-8 md:grid-cols-[minmax(0,1.2fr)_minmax(0,1fr)]"
      style={{
        borderTop: "1px solid var(--treatment-hairline)",
        paddingTop: "clamp(18px, 2.2vw, 26px)",
      }}
    >
      <div className="flex flex-col gap-3">
        <p
          className="font-mono text-[11px] font-semibold uppercase tracking-[0.22em]"
          style={{
            color: "var(--treatment-muted-meta)",
            fontVariationSettings: '"wght" 600',
            margin: 0,
          }}
        >
          Article · {item.date}
        </p>
        <h3
          style={{
            fontFamily: "var(--treatment-display-font)",
            fontVariationSettings: '"opsz" 96, "SOFT" 20',
            fontWeight: 400,
            fontSize: "clamp(22px, 2.4vw, 30px)",
            lineHeight: 1.1,
            letterSpacing: "-0.02em",
            color: "var(--treatment-ink)",
            margin: 0,
          }}
        >
          {item.title}
        </h3>
        <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
          <span
            style={{
              fontFamily: "'Fraunces', Georgia, serif",
              fontVariationSettings: '"opsz" 72, "SOFT" 20',
              fontWeight: 400,
              fontSize: "16px",
              color: "var(--treatment-ink)",
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
      </div>
      <p
        style={{
          fontFamily: "'Geist', sans-serif",
          fontSize: "clamp(14px, 1.3vw, 16px)",
          lineHeight: 1.55,
          color: "var(--treatment-muted-strong)",
          margin: 0,
          maxWidth: "52ch",
        }}
      >
        {item.deck}
      </p>
    </div>
  );
}
