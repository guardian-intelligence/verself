import type { ReactNode } from "react";
import { Lockup } from "@forge-metal/brand";

// NewsroomCard — the single card shape that every Newsroom surface uses.
//
// Hero cards (`size="hero"`) stack two registers:
//   1. Flare stripe (top) carrying the Lockup (WingsEmboss + "Guardian" in
//      Fraunces, Ink wordmark). The wordmark and logo are ALWAYS on Flare
//      on Newsroom; the featured card is where that rule is made visible.
//   2. Argent body (below) — kicker, Fraunces title, optional blurb, and
//      one primary Flare-on-Ink CTA. The body stays white so the Fraunces
//      title is legible and the reader's eye isn't fatigued.
//
// Standard cards (`size="standard"`) skip the stripe. An archive grid of
// ten little "Guardian" stripes reads as branding spam, not as content —
// the house mark appears once on the page, in the featured card, and the
// archive cards stay quiet so the reader can scan titles.
//
// The primary CTA paints Flare with Ink text — the Flare stripe above
// rhymes with the CTA below, tying the featured card's colour system
// together at two scales.

export type NewsroomCardSize = "hero" | "standard";

export interface NewsroomCardProps {
  readonly size?: NewsroomCardSize;
  readonly kicker: ReactNode;
  readonly title: ReactNode;
  readonly blurb?: ReactNode;
  readonly cta?: {
    readonly label: ReactNode;
    readonly href: string;
    readonly onClick?: () => void;
  };
  readonly ariaLabel?: string;
}

export function NewsroomCard({
  size = "standard",
  kicker,
  title,
  blurb,
  cta,
  ariaLabel,
}: NewsroomCardProps) {
  const isHero = size === "hero";
  return (
    <article
      aria-label={ariaLabel}
      style={{
        background: "var(--color-argent)",
        color: "var(--color-ink)",
        border: "1px solid rgba(11,11,11,0.12)",
        borderRadius: isHero ? "20px" : "14px",
        overflow: "hidden",
        display: "flex",
        flexDirection: "column",
        minHeight: isHero ? undefined : "100%",
      }}
    >
      {isHero ? <NewsroomCardStripe /> : null}
      <NewsroomCardBody
        size={size}
        kicker={kicker}
        title={title}
        blurb={blurb}
        cta={cta}
      />
    </article>
  );
}

function NewsroomCardStripe() {
  return (
    <div
      style={{
        background: "var(--color-flare)",
        color: "var(--color-ink)",
        padding: "clamp(18px, 2.4vw, 26px) clamp(22px, 3vw, 36px)",
      }}
    >
      <Lockup size="sm" variant="emboss" wordmarkColor="var(--color-ink)" />
    </div>
  );
}

function NewsroomCardBody({
  size,
  kicker,
  title,
  blurb,
  cta,
}: {
  size: NewsroomCardSize;
  kicker: ReactNode;
  title: ReactNode;
  blurb?: ReactNode;
  cta?: NewsroomCardProps["cta"];
}) {
  const isHero = size === "hero";
  return (
    <div
      style={{
        flex: 1,
        display: "grid",
        gap: isHero ? "clamp(20px, 2.4vw, 28px)" : "10px",
        padding: isHero
          ? "clamp(24px, 3vw, 36px) clamp(22px, 3vw, 36px)"
          : "18px 18px 20px",
        gridTemplateColumns: isHero ? "minmax(0, 1fr) auto" : "minmax(0, 1fr)",
        alignItems: isHero ? "end" : "start",
      }}
    >
      <div className="flex flex-col" style={{ gap: isHero ? "12px" : "8px", minWidth: 0 }}>
        <p
          className="font-mono font-semibold uppercase"
          style={{
            fontSize: isHero ? "11px" : "10px",
            letterSpacing: "0.18em",
            fontVariationSettings: '"wght" 600',
            color: "rgba(11,11,11,0.6)",
            margin: 0,
          }}
        >
          {kicker}
        </p>
        <h3
          style={{
            fontFamily: "'Fraunces', Georgia, serif",
            fontVariationSettings: isHero
              ? '"opsz" 144, "SOFT" 30'
              : '"opsz" 72, "SOFT" 20',
            fontWeight: 400,
            fontSize: isHero
              ? "clamp(32px, 4.8vw, 56px)"
              : "20px",
            lineHeight: isHero ? 1.0 : 1.15,
            letterSpacing: isHero ? "-0.026em" : "-0.015em",
            color: "var(--color-ink)",
            maxWidth: isHero ? "22ch" : undefined,
            margin: 0,
          }}
        >
          {title}
        </h3>
        {blurb ? (
          <p
            style={{
              fontFamily: "'Geist', sans-serif",
              fontSize: isHero ? "15px" : "13px",
              lineHeight: 1.55,
              color: "rgba(11,11,11,0.6)",
              margin: 0,
              maxWidth: isHero ? "56ch" : undefined,
            }}
          >
            {blurb}
          </p>
        ) : null}
        {!isHero && cta ? <NewsroomCardCTA cta={cta} size={size} /> : null}
      </div>
      {isHero && cta ? (
        <div className="flex items-end justify-start md:justify-end">
          <NewsroomCardCTA cta={cta} size={size} />
        </div>
      ) : null}
    </div>
  );
}

function NewsroomCardCTA({
  cta,
  size,
}: {
  cta: NonNullable<NewsroomCardProps["cta"]>;
  size: NewsroomCardSize;
}) {
  const isHero = size === "hero";
  return (
    <a
      href={cta.href}
      onClick={cta.onClick}
      className="rounded-md"
      style={{
        display: "inline-flex",
        alignItems: "center",
        gap: "8px",
        fontFamily: "'Geist', sans-serif",
        fontWeight: 500,
        fontSize: isHero ? "14px" : "13px",
        padding: isHero ? "10px 18px" : "8px 14px",
        background: "var(--color-flare)",
        color: "var(--color-ink)",
        border: "none",
        whiteSpace: "nowrap",
        width: "fit-content",
      }}
    >
      {cta.label}
      <span aria-hidden="true">→</span>
    </a>
  );
}
