import { createFileRoute, Link } from "@tanstack/react-router";
import { useEffect } from "react";
import type { Treatment } from "@forge-metal/brand";
import { Lockup } from "@forge-metal/brand";
import { emitSpan } from "~/lib/telemetry/browser";
import { AppliedFooter } from "~/features/design/sections/applied-footer";
import { ogMeta } from "~/lib/head";

// /design overview. Three full-width treatment cards — Workshop, Newsroom,
// Letters — stacked so each room's "soul" (ground, mark, accent CTA) reads
// as its own composition rather than a thumbnail in a 3-up grid. The cards
// are linked blocks; the visitor clicks into the treatment from the ground
// they are already looking at. The /design route sits inside the Workshop
// layout (iron chrome); the cards paint their own treatment ground locally
// without flipping the page chrome.

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

type TreatmentRoute = "/design/workshop" | "/design/newsroom" | "/design/letters";

interface TreatmentCard {
  readonly treatment: Treatment;
  readonly to: TreatmentRoute;
  readonly number: string;
  readonly title: string;
  readonly subtitle: string;
  readonly description: string;
  readonly ctaLabel: string;
}

const CARDS: readonly TreatmentCard[] = [
  {
    treatment: "workshop",
    to: "/design/workshop",
    number: "01",
    title: "Workshop",
    subtitle: "Where the work happens.",
    description:
      "Iron ground, Geist throughout, Amber as the sole accent. The productivity chrome — marketing, docs, console, the everyday register. Fraunces is reserved for editorial body copy in the other rooms.",
    ctaLabel: "Enter the Workshop",
  },
  {
    treatment: "newsroom",
    to: "/design/newsroom",
    number: "02",
    title: "Newsroom",
    subtitle: "The broadcast.",
    description:
      "Argent ground, Flare hero bands, Flare CTAs. Modeled as a press-room blog index: the single loud field carries the bulletin; the rest of the page stays crisp so the acid green can speak.",
    ctaLabel: "Enter the Newsroom",
  },
  {
    treatment: "letters",
    to: "/design/letters",
    number: "03",
    title: "Letters",
    subtitle: "The long form.",
    description:
      "Paper ground, Fraunces body, Bordeaux as the editorial accent. Where individual voices inside Guardian show their work — an engineer's postmortem, the founder's note, a milestone retold in full.",
    ctaLabel: "Read the Letters",
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
      <header className="mb-12 flex flex-col gap-4">
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
          The mark varies with ground — argent wings on Iron, the dark chip on Paper, the emboss
          medallion on Argent — but the wordmark does not. Guardian sets in tracked uppercase Geist
          on every ground, because the sign over the door is the name of the house, not the voice of
          the room. Every other decision — palette, body type, accent, section name — belongs to the
          treatment.
        </p>
      </header>

      <div className="flex flex-col gap-6">
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
      onClick={() =>
        emitSpan("design.overview.card_click", {
          treatment: card.treatment,
          destination: card.to,
        })
      }
      className="group block overflow-hidden rounded-2xl"
      style={{
        background: "var(--treatment-ground)",
        color: "var(--treatment-ink)",
        border: "1px solid var(--treatment-hairline)",
      }}
    >
      <div
        className="relative grid gap-8 px-6 py-10 md:grid-cols-[minmax(0,1fr)_auto] md:items-end md:px-12 md:py-14"
        style={{ minHeight: "clamp(260px, 36vw, 360px)" }}
      >
        <div className="flex flex-col gap-5">
          <div style={{ opacity: 0.92 }}>
            <TreatmentMark treatment={card.treatment} />
          </div>
          <p
            className="font-mono text-[10px] font-semibold uppercase tracking-[0.18em]"
            style={{
              color: "var(--treatment-muted-meta)",
              fontVariationSettings: '"wght" 600',
              margin: 0,
            }}
          >
            {card.number} · Treatment
          </p>
          <h2
            style={{
              fontFamily: "'Fraunces', Georgia, serif",
              fontVariationSettings: '"opsz" 144, "SOFT" 30',
              fontWeight: 400,
              fontSize: "clamp(36px, 5.2vw, 60px)",
              lineHeight: 1.02,
              letterSpacing: "-0.025em",
              margin: 0,
              color: "var(--treatment-ink)",
            }}
          >
            {card.title}
          </h2>
          <p
            style={{
              fontFamily: "'Geist', sans-serif",
              fontWeight: 500,
              fontSize: "clamp(15px, 1.6vw, 17px)",
              lineHeight: 1.45,
              letterSpacing: "-0.005em",
              margin: 0,
              color: "var(--treatment-muted-strong)",
            }}
          >
            {card.subtitle}
          </p>
          <p
            className="max-w-[56ch]"
            style={{
              fontFamily: "'Geist', sans-serif",
              fontSize: "15px",
              lineHeight: 1.55,
              margin: 0,
              color: "var(--treatment-muted)",
            }}
          >
            {card.description}
          </p>
        </div>

        <div className="flex items-end justify-start md:justify-end">
          <TreatmentCTA treatment={card.treatment} label={card.ctaLabel} />
        </div>
      </div>
    </Link>
  );
}

// TreatmentMark — the Lockup as it ships in each room's masthead. Workshop
// renders bare (GUARDIAN, no suffix, because Workshop is the house root);
// Newsroom and Letters render with their section suffix so the /design cards
// preview the exact GUARDIAN · LETTERS / GUARDIAN · NEWSROOM lockups the
// visitor will see when they step into the room.
function TreatmentMark({ treatment }: { treatment: Treatment }) {
  if (treatment === "workshop") {
    return <Lockup size="sm" variant="argent" wordmarkColor="var(--color-argent)" />;
  }
  if (treatment === "newsroom") {
    return (
      <Lockup size="sm" variant="emboss" wordmarkColor="var(--color-ink)" section="Newsroom" />
    );
  }
  return <Lockup size="sm" variant="chip" wordmarkColor="var(--color-ink)" section="Letters" />;
}

// TreatmentCTA — accent button per room. Workshop keeps its Amber primary
// (the one place Amber ships as a CTA). Newsroom and Letters both paint Ink
// (black) with Flare/Paper text respectively — Bordeaux is reserved for
// Letters' editorial ornaments (pull-quotes, drop-caps, rules), never for
// calls to action. Border radius matches shadcn's `rounded-md` token so the
// buttons read as boxy controls, not marketing pills.
//
// The CTA is a visual affordance; the whole card is a <Link>, so it renders
// as <span> not <button> to avoid a nested interactive element.
function TreatmentCTA({ treatment, label }: { treatment: Treatment; label: string }) {
  const style =
    treatment === "workshop"
      ? { bg: "var(--color-amber)", fg: "var(--color-ink)" }
      : treatment === "newsroom"
        ? { bg: "var(--color-flare)", fg: "var(--color-ink)" }
        : { bg: "var(--color-ink)", fg: "var(--color-paper)" };
  return (
    <span
      className="inline-flex items-center gap-2 rounded-md transition-transform group-hover:translate-x-0.5"
      style={{
        fontFamily: "'Geist', sans-serif",
        fontWeight: 500,
        fontSize: "14px",
        padding: "10px 18px",
        background: style.bg,
        color: style.fg,
        border: "none",
        whiteSpace: "nowrap",
      }}
    >
      {label}
      <span aria-hidden="true">→</span>
    </span>
  );
}
