import { createFileRoute, Link } from "@tanstack/react-router";
import { BookOpen, Code2, ScrollText } from "lucide-react";

export const Route = createFileRoute("/")({
  component: LandingPage,
  head: () => ({
    meta: [
      { title: "Forge Metal Platform" },
      {
        name: "description",
        content:
          "Documentation and API reference for the Forge Metal platform: sandbox runtime, identity, billing, and mailbox services.",
      },
    ],
  }),
});

function LandingPage() {
  return (
    <div className="mx-auto w-full max-w-5xl px-4 py-16 md:px-6 md:py-24">
      <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
        Forge Metal Platform
      </p>
      <h1 className="mt-3 text-4xl font-semibold leading-tight tracking-tight md:text-5xl">
        Build on Forge Metal
      </h1>
      <p className="mt-4 max-w-2xl text-base leading-7 text-muted-foreground md:text-lg">
        Documentation, API references, and architecture notes for the Forge Metal platform — sandbox
        runtime, identity, billing, and mailbox services running on a single operator-owned
        bare-metal node.
      </p>

      <div className="mt-10 grid gap-3 md:grid-cols-3">
        <LandingCard
          to="/docs"
          title="Docs"
          description="Architecture, deployment topology, and service-by-service explainers."
          icon={BookOpen}
        />
        <LandingCard
          to="/docs/reference"
          title="API Reference"
          description="The HTTP surface of every Forge Metal service, generated from OpenAPI."
          icon={Code2}
        />
        <LandingCard
          to="/policy"
          title="Policy"
          description="Data retention, account lifecycle, and the commitments Forge Metal makes about what it stores on your behalf."
          icon={ScrollText}
        />
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
      className="group flex flex-col gap-2 rounded-lg border border-border bg-card p-5 transition-colors hover:border-foreground/30"
    >
      <Icon className="size-5 text-muted-foreground transition-colors group-hover:text-foreground" />
      <div className="mt-1 font-medium">{title}</div>
      <p className="text-sm leading-6 text-muted-foreground">{description}</p>
    </Link>
  );
}
