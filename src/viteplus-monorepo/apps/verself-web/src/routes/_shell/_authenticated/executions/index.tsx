import { createFileRoute } from "@tanstack/react-router";
import {
  Page,
  PageDescription,
  PageHeader,
  PageHeaderContent,
  PageSection,
  PageSections,
  PageTitle,
} from "@verself/ui/components/ui/page";
import { Callout } from "~/components/callout";
import { ExecutionListPanel } from "~/features/executions/components";
import { loadExecutionsIndex } from "~/features/executions/queries";

export const Route = createFileRoute("/_shell/_authenticated/executions/")({
  loader: ({ context }) => loadExecutionsIndex(context.queryClient, context.auth),
  component: ExecutionsPage,
});

function ExecutionsPage() {
  const { auth } = Route.useRouteContext();

  return (
    <Page>
      <PageHeader>
        <PageHeaderContent>
          <PageTitle>Executions</PageTitle>
          <PageDescription>
            Execution history from GitHub runners and scheduled canaries.
          </PageDescription>
        </PageHeaderContent>
      </PageHeader>

      <PageSections>
        <PageSection>
          {auth.orgId ? (
            <ExecutionListPanel />
          ) : (
            <Callout tone="destructive" title="Missing organization">
              Your session is missing organization context. Try signing out and back in.
            </Callout>
          )}
        </PageSection>
      </PageSections>
    </Page>
  );
}
