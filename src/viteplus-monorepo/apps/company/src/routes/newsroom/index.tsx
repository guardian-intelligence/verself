import { createFileRoute } from "@tanstack/react-router";
import { useEffect, useMemo, useState, useCallback, type FormEvent } from "react";
import {
  CATEGORY_LABELS,
  NEWSROOM_META,
  type NewsroomCategory,
  type NewsroomItem,
  newsroomCtaHref,
  newsroomCtaLabel,
  sortedNewsroomItems,
} from "~/content/newsroom";
import { NewsroomCard } from "~/features/design/newsroom-card";
import { NewsroomArchiveCard } from "~/features/design/newsroom-archive-card";
import { emitSpan } from "~/lib/telemetry/browser";
import { ogMeta } from "~/lib/head";

// /newsroom — the press-room index.
//
// Structural debt to Ramp's /blog/news: masthead + featured hero + "Latest"
// tab strip + archive grid + subscribe band. We take the rhythm, not the
// palette. Argent body, Fraunces serif, Flare reserved for the hero card's
// masthead stripe and the subscribe band's ground. Everything else stays
// quiet so the two Flare events on the page can be heard.
//
// Tabs, pagination, and the subscribe form are hand-rolled rather than
// reaching for shadcn primitives: the shadcn variants drive their palettes
// from the Tailwind @theme tokens (primary/input/etc.), which don't align
// with the per-treatment --treatment-* ramp that every other Newsroom
// surface reads from. Rolling the controls here keeps the Newsroom scope
// self-consistent and avoids a layer of CSS overrides that would regress
// the moment the shadcn components change their internal classes.
//
// Pagination is currently a static single-page scaffold (the archive has
// four entries). The markup and the span emitter are wired so the day a
// thirteenth bulletin lands we can switch PAGE_SIZE, slice by page index,
// and the control surface already exists.

const PAGE_SIZE = 9;

type TabValue = "all" | NewsroomCategory;

