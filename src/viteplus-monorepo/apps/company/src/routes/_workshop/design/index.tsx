import { createFileRoute, Link } from "@tanstack/react-router";
import { useEffect } from "react";
import type { Treatment } from "@forge-metal/brand";
import { Lockup, WingsChip, WingsEmboss } from "@forge-metal/brand";
import { emitSpan } from "~/lib/telemetry/browser";
import { AppliedFooter } from "~/features/design/sections/applied-footer";
import { ogMeta } from "~/lib/head";

// /design overview. Three treatment cards — Workshop, Newsroom, Letters —
// plus the Applied cross-treatment rules at the foot. Each card renders a
// short letterbox in the treatment's own ground so the visitor sees the
// room's palette before clicking in; the chip/emboss/argent lockup in the
// corner previews the wordmark variant. The page itself is rendered inside
// the Workshop layout (iron chrome) — this is a Workshop view of the three
// rooms, not a chrome flip.

export const Route = createFileRoute("/_workshop/design/")({
  component: DesignOverview,
  head: () => ({
    meta: ogMeta({
      slug: "design",
      title: "Guardian — Brand system",
      description:
        "The Guardian brand system: three treatments — Workshop, Newsroom, Letters — each a room with its own palette, type, and mark usage.",
    }),
    links: [{ rel: "canonical", href: "/design" }],
  }),
});

interface TreatmentCard {
  readonly treatment: Treatment;
  readonly to: "/design/workshop" | "/design/newsroom" | "/design/letters";
  readonly number: string;
  readonly title: string;
  readonly subtitle: string;
  readonly description: string;
}

const CARDS: readonly TreatmentCard[] = [
  {
    treatment: "workshop",
    to: "/design/workshop",
    number: "01",
    title: "Workshop",
    subtitle: "Where the work happens.",
    description:
      "Iron ground, Geist throughout, Amber as the sole accent. The productivity chrome — marketing, docs, console, the everyday register. Fraunces is absent here.",
  },
  {
    treatment: "newsroom",
    to: "/design/newsroom",
    number: "02",
    title: "Newsroom",
    subtitle: "The broadcast.",
    description:
      "Flare ground, wings inside a circular ink emboss, Fraunces at display weight. Guardian when it needs to be seen in someone else's feed — OG cards, share previews, conference backdrops.",
  },
  {
    treatment: "letters",
    to: "/design/letters",
    number: "03",
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
    <div className="mx-auto w-full max-w-7xl px-4 py-14 md:px-6 md:py-20">
      <header className="mb-14 flex flex-col gap-4">
        <p
          className="font-mono text-[11px] font-semibold uppercase tracking-[0.16em]"
          style={{ color: "var(--treatment-muted)", fontVariationSettings: '"wght" 600' }}
        >
          Guardian · Brand system
        </p>
        <h1
          style={{
            fontFamily: "'Geist', sans-serif",
            fontWeight: 500,
            fontSize: "clamp(32px, 4.4vw, 48px)",
            lineHeight: 1.05,
            letterSpacing: "-0.022em",
            margin: 0,
            maxWidth: "22ch",
          }}
        >
          Three rooms, one house.
        </h1>
        <p
          className="max-w-3xl"
          style={{
            color: "var(--treatment-muted-strong)",
            fontFamily: "'Geist', sans-serif",
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

      <div className="grid gap-6 md:grid-cols-3">
        {CARDS.map((card) => (
          <OverviewCard key={card.treatment} card={card} />
        ))}
      </div>

      <div className="mt-20">
        <AppliedFooter />
      </div>
    </div>
  );
}

function OverviewCard({ card }: { card: TreatmentCard }) {
  return (
    <Link
      to={card.to}
      data-treatment={card.treatment}
      className="group block overflow-hidden rounded-xl"
      style={{
        background: "var(--treatment-ground)",
        border: "1px solid var(--treatment-hairline)",
      }}
    >
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
          style={{
            fontFamily: "'Geist', sans-serif",
            fontWeight: 600,
            fontSize: "24px",
            lineHeight: 1.1,
            letterSpacing: "-0.018em",
            margin: "0 0 4px",
            color: "var(--color-type-iron)",
          }}
        >
          {card.title}
        </h2>
        <p
          style={{
            fontFamily: "'Geist', sans-serif",
            fontSize: "14px",
            lineHeight: 1.45,
            letterSpacing: "-0.005em",
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
