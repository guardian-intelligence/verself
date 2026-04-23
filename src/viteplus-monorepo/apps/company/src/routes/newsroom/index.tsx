import { createFileRoute } from "@tanstack/react-router";
import { useEffect } from "react";
import { WingsEmboss } from "@forge-metal/brand";
import { currentNewsroomItem } from "~/content/newsroom";
import { emitSpan } from "~/lib/telemetry/browser";
import { ogMeta } from "~/lib/head";

// /newsroom — the broadcast surface. When a current bulletin exists, this is
// where it lives in full. When it doesn't, the page is an honest empty state:
// the masthead, the emboss medallion, a single line of copy. Flare is the
// broadcast ground; nothing here pretends to be navigation.

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
    <div className="mx-auto w-full max-w-5xl px-4 py-16 md:px-6 md:py-24">
      <div style={{ marginBottom: "32px" }}>
        <WingsEmboss style={{ width: "clamp(96px, 14vw, 160px)", height: "auto" }} />
      </div>
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
      {item ? (
        <Bulletin item={item} />
      ) : (
        <EmptyState />
      )}
    </div>
  );
}

function Bulletin({
  item,
}: {
  item: NonNullable<ReturnType<typeof currentNewsroomItem>>;
}) {
  return (
    <>
      <p
        className="mt-6 font-mono text-[11px] uppercase tracking-[0.16em]"
        style={{ color: "var(--treatment-muted-meta)" }}
      >
        {item.kicker} · {item.date}
      </p>
      <h1
        className="mt-4 font-display"
        style={{
          fontVariationSettings: '"opsz" 144, "SOFT" 30',
          fontWeight: 400,
          fontSize: "clamp(36px, 6vw, 64px)",
          lineHeight: 1.02,
          letterSpacing: "-0.025em",
          color: "var(--treatment-ink)",
          maxWidth: "22ch",
          margin: 0,
        }}
      >
        {item.title}
      </h1>
      <div className="mt-10">
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
      </div>
    </>
  );
}

function EmptyState() {
  return (
    <>
      <h1
        className="mt-6 font-display"
        style={{
          fontVariationSettings: '"opsz" 144, "SOFT" 30',
          fontWeight: 400,
          fontSize: "clamp(36px, 6vw, 64px)",
          lineHeight: 1.02,
          letterSpacing: "-0.025em",
          color: "var(--treatment-ink)",
          maxWidth: "24ch",
          margin: 0,
        }}
      >
        Quiet on the wire.
      </h1>
      <p
        className="mt-8"
        style={{
          fontFamily: "'Geist', sans-serif",
          fontWeight: 400,
          fontSize: "clamp(16px, 1.7vw, 18px)",
          lineHeight: 1.55,
          color: "var(--treatment-muted-strong)",
          maxWidth: "52ch",
          margin: 0,
        }}
      >
        When Guardian has something worth broadcasting, it lands here. Until then, this space stays
        honest.
      </p>
    </>
  );
}
