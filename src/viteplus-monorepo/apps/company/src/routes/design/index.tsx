import { createFileRoute, Link } from "@tanstack/react-router";
import { useEffect } from "react";
import type { Treatment } from "@forge-metal/brand";
import { Lockup, WingsChip, WingsEmboss } from "@forge-metal/brand";
import { emitSpan } from "~/lib/telemetry/browser";
import { AppliedFooter } from "~/features/design/sections/applied-footer";
import { ogMeta } from "~/lib/head";

// /design overview. The landing surface for the brand system: four
// treatment cards — each rendered in its own palette with a placeholder
// letterbox (a real photograph slot the founder will fill later) — plus
// the Applied cross-treatment rules at the bottom as a footer section.
//
// Default staticData.treatment is "company" so __root renders Iron chrome
// at this page. The treatment cards each navigate into their own surface
// where the chrome repaints.

export const Route = createFileRoute("/design/")({
  component: DesignOverview,
  staticData: { treatment: "company" as const },
  head: () => ({
    meta: ogMeta({
      slug: "design",
      title: "Guardian — Brand system",
      description:
        "The Guardian brand system: four treatments — Company, Workshop, Newsroom, Letters — each a room with its own palette, type, and mark usage.",
    }),
    links: [{ rel: "canonical", href: "/design" }],
  }),
});

interface TreatmentCard {
  readonly treatment: Treatment;
  readonly to: "/design/company" | "/design/workshop" | "/design/newsroom" | "/design/letters";
  readonly number: string;
  readonly title: string;
  readonly subtitle: string;
  readonly description: string;
}

// Brand system — one H1 per route. The overview title is deliberately
// plain; voice pass will upgrade it.
const CARDS: readonly TreatmentCard[] = [
  {
    treatment: "company",
    to: "/design/company",
    number: "01",
    title: "Company",
    subtitle: "The record.",
    description:
      "Iron ground, Fraunces masthead, Flare as the single action. This is how Guardian appears when it is speaking for itself — the landing, the mission, the press contact.",
  },
  {
    treatment: "workshop",
    to: "/design/workshop",
    number: "02",
    title: "Workshop",
    subtitle: "Where the work happens.",
    description:
      "Iron ground, Geist throughout, Amber as the sole accent. Fraunces is absent; the chrome carries the tenant's name, not ours. A quiet identity anchor for people who are trying to get something done.",
  },
  {
    treatment: "newsroom",
    to: "/design/newsroom",
    number: "03",
    title: "Newsroom",
    subtitle: "The broadcast.",
    description:
      "Flare ground, wings inside a circular ink emboss, Fraunces at display weight. Guardian when it needs to be seen in someone else's feed — OG cards, share previews, conference backdrops.",
  },
  {
    treatment: "letters",
    to: "/design/letters",
    number: "04",
    title: "Letters",
    subtitle: "The long form.",
    description:
      "Paper ground, Fraunces body, Bordeaux as the editorial accent. Where individual voices inside Guardian show their work — an engineer's postmortem, the founder's note, a milestone retold in full.",
  },
];

function DesignOverview() {
  useEffect(() => {
    if (typeof window === "undefined") return;
    emitSpan("design.overview.view", {
      referrer: document.referrer ?? "",
      viewport_width: String(window.innerWidth),
    });
  }, []);

  return (
    <div
      style={{
        background: "var(--treatment-ground)",
        color: "var(--treatment-ink)",
        minHeight: "100vh",
      }}
    >
      <div className="mx-auto w-full max-w-7xl px-4 py-14 md:px-6 md:py-20">
        <header className="mb-14 flex flex-col gap-4">
          <p
            className="font-mono text-[11px] font-semibold uppercase tracking-[0.16em]"
            style={{ color: "var(--treatment-muted)", fontVariationSettings: '"wght" 600' }}
          >
            Guardian · Brand system
          </p>
          <h1
            className="font-display"
            style={{
              fontVariationSettings: '"opsz" 144, "SOFT" 30',
              fontWeight: 400,
              fontSize: "clamp(36px, 5vw, 56px)",
              lineHeight: 1.02,
              letterSpacing: "-0.026em",
              margin: 0,
              maxWidth: "20ch",
            }}
          >
            Four rooms, one house.
          </h1>
          <p
            className="max-w-3xl"
            style={{
              color: "var(--treatment-muted-strong)",
              fontSize: "17px",
              lineHeight: 1.55,
              margin: 0,
            }}
          >
            The wings stay Argent on every ground. Every other decision — palette, typography, mark
            variant, lockup — belongs to the treatment. Walk into each room to see it inhabited, not
            described.
          </p>
        </header>

        <div className="grid gap-6 md:grid-cols-2">
          {CARDS.map((card) => (
            <OverviewCard key={card.treatment} card={card} />
          ))}
        </div>

        <div className="mt-20">
          <AppliedFooter />
        </div>
      </div>
    </div>
  );
}

