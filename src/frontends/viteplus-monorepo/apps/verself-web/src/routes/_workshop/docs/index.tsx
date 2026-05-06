import { createFileRoute, Link } from "@tanstack/react-router";
import { ArrowRight } from "lucide-react";
import { type ReactNode } from "react";

import { DOCS_NAV } from "~/lib/docs-nav";

export const Route = createFileRoute("/_workshop/docs/")({
  component: DocsOverview,
  head: () => ({
    meta: [{ title: "Docs — Verself Platform" }],
  }),
});

function DocsOverview() {
  const available = DOCS_NAV.filter((entry) => entry.status !== "coming-soon");
  const comingSoon = DOCS_NAV.filter((entry) => entry.status === "coming-soon");

  return (
    <article className="flex flex-col gap-10">
      <header className="flex flex-col gap-4 border-b border-border pb-8">
        <div className="flex flex-col gap-2">
          <h1 className="text-3xl font-semibold tracking-tight md:text-4xl">Verself Platform</h1>
          <p className="max-w-2xl text-sm leading-6 text-muted-foreground md:text-base md:leading-7">
            Public documentation for sandbox compute, clone operation, service APIs, and the
            security model behind customer-rented Firecracker workloads.
          </p>
        </div>
      </header>

      <DocsSection title="Available">
        {available.map((entry) => (
          <DocsCard key={entry.id} entry={entry} />
        ))}
      </DocsSection>

      <DocsSection title="Coming soon">
        {comingSoon.map((entry) => (
          <DocsCard key={entry.id} entry={entry} />
        ))}
      </DocsSection>
    </article>
  );
}

function DocsSection({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="flex flex-col gap-3">
      <h2 className="text-lg font-semibold tracking-tight">{title}</h2>
      <div className="grid gap-3 md:grid-cols-2">{children}</div>
    </section>
  );
}

function DocsCard({ entry }: { entry: (typeof DOCS_NAV)[number] }) {
  return (
    <Link
      to={entry.to}
      className="group flex min-h-32 flex-col justify-between gap-4 rounded-md border border-border p-4 transition-colors hover:bg-accent/60"
    >
      <span className="flex items-start justify-between gap-3">
        <span className="flex flex-col gap-1">
          <span className="font-medium text-[var(--treatment-ink)] group-hover:text-foreground">
            {entry.label}
          </span>
          {entry.description && (
            <span className="text-sm leading-6 text-muted-foreground">{entry.description}</span>
          )}
        </span>
        {entry.status === "coming-soon" && (
          <span className="shrink-0 rounded-sm border border-border px-1.5 py-0.5 font-mono text-[10px] font-medium uppercase tracking-normal text-muted-foreground">
            Soon
          </span>
        )}
      </span>
      <span className="inline-flex items-center gap-1 text-sm font-medium text-[var(--treatment-ink)] group-hover:text-foreground">
        Open
        <ArrowRight className="size-4 transition-transform group-hover:translate-x-0.5" />
      </span>
    </Link>
  );
}
