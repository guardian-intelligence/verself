import { createFileRoute } from "@tanstack/react-router";
import { WingsArgent } from "@forge-metal/brand";
import { FilmGrain } from "~/components/film-grain";
import { RevealSpan } from "~/components/reveal-span";
import { landing } from "~/content/landing";

export const Route = createFileRoute("/")({
  component: LandingPage,
  head: () => ({
    meta: [
      { title: "Guardian Intelligence" },
      {
        name: "description",
        content:
          "Guardian Intelligence is an American applied intelligence company. We build the reference architecture for the systems every founder has to build before they can build what matters.",
      },
      {
        property: "og:title",
        content: "Guardian Intelligence — The world needs your business to succeed.",
      },
      {
        property: "og:description",
        content:
          "We build the reference architecture for the systems every founder has to build before they can build what matters.",
      },
      { property: "og:image", content: "/og/home" },
      { property: "og:image:type", content: "image/svg+xml" },
      { property: "og:image:width", content: "1200" },
      { property: "og:image:height", content: "630" },
      { name: "twitter:card", content: "summary_large_image" },
      { name: "twitter:image", content: "/og/home" },
    ],
  }),
});

function LandingPage() {
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
          style={{ color: "rgba(245,245,245,0.55)" }}
        >
          {landing.kicker}
        </p>

        <h1
          className="mt-5 font-display"
          style={{
            fontVariationSettings: '"opsz" 144, "SOFT" 30',
            fontWeight: 400,
            fontSize: "clamp(38px, 6.8vw, 72px)",
            lineHeight: 1.0,
            letterSpacing: "-0.026em",
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
              color: "rgba(245,245,245,0.82)",
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
            fontFamily: "'Fraunces', Georgia, serif",
            fontVariationSettings: '"opsz" 72, "SOFT" 30',
            fontWeight: 400,
            fontStyle: "italic",
            fontSize: "clamp(20px, 2.4vw, 26px)",
            lineHeight: 1.3,
            letterSpacing: "-0.01em",
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
