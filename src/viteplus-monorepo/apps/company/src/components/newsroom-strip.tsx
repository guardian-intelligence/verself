import { Link } from "@tanstack/react-router";
import { useEffect } from "react";
import {
  currentNewsroomItem,
  newsroomCtaHref,
  newsroomCtaLabel,
} from "~/content/newsroom";
import { emitSpan } from "~/lib/telemetry/browser";

// NewsroomStrip — a bounded Flare broadcast band shown on the homepage (and
// anywhere else Guardian wants to speak in the public register) whenever a
// current newsroom item exists. Renders nothing when there is no content —
// the band is conditional by construction so Flare on the homepage always
// carries news, never decoration.
//
// Shape: ~140px tall, full-bleed width, ink text on Flare, inverted black
// CTA. The strip intentionally does NOT flip data-treatment — under the
// three-room model, the Newsroom SCOPE is a Paper register, not a Flare
// one. Flare appears here as an explicit broadcast band layered on top of
// the host chrome, not as a treatment ground. That invariant lets the band
// embed cleanly inside any host (Workshop homepage today, Letters footer
// tomorrow) without dragging the wrong token ramp with it.

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
      aria-label="From the Newsroom"
      style={{
        background: "var(--color-flare)",
        color: "var(--color-ink)",
        borderTop: "1px solid rgba(11, 11, 11, 0.14)",
        borderBottom: "1px solid rgba(11, 11, 11, 0.14)",
      }}
    >
      <div className="mx-auto flex w-full max-w-7xl flex-col gap-4 px-4 py-8 md:flex-row md:items-center md:justify-between md:gap-8 md:px-6 md:py-10">
        <div className="flex flex-col gap-2">
          <p
            className="font-mono text-[10px] font-semibold uppercase tracking-[0.2em]"
            style={{
              color: "rgba(11, 11, 11, 0.72)",
              fontVariationSettings: '"wght" 600',
              margin: 0,
            }}
          >
            From the Newsroom · {item.kicker} · {item.date}
          </p>
          <p
            style={{
              // Fraunces inside the Newsroom band — read via the treatment
              // display token so the strip stays consistent if it's ever
              // embedded under a scope that binds Geist to this var.
              fontFamily: "var(--treatment-display-font, 'Fraunces', Georgia, serif)",
              fontVariationSettings: '"opsz" 96, "SOFT" 20',
              fontWeight: 400,
              fontSize: "clamp(22px, 2.8vw, 32px)",
              lineHeight: 1.1,
              letterSpacing: "-0.02em",
              margin: 0,
              color: "var(--color-ink)",
              maxWidth: "28ch",
            }}
          >
            {item.title}
          </p>
        </div>
        <div className="shrink-0">
          <Link
            to={newsroomCtaHref(item)}
            onClick={() =>
              emitSpan("newsroom.strip.cta_click", {
                slug: item.slug,
                destination: newsroomCtaHref(item),
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
            {newsroomCtaLabel(item)} →
          </Link>
        </div>
      </div>
    </aside>
  );
}
