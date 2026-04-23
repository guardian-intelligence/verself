import type { ReactNode } from "react";

// NewsroomArchiveCard — the grid-card shape used by /newsroom. Distinct from
// NewsroomCard:
//
//   - NewsroomCard (hero) carries the Lockup on its Flare stripe. That is the
//     ONE place on the Newsroom page where Guardian identifies itself on Flare
//     per the treatment doctrine. An archive grid of twelve little "Guardian"
//     stripes would read as branding spam, not as content.
//   - NewsroomArchiveCard carries no Lockup. Its top band is a shorter Flare
//     bar that paints the kicker/category in Ink — a dateline, not a masthead.
//     The body below is Argent so the Fraunces title reads without fatigue.
//
// Modeled structurally on Ramp's /blog/news archive card: a bold top region
// plus a plain text region below. Ramp fills the top with an illustration; we
// fill it with Flare and a category dateline, which (a) stays on-brand without
// owing art and (b) makes the "Flare means news" rule visible at every card
// position, not just the hero.

export interface NewsroomArchiveCardProps {
  readonly slug: string;
  readonly kicker: ReactNode;
  readonly date: ReactNode;
  readonly title: ReactNode;
  readonly deck?: ReactNode;
  readonly category: string;
  readonly onClick?: () => void;
}

export function NewsroomArchiveCard({
  slug,
  kicker,
  date,
  title,
  deck,
  category,
  onClick,
}: NewsroomArchiveCardProps) {
  return (
    <a
      href={`/newsroom/${slug}`}
      onClick={onClick}
      data-newsroom-archive-card
      data-slug={slug}
      data-category={category}
      style={{
        display: "flex",
        flexDirection: "column",
        overflow: "hidden",
        background: "var(--color-argent)",
        color: "var(--color-ink)",
        border: "1px solid rgba(11, 11, 11, 0.12)",
        borderRadius: "16px",
        textDecoration: "none",
        transition: "transform 160ms ease, box-shadow 160ms ease",
      }}
      className="group hover:-translate-y-0.5 hover:shadow-[0_4px_24px_rgba(11,11,11,0.08)]"
    >
      <div
        style={{
          background: "var(--color-flare)",
          color: "var(--color-ink)",
          padding: "clamp(14px, 2vw, 20px) clamp(18px, 2.4vw, 24px)",
          display: "flex",
          alignItems: "baseline",
          justifyContent: "space-between",
          gap: "12px",
          borderBottom: "1px solid rgba(11, 11, 11, 0.12)",
        }}
      >
        <span
          className="font-mono font-semibold uppercase"
          style={{
            fontSize: "10px",
            letterSpacing: "0.2em",
            fontVariationSettings: '"wght" 600',
            color: "rgba(11, 11, 11, 0.8)",
          }}
        >
          {kicker}
        </span>
        <span
          className="font-mono uppercase"
          style={{
            fontSize: "10px",
            letterSpacing: "0.16em",
            color: "rgba(11, 11, 11, 0.64)",
          }}
        >
          {date}
        </span>
      </div>
      <div
        style={{
          flex: 1,
          padding: "clamp(18px, 2.4vw, 24px) clamp(18px, 2.4vw, 24px) clamp(20px, 2.6vw, 26px)",
          display: "flex",
          flexDirection: "column",
          gap: "10px",
        }}
      >
        <h3
          style={{
            fontFamily: "'Fraunces', Georgia, serif",
            fontVariationSettings: '"opsz" 72, "SOFT" 20',
            fontWeight: 400,
            fontSize: "clamp(20px, 2.2vw, 24px)",
            lineHeight: 1.15,
            letterSpacing: "-0.016em",
            color: "var(--color-ink)",
            margin: 0,
          }}
        >
          {title}
        </h3>
        {deck ? (
          <p
            style={{
              fontFamily: "'Geist', sans-serif",
              fontSize: "14px",
              lineHeight: 1.55,
              color: "rgba(11, 11, 11, 0.62)",
              margin: 0,
            }}
          >
            {deck}
          </p>
        ) : null}
        <span
          aria-hidden="true"
          className="mt-auto font-mono uppercase"
          style={{
            fontSize: "10px",
            letterSpacing: "0.18em",
            color: "rgba(11, 11, 11, 0.55)",
            paddingTop: "14px",
          }}
        >
          Read →
        </span>
      </div>
    </a>
  );
}
