import { createFileRoute } from "@tanstack/react-router";
import { RepoConsole, RepoConsoleSkeleton } from "~/features/console/repo-console-card";
import { loadConsole } from "~/features/console/runs";

// The console is the only signed-in surface. Everything operators used to do
// through dashboard routes now lives in the CLI/SDK/HTTP API; this page is an
// ambient view of connected repos and their recent CI.
export const Route = createFileRoute("/_shell/_authenticated/$orgSlug/")({
  loader: ({ context }) => loadConsole(context.queryClient, context.auth),
  pendingComponent: RepoConsoleSkeleton,
  component: RepoConsole,
});
