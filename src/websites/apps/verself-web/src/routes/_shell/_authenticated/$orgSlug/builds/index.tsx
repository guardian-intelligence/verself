import { createFileRoute } from "@tanstack/react-router";
import { Page } from "@verself/ui/components/ui/page";
import { BuildsDashboard, BuildsDashboardSkeleton } from "~/features/builds/components";
import { loadBuildsDashboard } from "~/features/builds/queries";
import { parseBuildsSearch } from "~/features/builds/search";

export const Route = createFileRoute("/_shell/_authenticated/$orgSlug/builds/")({
  validateSearch: parseBuildsSearch,
  loader: ({ context }) => loadBuildsDashboard(context.queryClient, context.auth),
  pendingComponent: BuildsPagePending,
  component: BuildsPage,
});

function BuildsPage() {
  const search = Route.useSearch();
  return (
    <Page variant="full" className="gap-0">
      <h1 className="sr-only">Deployments</h1>
      <BuildsDashboard search={search} />
    </Page>
  );
}

function BuildsPagePending() {
  return (
    <Page variant="full" className="gap-0">
      <h1 className="sr-only">Deployments</h1>
      <BuildsDashboardSkeleton />
    </Page>
  );
}
