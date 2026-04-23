import { createFileRoute } from "@tanstack/react-router";
import { WingsArgent } from "@forge-metal/brand";
import { FilmGrain } from "~/components/film-grain";
import { NewsroomStrip } from "~/components/newsroom-strip";
import { RevealSpan } from "~/components/reveal-span";
import { landing } from "~/content/landing";
import { ogMeta } from "~/lib/head";

export const Route = createFileRoute("/_workshop/")({
  component: LandingPage,
  head: () => ({
    meta: ogMeta({
      slug: "home",
      title: "Guardian — The world needs your business to succeed.",
      description:
        "Guardian is an American applied intelligence firm. We build the reference architecture for the systems every founder has to build before they can build what matters.",
    }),
  }),
});

function LandingPage() {
  return (
    <>
      <LandingHero />
      <NewsroomStrip />
    </>
  );
}

function LandingHero() {
  return (
    <div className="relative mx-auto w-full max-w-5xl px-4 py-16 md:px-6 md:py-24">
      {/* FilmGrain wraps the container in a warm vintage overlay blended with
          the Iron ground beneath. The hero wings + text sit above it because
          <FilmGrain> is position:absolute and does not capture pointer events.
          Intensity 0.22 is pitched quiet enough that text remains legible at
          every ground density. */}
      <FilmGrain intensity={0.22} />

      <RevealSpan spanName="company.landing.hero_view" attrs={{ "hero.variant": "iron" }}>
        {/* Argent wings at hero scale, on the fold. Honors /design §09 Iron
            spec. */}
        <div style={{ marginBottom: "40px", position: "relative" }}>
          <WingsArgent
            style={{ width: "clamp(96px, 14vw, 160px)", height: "auto", display: "block" }}
          />
        </div>

        <p
          className="font-mono text-[11px] font-medium uppercase tracking-[0.16em]"
          style={{ color: "var(--treatment-muted-faint)" }}
        >
          {landing.kicker}
        </p>

        <h1
          className="mt-5"
          style={{
            // Workshop voice is Geist-only — Fraunces is reserved for Letters.
            // The landing sits under Workshop chrome so the hero sets in Geist
            // at display scale; letterspacing tightens to -0.028em to hold the
            // display-type rhythm without the optical-size axis Fraunces would
            // otherwise provide.
            fontFamily: "'Geist', ui-sans-serif, system-ui, sans-serif",
            fontWeight: 500,
            fontSize: "clamp(38px, 6.8vw, 72px)",
            lineHeight: 1.02,
            letterSpacing: "-0.028em",
            color: "var(--color-type-iron)",
            maxWidth: "22ch",
            margin: 0,
          }}
        >
          {landing.hero}
        </h1>
      </RevealSpan>

      <div className="mt-12 flex flex-col gap-5" style={{ maxWidth: "62ch" }}>
        {landing.mission.map((paragraph, idx) => (
          <RevealSpan
            key={idx}
            as="p"
            spanName="company.landing.section_view"
            attrs={{ "section.id": `mission-${idx}`, "section.index": String(idx) }}
            style={{
              fontFamily: "'Geist', sans-serif",
              fontWeight: 400,
              fontSize: "clamp(16px, 1.7vw, 18px)",
              lineHeight: 1.55,
              color: "var(--treatment-muted-strong)",
              margin: 0,
            }}
          >
            {paragraph}
          </RevealSpan>
        ))}

        <RevealSpan
          as="p"
          spanName="company.landing.section_view"
          attrs={{ "section.id": "closer", "section.index": String(landing.mission.length) }}
          style={{
            // Workshop declines Fraunces — the closer still gets to sit at
            // larger-than-body scale to signal it's the landing's closing beat,
            // but it sets in Geist at semibold italic rather than Fraunces.
            fontFamily: "'Geist', ui-sans-serif, system-ui, sans-serif",
            fontWeight: 500,
            fontStyle: "italic",
            fontSize: "clamp(20px, 2.4vw, 26px)",
            lineHeight: 1.3,
            letterSpacing: "-0.012em",
            color: "var(--color-type-iron)",
            maxWidth: "34ch",
            margin: "4px 0 0",
          }}
        >
          {landing.closer}
        </RevealSpan>
      </div>
    </div>
  );
}
