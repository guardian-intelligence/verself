import { Link } from "@tanstack/react-router";
import { useEffect } from "react";
import { currentNewsroomItem } from "~/content/newsroom";
import { emitSpan } from "~/lib/telemetry/browser";

// NewsroomStrip — a full-bleed Flare band that appears on the homepage (and
// anywhere else Guardian wants to speak in the public register) whenever a
// current newsroom item exists. Renders nothing when there is no content —
// the component is conditional by construction so Flare on the homepage
// always carries news, never decoration.
//
// Shape: ~140px tall, full-bleed-at-viewport width (max-w-none, horizontal
// padding only), ink text on acid-green ground, black CTA button. Reads
// --treatment-* in the "newsroom" scope so every colour decision is token-
// driven; the band could move to any Flare surface (OG cards, press pages)
// without re-authoring its internals.

export function NewsroomStrip() {
  const item = currentNewsroomItem();

  useEffect(() => {
    if (!item) return;
    if (typeof window === "undefined") return;
    emitSpan("newsroom.strip.view", {
      slug: item.slug,
      title: item.title,
    });
  }, [item]);

  if (!item) return null;

  return (
    <aside
      data-treatment="newsroom"
      aria-label="From the Newsroom"
      style={{
        background: "var(--treatment-ground)",
        color: "var(--treatment-ink)",
        borderTop: "1px solid var(--treatment-hairline)",
        borderBottom: "1px solid var(--treatment-hairline)",
      }}
    >
      <div className="mx-auto flex w-full max-w-7xl flex-col gap-4 px-4 py-8 md:flex-row md:items-center md:justify-between md:gap-8 md:px-6 md:py-10">
        <div className="flex flex-col gap-2">
          <p
            className="font-mono text-[10px] font-semibold uppercase tracking-[0.2em]"
            style={{
              color: "var(--treatment-muted)",
              fontVariationSettings: '"wght" 600',
              margin: 0,
            }}
          >
            From the Newsroom · {item.kicker} · {item.date}
          </p>
          <p
            className="font-display"
            style={{
              fontVariationSettings: '"opsz" 96, "SOFT" 20',
              fontWeight: 400,
              fontSize: "clamp(22px, 2.8vw, 32px)",
              lineHeight: 1.1,
              letterSpacing: "-0.02em",
              margin: 0,
              color: "var(--treatment-ink)",
              maxWidth: "28ch",
            }}
          >
            {item.title}
          </p>
        </div>
        <div className="shrink-0">
          <Link
            to={item.ctaHref}
            onClick={() =>
              emitSpan("newsroom.strip.cta_click", {
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
          </Link>
        </div>
      </div>
    </aside>
  );
}
