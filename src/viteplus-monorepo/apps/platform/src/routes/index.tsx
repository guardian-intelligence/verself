import { createFileRoute, Link } from "@tanstack/react-router";
import { BookOpen, Code2, ScrollText } from "lucide-react";

export const Route = createFileRoute("/")({
  component: LandingPage,
  head: () => ({
    meta: [
      { title: "Verself" },
      {
        name: "description",
        content:
          "Self-hosted platform infrastructure: console, docs, API reference, and policy for Verself.",
      },
      {
        property: "og:title",
        content: "Verself — self-hosted platform infrastructure.",
      },
      {
        property: "og:description",
        content:
          "The self-hosted Vercel joke taken far enough to have Firecracker, billing, identity, source hosting, docs, and policy.",
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
        Verself · self-hosted platform infrastructure · by Guardian Intelligence
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
        Self-hosted Vercel, for people who read their own systemd logs.
      </h1>

      {/* Product summary block. Guardian's company narrative lives on
          guardianintelligence.org; this page explains the product surface. */}
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
          Verself is the platform surface Guardian runs for itself: identity, billing, source
          hosting, CI-style runners, VM workloads, secrets, audit trails, and the console that keeps
          the whole contraption visible.
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
          It is not trying to be mysterious. It is Go services, TanStack apps, PostgreSQL,
          ClickHouse, Firecracker, Caddy, SPIFFE, and Ansible, wired together so the boring platform
          work is inspectable instead of outsourced to a dashboard nobody owns.
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
          The docs, API reference, and customer policies live here. The authenticated console lives
          at console.verself.sh.
        </p>
      </div>

      {/* Developer entry points — the practical corner of an otherwise
          brand-forward product landing page. Company narrative lives on
          guardianintelligence.org; this apex keeps product docs and policy. */}
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
            description="The HTTP surface of every Verself service, generated from OpenAPI."
            icon={Code2}
          />
          <LandingCard
            to="/policy"
            title="Policy"
            description="Data retention, account lifecycle, and the commitments Verself makes about what it stores on your behalf."
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
