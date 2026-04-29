import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/_workshop/docs/")({
  component: DocsOverview,
  head: () => ({
    meta: [{ title: "Docs — Verself Platform" }],
  }),
});

function DocsOverview() {
  return (
    <div className="flex flex-col gap-6">
      <header className="flex flex-col gap-2">
        <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
          Platform Docs
        </p>
        <h1 className="text-3xl font-semibold tracking-tight md:text-4xl">Verself Platform</h1>
        <p className="max-w-2xl text-sm leading-6 text-muted-foreground md:text-base md:leading-7">
          Architecture notes, deployment topology, and API references for every Verself service. The
          docs subtree is currently a scaffold — content will land alongside each service's OpenAPI
          surface.
        </p>
      </header>
    </div>
  );
}
