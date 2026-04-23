import { createFileRoute } from "@tanstack/react-router";
import { useEffect } from "react";
import { currentNewsroomItem } from "~/content/newsroom";
import { NewsroomCard } from "~/features/design/newsroom-card";
import { emitSpan } from "~/lib/telemetry/browser";
import { ogMeta } from "~/lib/head";

// /newsroom — a calm Argent canvas holding one hero NewsroomCard + an
// archive grid. Layout borrows Ramp's blog/news page: small label at top,
// one featured bulletin card, a "Latest" grid below. The card's Flare top
// stripe is where the logo + wordmark live (Newsroom rule: Guardian is
// always on Flare). The card body stays Argent so the Fraunces title is
// legible and the reader's eye isn't fatigued by a full-field of green.

export const Route = createFileRoute("/newsroom/")({
  component: NewsroomIndex,
  head: () => ({
    meta: ogMeta({
      slug: "home",
      title: "Newsroom — Guardian",
      description: "Bulletins and announcements from Guardian Intelligence.",
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
      slug: item?.slug ?? "",
    });
  }, [item]);

  return (
    <div className="mx-auto w-full max-w-6xl px-4 py-10 md:px-6 md:py-14">
      <NewsroomLabel />
      <div className="mt-8">
        {item ? (
          <NewsroomCard
            size="hero"
            ariaLabel={`Bulletin: ${item.title}`}
            kicker={`${item.kicker} · ${item.date}`}
            title={item.title}
            cta={{
              label: item.ctaLabel,
              href: item.ctaHref,
              onClick: () =>
                emitSpan("newsroom.index.cta_click", {
                  slug: item.slug,
                  destination: item.ctaHref,
                }),
            }}
          />
        ) : (
          <EmptyHero />
        )}
      </div>
      <ArchiveStub />
    </div>
  );
}

function NewsroomLabel() {
  return (
    <div className="flex flex-col gap-3">
      <h1
        style={{
          fontFamily: "'Geist', sans-serif",
          fontWeight: 500,
          fontSize: "clamp(28px, 3.2vw, 36px)",
          lineHeight: 1.1,
          letterSpacing: "-0.018em",
          color: "var(--color-ink)",
          margin: 0,
        }}
      >
        Newsroom
      </h1>
      <p
        className="max-w-2xl"
        style={{
          fontFamily: "'Geist', sans-serif",
          fontSize: "15px",
          lineHeight: 1.55,
          color: "rgba(11,11,11,0.6)",
          margin: 0,
        }}
      >
        Bulletins, milestones, and public notes from Guardian Intelligence.
      </p>
    </div>
  );
}

// Empty hero — keeps the NewsroomCard shape (Flare stripe + Argent body)
// so the page rhythm holds even when there is no current bulletin. Title
// swaps to an honest "Quiet on the wire." string; no CTA.
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

function ArchiveStub() {
  return (
    <section className="mt-16" aria-label="Bulletin archive">
      <p
        className="font-mono text-[11px] font-semibold uppercase tracking-[0.18em]"
        style={{
          color: "rgba(11,11,11,0.55)",
          fontVariationSettings: '"wght" 600',
          margin: "0 0 14px",
        }}
      >
        Archive
      </p>
      <p
        style={{
          borderTop: "1px solid rgba(11,11,11,0.12)",
          paddingTop: "24px",
          fontFamily: "'Geist', sans-serif",
          fontSize: "14px",
          lineHeight: 1.55,
          color: "rgba(11,11,11,0.6)",
          maxWidth: "52ch",
          margin: 0,
        }}
      >
        Guardian speaks rarely. When the second bulletin files, it will land above this line and
        the grid will fill in under it.
      </p>
    </section>
  );
}
