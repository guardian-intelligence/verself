import { createFileRoute, Link } from "@tanstack/react-router";

// Stub destination for the flight commit pill. Live run logs are the next
// slice; this keeps the affordance honest without dead-ending.
export const Route = createFileRoute("/_shell/_authenticated/$orgSlug/runs/$providerRunId")({
  component: RunLogsStub,
});

function RunLogsStub() {
  const { orgSlug, providerRunId } = Route.useParams();
  return (
    <div className="mx-auto w-full max-w-3xl px-4 py-16">
      <p className="font-mono text-xs tracking-tight text-muted-foreground">run {providerRunId}</p>
      <h1 className="mt-3 text-lg font-medium text-foreground">CI run logs</h1>
      <p className="mt-2 max-w-md text-sm text-muted-foreground">
        Live logs for this run are coming soon.
      </p>
      <Link
        to="/$orgSlug"
        params={{ orgSlug }}
        search={{
          flight: undefined,
          actor: undefined,
          src: undefined,
          dst: undefined,
          status: undefined,
          state: undefined,
          remaining: undefined,
          commits: undefined,
          frame: undefined,
        }}
        className="mt-6 inline-block text-sm text-foreground underline underline-offset-4"
      >
        Back to flights
      </Link>
    </div>
  );
}
