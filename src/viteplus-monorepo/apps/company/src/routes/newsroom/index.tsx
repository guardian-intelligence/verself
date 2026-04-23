import { createFileRoute } from "@tanstack/react-router";
import { useEffect } from "react";
import { WingsEmboss } from "@forge-metal/brand";
import { currentNewsroomItem } from "~/content/newsroom";
import { emitSpan } from "~/lib/telemetry/browser";
import { ogMeta } from "~/lib/head";

// /newsroom — the broadcast surface. Under the three-room model, Newsroom is
// a Paper reading ground hosting a bounded Flare hero band that carries the
// current bulletin. Flare is never the page ground; it appears inside the
// band and in the CTA. When there is no current item the page renders an
// honest empty state on Paper — no Flare at all, because there is nothing
// to broadcast.

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

  return item ? <BulletinSurface item={item} /> : <EmptySurface />;
}

function BulletinSurface({
  item,
}: {
  item: NonNullable<ReturnType<typeof currentNewsroomItem>>;
}) {
  return (
    <div className="mx-auto w-full max-w-5xl px-4 py-12 md:px-6 md:py-16">
      <p
        className="font-mono text-[11px] font-semibold uppercase tracking-[0.2em]"
        style={{
          color: "var(--treatment-muted)",
          fontVariationSettings: '"wght" 600',
          margin: 0,
        }}
      >
        Newsroom · Guardian Intelligence
      </p>
      {/* Bounded Flare hero band. The band is the one Flare surface on the
          page — everything else sits on Paper. It carries the bulletin the
          way a magazine cover carries the lead story: one loud field, one
          quiet kicker, one ink CTA. */}
      <section
        aria-label={`Bulletin: ${item.title}`}
        style={{
          marginTop: "32px",
          background: "var(--color-flare)",
          color: "var(--color-ink)",
          borderRadius: "16px",
          border: "1px solid rgba(11, 11, 11, 0.14)",
          padding: "clamp(28px, 4.5vw, 56px) clamp(20px, 4vw, 48px)",
          maxHeight: "480px",
          overflow: "hidden",
          position: "relative",
        }}
      >
        <div style={{ marginBottom: "22px" }}>
          <WingsEmboss style={{ width: "clamp(44px, 6vw, 64px)", height: "auto" }} />
        </div>
        <p
          className="font-mono text-[11px] font-semibold uppercase tracking-[0.18em]"
          style={{
            color: "rgba(11, 11, 11, 0.72)",
            fontVariationSettings: '"wght" 600',
            margin: "0 0 10px",
          }}
        >
          {item.kicker} · {item.date}
        </p>
        <h1
          style={{
            fontFamily: "'Fraunces', Georgia, serif",
            fontVariationSettings: '"opsz" 144, "SOFT" 30',
            fontWeight: 400,
            fontSize: "clamp(32px, 5vw, 52px)",
            lineHeight: 1.02,
            letterSpacing: "-0.025em",
            color: "var(--color-ink)",
            maxWidth: "24ch",
            margin: "0 0 22px",
          }}
        >
          {item.title}
        </h1>
        <a
          href={item.ctaHref}
          onClick={() =>
            emitSpan("newsroom.index.cta_click", {
              slug: item.slug,
              destination: item.ctaHref,
            })
          }
          style={{
            display: "inline-flex",
            alignItems: "center",
            gap: "8px",
            fontFamily: "'Geist', sans-serif",
            fontWeight: 500,
            fontSize: "14px",
            padding: "12px 20px",
            borderRadius: "8px",
            background: "var(--color-iron)",
            color: "var(--color-flare)",
            border: "1px solid var(--color-iron)",
          }}
        >
          {item.ctaLabel} →
        </a>
      </section>
    </div>
  );
}

function EmptySurface() {
  return (
    <div className="mx-auto w-full max-w-5xl px-4 py-16 md:px-6 md:py-24">
      <p
        className="font-mono text-[11px] font-semibold uppercase tracking-[0.2em]"
        style={{
          color: "var(--treatment-muted)",
          fontVariationSettings: '"wght" 600',
          margin: 0,
        }}
      >
        Newsroom · Guardian Intelligence
      </p>
      <h1
        style={{
          fontFamily: "'Fraunces', Georgia, serif",
          fontVariationSettings: '"opsz" 144, "SOFT" 30',
          fontWeight: 400,
          fontSize: "clamp(36px, 6vw, 64px)",
          lineHeight: 1.02,
          letterSpacing: "-0.025em",
          color: "var(--treatment-ink)",
          maxWidth: "24ch",
          margin: "24px 0 0",
        }}
      >
        Quiet on the wire.
      </h1>
      <p
        style={{
          fontFamily: "'Geist', sans-serif",
          fontWeight: 400,
          fontSize: "clamp(16px, 1.7vw, 18px)",
          lineHeight: 1.55,
          color: "var(--treatment-muted-strong)",
          maxWidth: "52ch",
          margin: "28px 0 0",
        }}
      >
        When Guardian has something worth broadcasting, it lands here. Until then, this space stays
        honest.
      </p>
    </div>
  );
}
