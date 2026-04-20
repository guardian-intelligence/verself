import { createFileRoute } from "@tanstack/react-router";
import { WingsArgent } from "@forge-metal/brand";
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
    ],
  }),
});

function LandingPage() {
  return (
    <div className="mx-auto w-full max-w-5xl px-4 py-16 md:px-6 md:py-24">
      {/* Argent wings at hero scale, on the fold. Honors /design §09 Iron spec.
          Phase 3 will replace the inline SVG wrapping with a dedicated hero
          component once the photography treatment lands. */}
      <div style={{ marginBottom: "40px" }}>
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

      <div className="mt-12 flex flex-col gap-5" style={{ maxWidth: "62ch" }}>
        {landing.mission.map((paragraph, idx) => (
          <p
            key={idx}
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
          </p>
        ))}

        <p
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
        </p>
      </div>
    </div>
  );
}
