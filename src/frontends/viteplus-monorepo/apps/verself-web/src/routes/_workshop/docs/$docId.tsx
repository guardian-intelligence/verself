import { createFileRoute, Link, notFound } from "@tanstack/react-router";
import { ArrowRight } from "lucide-react";

import { DOCS_NAV } from "~/lib/docs-nav";

export const Route = createFileRoute("/_workshop/docs/$docId")({
  component: ComingSoonDocsPage,
  head: ({ params }) => {
    const entry = findDocsEntry(params.docId);
    return {
      meta: [
        {
          title: entry ? `${entry.label} — Verself Platform` : "Docs — Verself Platform",
        },
      ],
    };
  },
});

function ComingSoonDocsPage() {
  const { docId } = Route.useParams();
  const entry = findDocsEntry(docId);

  if (!entry) {
    throw notFound();
  }

  return (
    <article className="flex flex-col gap-8">
      <header className="flex flex-col gap-4 border-b border-border pb-8">
        <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
          Coming soon
        </p>
        <div className="flex flex-col gap-2">
          <h1 className="text-3xl font-semibold tracking-tight md:text-4xl">{entry.label}</h1>
          {entry.description && (
            <p className="max-w-2xl text-sm leading-6 text-muted-foreground md:text-base md:leading-7">
              {entry.description}
            </p>
          )}
        </div>
      </header>

      <section className="flex max-w-2xl flex-col gap-4 text-sm leading-6 text-muted-foreground md:text-base md:leading-7">
        <p>
          This page is reserved in the public docs navigation while the implementation and examples
          are cut over to the Verself API surface.
        </p>
        <Link
          to="/docs/reference"
          className="inline-flex w-fit items-center gap-2 rounded-md border border-border px-3 py-2 text-sm font-medium text-[var(--treatment-ink)] transition-colors hover:bg-accent/60 hover:text-foreground"
        >
          Open API Reference
          <ArrowRight className="size-4" />
        </Link>
      </section>
    </article>
  );
}

function findDocsEntry(docId: string) {
  return DOCS_NAV.find((entry) => entry.to === `/docs/${docId}` && entry.status === "coming-soon");
}
