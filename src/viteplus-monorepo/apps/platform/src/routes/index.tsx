import { createFileRoute, Link } from "@tanstack/react-router";
import { BookOpen, Code2, ScrollText } from "lucide-react";

export const Route = createFileRoute("/")({
  component: LandingPage,
  head: () => ({
    meta: [
      { title: "Guardian Intelligence" },
      {
        name: "description",
        content:
          "The world needs your business to succeed, and we're here to help. Guardian Intelligence is an American applied intelligence company.",
      },
      {
        property: "og:title",
        content: "Guardian Intelligence — The world needs your business to succeed.",
      },
      {
        property: "og:description",
        content:
          "We build the reference architecture for the systems every founder has to build before they can build what matters — so one founder with Claude Code can run a billion-dollar company.",
      },
    ],
  }),
});

function LandingPage() {
  return (
    <div className="mx-auto w-full max-w-5xl px-4 py-16 md:px-6 md:py-24">
      <p
        className="font-mono text-[11px] font-medium uppercase tracking-[0.16em]"
        style={{ color: "rgba(245,245,245,0.55)" }}
      >
        Guardian Intelligence · An American applied intelligence company · Seattle, Washington
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
        The world needs your business to succeed, and we&apos;re here to help.
      </h1>

      {/* Mission block: three paragraphs of cash-out below the one-sentence
          hero. Voice: first-person plural, present tense, grandfather-calm.
          The closer returns to Fraunces italic so the paragraph that opens the
          page in serif ends the section in the same register. */}
      <div className="mt-12 flex flex-col gap-5" style={{ maxWidth: "62ch" }}>
        <p
          style={{
            fontFamily: "'Geist', sans-serif",
            fontWeight: 400,
            fontSize: "clamp(16px, 1.7vw, 18px)",
            lineHeight: 1.55,
            color: "rgba(245,245,245,0.82)",
            margin: 0,
          }}
        >
          Every founder spends the first year on the same dozen systems — identity, billing,
          analytics, email, infrastructure, security, the thousand edges where a real company
          touches the real world. None of it is what you started the company to build. We build the
          reference architecture for all of it — open-source, documented, and clean enough that one
          founder with <b>Claude Code</b> can run a billion-dollar company.
        </p>
        <p
          style={{
            fontFamily: "'Geist', sans-serif",
            fontWeight: 400,
            fontSize: "clamp(16px, 1.7vw, 18px)",
            lineHeight: 1.55,
            color: "rgba(245,245,245,0.82)",
            margin: 0,
          }}
        >
          Value created per capita is the ultimate metric. Value is more than a transaction. It is a
          painting. A novel. An API in front of a physical service. A quiet service that sends a
          calendar invite to the neighborhood when the dog park is going to be 72 and sunny with 80%
          confidence. Humanity&apos;s golden age is the one where every person gets to contribute
          unprecedented value to the world, and software and AI finally supply the leverage to make
          that possible for everyone.
        </p>
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
          If you want to do something good for the world, we want to make it easy.
        </p>
      </div>

      {/* Developer entry points — the practical corner of an otherwise
          brand-forward landing page. Until guardianintelligence.org exists
          separately, platform.* plays both roles; when it splits, these move
          to platform-only and the mission block stays at the root. */}
      <div className="mt-16">
        <p
          className="font-mono text-[11px] font-medium uppercase tracking-[0.16em]"
          style={{ color: "rgba(245,245,245,0.4)", marginBottom: "16px" }}
        >
          For developers
        </p>
        <div className="grid gap-3 md:grid-cols-3">
          <LandingCard
            to="/docs"
            title="Docs"
            description="Architecture, deployment topology, and service-by-service explainers."
            icon={BookOpen}
          />
          <LandingCard
            to="/docs/reference"
            title="API Reference"
            description="The HTTP surface of every Guardian service, generated from OpenAPI."
            icon={Code2}
          />
          <LandingCard
            to="/policy"
            title="Policy"
            description="Data retention, account lifecycle, and the commitments Guardian makes about what it stores on your behalf."
            icon={ScrollText}
          />
        </div>
      </div>
    </div>
  );
}

function LandingCard({
  to,
  title,
  description,
  icon: Icon,
}: {
  to: string;
  title: string;
  description: string;
  icon: typeof BookOpen;
}) {
  return (
    <Link
      to={to}
      className="group flex flex-col gap-2 rounded-lg p-5 transition-colors"
      style={{
        border: "1px solid rgba(245,245,245,0.12)",
        background: "rgba(245,245,245,0.02)",
        color: "var(--color-type-iron)",
      }}
    >
      <Icon className="size-5" style={{ color: "rgba(245,245,245,0.55)" }} />
      <div className="mt-1 font-medium">{title}</div>
      <p className="text-sm leading-6" style={{ color: "rgba(245,245,245,0.6)" }}>
        {description}
      </p>
    </Link>
  );
}