const TABS: ReadonlyArray<{ readonly value: TabValue; readonly label: string }> = [
  { value: "all", label: "All" },
  { value: "announcement", label: CATEGORY_LABELS.announcement },
  { value: "milestone", label: CATEGORY_LABELS.milestone },
  { value: "note", label: CATEGORY_LABELS.note },
];

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
  const items = sortedNewsroomItems();
  const featured = items[0];
  const archive = items.slice(1);

  const [activeTab, setActiveTab] = useState<TabValue>("all");

  const visibleArchive = useMemo(
    () => (activeTab === "all" ? archive : archive.filter((item) => item.category === activeTab)),
    [archive, activeTab],
  );

  useEffect(() => {
    if (typeof window === "undefined") return;
    emitSpan("newsroom.index.view", {
      item_count: String(items.length),
      featured_slug: featured?.slug ?? "",
      has_item: featured ? "true" : "false",
    });
  }, [items.length, featured]);

  const handleTabChange = useCallback(
    (next: TabValue) => {
      setActiveTab((prev) => {
        if (prev === next) return prev;
        const visibleCount =
          next === "all" ? archive.length : archive.filter((item) => item.category === next).length;
        emitSpan("newsroom.index.tab_change", {
          from_tab: prev,
          to_tab: next,
          visible_count: String(visibleCount),
        });
        return next;
      });
    },
    [archive],
  );

  const handleCardClick = useCallback(
    (item: NewsroomItem, position: number, kind: "hero" | "archive") => {
      emitSpan("newsroom.index.card_click", {
        slug: item.slug,
        position: String(position),
        card_kind: kind,
        destination: newsroomCtaHref(item),
      });
    },
    [],
  );

  return (
    <div className="mx-auto w-full max-w-6xl px-4 py-10 md:px-6 md:py-14">
      <Masthead />
      <section aria-label="Featured bulletin" className="mt-10 md:mt-14">
        {featured ? (
          <NewsroomCard
            size="hero"
            ariaLabel={`Bulletin: ${featured.title}`}
            kicker={`${featured.kicker} · ${featured.date}`}
            title={featured.title}
            blurb={featured.deck}
            cta={{
              label: newsroomCtaLabel(featured),
              href: newsroomCtaHref(featured),
              onClick: () => handleCardClick(featured, 0, "hero"),
            }}
          />
        ) : (
          <EmptyHero />
        )}
      </section>

      <LatestSection
        archive={archive}
        visible={visibleArchive.slice(0, PAGE_SIZE)}
        activeTab={activeTab}
        onTabChange={handleTabChange}
        onCardClick={handleCardClick}
      />

      <SubscribeBand />
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

function EmptyHero() {
  return (
    <NewsroomCard
      size="hero"
      ariaLabel="No current bulletin"
      kicker="No current bulletin"
      title="Quiet on the wire."
      blurb="When Guardian has something worth broadcasting, it lands here. Until then, this space stays honest."
    />
  );
}

function LatestSection({
  archive,
  visible,
  activeTab,
  onTabChange,
  onCardClick,
}: {
  archive: readonly NewsroomItem[];
  visible: readonly NewsroomItem[];
  activeTab: TabValue;
  onTabChange: (next: TabValue) => void;
  onCardClick: (item: NewsroomItem, position: number, kind: "hero" | "archive") => void;
}) {
  if (archive.length === 0) return null;

  return (
    <section aria-label="Latest bulletins" className="mt-16 md:mt-20">
      <div className="flex flex-wrap items-baseline justify-between gap-4">
        <h2
          style={{
            fontFamily: "var(--treatment-display-font)",
            fontVariationSettings: '"opsz" 96, "SOFT" 20',
            fontWeight: 400,
            fontSize: "clamp(28px, 3vw, 36px)",
            lineHeight: 1.1,
            letterSpacing: "-0.02em",
            color: "var(--treatment-ink)",
            margin: 0,
          }}
        >
          Latest
        </h2>
        <TabStrip activeTab={activeTab} onChange={onTabChange} />
      </div>

      <div className="mt-8 grid grid-cols-1 gap-5 md:mt-10 md:grid-cols-2 md:gap-6 lg:grid-cols-3">
        {visible.map((item, idx) => (
          <NewsroomArchiveCard
            key={item.slug}
            slug={item.slug}
            kicker={item.kicker}
            date={item.date}
            title={item.title}
            deck={item.deck}
            category={item.category}
            onClick={() => onCardClick(item, idx + 1, "archive")}
          />
        ))}
        {visible.length === 0 ? <EmptyTabPanel /> : null}
      </div>

      <PaginationStub itemCount={archive.length} pageSize={PAGE_SIZE} />
    </section>
  );
}

function TabStrip({
  activeTab,
  onChange,
}: {
  activeTab: TabValue;
  onChange: (next: TabValue) => void;
}) {
  return (
    <div
      role="tablist"
      aria-label="Filter bulletins"
      data-newsroom-tabstrip
      className="flex flex-wrap items-center gap-1.5"
      style={{
        padding: "4px",
        borderRadius: "999px",
        border: "1px solid var(--treatment-surface-border)",
        background: "var(--treatment-surface-subtle)",
      }}
    >
      {TABS.map((tab) => {
        const isActive = tab.value === activeTab;
        return (
          <button
            key={tab.value}
            type="button"
            role="tab"
            aria-selected={isActive}
            data-tab-value={tab.value}
            onClick={() => onChange(tab.value)}
            className="font-mono uppercase transition-colors"
            style={{
              fontSize: "11px",
              letterSpacing: "0.16em",
              padding: "8px 14px",
              borderRadius: "999px",
              border: "none",
              cursor: "pointer",
              background: isActive ? "var(--color-flare)" : "transparent",
              color: isActive ? "var(--color-ink)" : "var(--treatment-muted)",
              fontWeight: isActive ? 600 : 500,
            }}
          >
            {tab.label}
          </button>
        );
      })}
    </div>
  );
}

function EmptyTabPanel() {
  return (
    <p
      className="col-span-full"
      style={{
        fontFamily: "'Geist', sans-serif",
        fontSize: "14px",
        lineHeight: 1.55,
        color: "var(--treatment-muted-meta)",
        borderTop: "1px solid var(--treatment-hairline)",
        paddingTop: "18px",
        margin: 0,
      }}
    >
      Nothing in this category yet. Check the other tabs.
    </p>
  );
}

function PaginationStub({ itemCount, pageSize }: { itemCount: number; pageSize: number }) {
  const totalPages = Math.max(1, Math.ceil(itemCount / pageSize));
  const showPagination = itemCount > pageSize;

  return (
    <nav
      aria-label="Bulletin archive pages"
      data-newsroom-pagination
      className="mt-10 flex items-center justify-center gap-2"
      style={{ color: "var(--treatment-muted)" }}
    >
      <PaginationArrow label="Previous" direction="prev" disabled />
      {Array.from({ length: totalPages }, (_, i) => {
        const isActive = i === 0;
        return (
          <button
            key={i}
            type="button"
            aria-current={isActive ? "page" : undefined}
            disabled={!showPagination}
            className="font-mono"
            style={{
              fontSize: "12px",
              minWidth: "32px",
              height: "32px",
              padding: "0 10px",
              borderRadius: "8px",
              border: `1px solid ${isActive ? "var(--treatment-surface-border)" : "transparent"}`,
              background: isActive ? "var(--treatment-surface-subtle)" : "transparent",
              color: isActive ? "var(--treatment-ink)" : "var(--treatment-muted-meta)",
              cursor: showPagination ? "pointer" : "default",
            }}
          >
            {i + 1}
          </button>
        );
      })}
      <PaginationArrow label="Next" direction="next" disabled={!showPagination} />
    </nav>
  );
}

function PaginationArrow({
  label,
  direction,
  disabled,
}: {
  label: string;
  direction: "prev" | "next";
  disabled?: boolean;
}) {
  return (
    <button
      type="button"
      aria-label={label}
      disabled={disabled}
      className="font-mono uppercase"
      style={{
        fontSize: "11px",
        letterSpacing: "0.16em",
        padding: "0 12px",
        height: "32px",
        borderRadius: "8px",
        border: "1px solid transparent",
        background: "transparent",
        color: "var(--treatment-muted-meta)",
        opacity: disabled ? 0.4 : 1,
        cursor: disabled ? "default" : "pointer",
      }}
    >
      {direction === "prev" ? "← Prev" : "Next →"}
    </button>
  );
}

// Subscribe band — Flare ground, Ink type, one affordance (email + submit).
// Newsletter Service is on the roadmap (see CLAUDE.md → Planned Upcoming
// Projects); until that lands, submit emits a span with outcome="noop_stub"
// and we keep the control honest by showing copy that admits the subscribe
// path isn't wired yet. When the newsletter service ships, this becomes a
// real POST and the span outcome flips to "submitted"/"error".
function SubscribeBand() {
  const onSubmit = useCallback((event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    emitSpan("newsroom.index.subscribe_submit", {
      outcome: "noop_stub",
    });
    const form = event.currentTarget;
    form.reset();
  }, []);

  return (
    <section
      aria-label="Subscribe to bulletins"
      data-newsroom-subscribe
      className="mt-20 overflow-hidden"
      style={{
        background: "var(--color-flare)",
        color: "var(--color-ink)",
        border: "1px solid rgba(11, 11, 11, 0.12)",
        borderRadius: "20px",
      }}
    >
      <div className="flex flex-col gap-6 px-6 py-10 md:flex-row md:items-end md:justify-between md:gap-10 md:px-10 md:py-12">
        <div className="flex flex-col gap-3 md:max-w-xl">
          <p
            className="font-mono text-[11px] font-semibold uppercase tracking-[0.2em]"
            style={{
              color: "rgba(11, 11, 11, 0.7)",
              fontVariationSettings: '"wght" 600',
              margin: 0,
            }}
          >
            Subscribe
          </p>
          <h2
            style={{
              fontFamily: "'Fraunces', Georgia, serif",
              fontVariationSettings: '"opsz" 96, "SOFT" 20',
              fontWeight: 400,
              fontSize: "clamp(28px, 3.4vw, 40px)",
              lineHeight: 1.08,
              letterSpacing: "-0.022em",
              color: "var(--color-ink)",
              margin: 0,
            }}
          >
            Bulletins in your inbox.
          </h2>
          <p
            style={{
              fontFamily: "'Geist', sans-serif",
              fontSize: "14px",
              lineHeight: 1.55,
              color: "rgba(11, 11, 11, 0.7)",
              margin: 0,
            }}
          >
            We broadcast rarely. Leave an address and every new bulletin lands in your inbox the day
            it files.
          </p>
        </div>
        <form
          onSubmit={onSubmit}
          className="flex w-full flex-col gap-3 sm:flex-row sm:items-center md:w-auto md:min-w-[380px]"
          noValidate
        >
          <label htmlFor="newsroom-subscribe-email" className="sr-only">
            Email address
          </label>
          <input
            id="newsroom-subscribe-email"
            type="email"
            name="email"
            required
            placeholder="you@example.com"
            autoComplete="email"
            className="font-sans"
            style={{
              flex: 1,
              height: "44px",
              padding: "0 14px",
              borderRadius: "10px",
              border: "1px solid rgba(11, 11, 11, 0.2)",
              background: "var(--color-argent)",
              color: "var(--color-ink)",
              fontSize: "14px",
              outline: "none",
            }}
          />
          <button
            type="submit"
            className="font-sans"
            style={{
              height: "44px",
              padding: "0 20px",
              borderRadius: "10px",
              border: "none",
              background: "var(--color-ink)",
              color: "var(--color-flare)",
              fontSize: "14px",
              fontWeight: 500,
              cursor: "pointer",
            }}
          >
            Subscribe →
          </button>
        </form>
      </div>
    </section>
  );
}