function OverviewCard({ card }: { card: TreatmentCard }) {
  return (
    <Link
      to={card.to}
      data-treatment={card.treatment}
      className="group block overflow-hidden rounded-xl transition-colors"
      style={{
        background: "var(--treatment-ground)",
        border: "1px solid var(--treatment-hairline)",
      }}
    >
      {/* Letterbox placeholder — the slot a photograph will occupy. Today
          it's a solid-ground band so the reader can see the treatment
          colour before clicking in. No lorem, no gradient, no decoration;
          the ground IS the signal. */}
      <OverviewCardHero card={card} />

      <div
        className="p-6 md:p-8"
        style={{ background: "var(--color-iron)", color: "var(--color-type-iron)" }}
      >
        <p
          className="mb-2 font-mono text-[10px] font-semibold uppercase tracking-[0.18em]"
          style={{ color: "rgba(245,245,245,0.55)", fontVariationSettings: '"wght" 600' }}
        >
          {card.number} · Treatment
        </p>
        <h2
          className="font-display"
          style={{
            fontVariationSettings: '"opsz" 72, "SOFT" 20',
            fontWeight: 400,
            fontSize: "28px",
            lineHeight: 1.1,
            letterSpacing: "-0.02em",
            margin: "0 0 4px",
            color: "var(--color-type-iron)",
          }}
        >
          {card.title}
        </h2>
        <p
          className="font-display italic"
          style={{
            fontVariationSettings: '"opsz" 72, "SOFT" 30',
            fontWeight: 400,
            fontSize: "18px",
            lineHeight: 1.2,
            letterSpacing: "-0.01em",
            margin: "0 0 14px",
            color: "rgba(245,245,245,0.72)",
          }}
        >
          {card.subtitle}
        </p>
        <p
          style={{
            fontFamily: "'Geist', sans-serif",
            fontSize: "14px",
            lineHeight: 1.55,
            margin: 0,
            color: "rgba(245,245,245,0.72)",
          }}
        >
          {card.description}
        </p>
      </div>
    </Link>
  );
}

// OverviewCardHero — the ~200px letterbox slot at the top of each treatment
// card. Rendered in the card's own data-treatment scope so it paints its
// own ground, and carries a treatment-appropriate lockup in the corner so
// the wordmark variant (argent / chip / emboss) reads at a glance without
// the reader having to click in.
function OverviewCardHero({ card }: { card: TreatmentCard }) {
  return (
    <div
      data-treatment={card.treatment}
      className="relative"
      style={{
        aspectRatio: "16 / 7",
        background: "var(--treatment-ground)",
        color: "var(--treatment-ink)",
      }}
    >
      <div className="absolute bottom-4 left-4 md:bottom-6 md:left-6">
        {card.treatment === "newsroom" ? (
          <Lockup size="sm" variant="emboss" wordmarkColor="var(--color-ink)" />
        ) : card.treatment === "letters" ? (
          <Lockup size="sm" variant="chip" wordmarkColor="var(--color-ink)" />
        ) : (
          <Lockup size="sm" variant="argent" wordmarkColor="var(--color-argent)" />
        )}
      </div>
      {/* Treatment-specific glyph at right — a quiet echo of the mark,
          large, low-opacity, rendered in the treatment's wordmark colour.
          Workshop omits this (wings-only chrome rule) and instead shows
          nothing — the empty ground IS the signal. */}
      {card.treatment === "newsroom" && (
        <WingsEmboss
          style={{
            position: "absolute",
            right: "-20px",
            top: "50%",
            transform: "translateY(-50%)",
            width: "45%",
            height: "auto",
            opacity: 0.24,
          }}
        />
      )}
      {card.treatment === "letters" && (
        <WingsChip
          style={{
            position: "absolute",
            right: "24px",
            top: "50%",
            transform: "translateY(-50%)",
            width: "28%",
            height: "auto",
            opacity: 0.32,
          }}
        />
      )}
    </div>
  );
}
