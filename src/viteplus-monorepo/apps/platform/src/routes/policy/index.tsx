import { createFileRoute, Link } from "@tanstack/react-router";
import { ArrowRight } from "lucide-react";

export const Route = createFileRoute("/policy/")({
  component: PolicyOverview,
  head: () => ({
    meta: [
      { title: "Policy — Forge Metal Platform" },
      {
        name: "description",
        content: "Customer-facing policy documents for the Forge Metal platform.",
      },
    ],
  }),
});

function PolicyOverview() {
  return (
    <div className="flex flex-col gap-10">
      <header className="flex flex-col gap-2">
        <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
          Platform Policy
        </p>
        <h1 className="text-3xl font-semibold tracking-tight md:text-4xl">Policy</h1>
        <p className="max-w-2xl text-sm leading-6 text-muted-foreground md:text-base md:leading-7">
          Forge Metal's commitments to customers about how we handle your data, your account, and
          the services we run on your behalf. Each document below applies to every organization on
          this deployment, including our own.
        </p>
      </header>

      <ul className="flex flex-col gap-2">
        <PolicyCard
          to="/policy/data-retention"
          title="Data retention"
          description="What we keep, how long we keep it, and how it can be exported or deleted across the account lifecycle."
          effective="Effective April 17, 2026"
        />
      </ul>
    </div>
  );
}

function PolicyCard({
  to,
  title,
  description,
  effective,
}: {
  to: string;
  title: string;
  description: string;
  effective: string;
}) {
  return (
    <li>
      <Link
        to={to}
        className="group flex items-start gap-4 rounded-lg border border-border bg-card p-5 transition-colors hover:border-foreground/30"
      >
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
            <span className="font-medium">{title}</span>
            <span className="text-xs text-muted-foreground">{effective}</span>
          </div>
          <p className="mt-1 text-sm leading-6 text-muted-foreground">{description}</p>
        </div>
        <ArrowRight className="mt-0.5 size-4 shrink-0 text-muted-foreground transition-colors group-hover:text-foreground" />
      </Link>
    </li>
  );
}
