import { createFileRoute, Link } from "@tanstack/react-router";
import { Button } from "@forge-metal/ui/components/ui/button";
import {
  Page,
  PageActions,
  PageDescription,
  PageHeader,
  PageHeaderContent,
  PageSection,
  PageSections,
  PageTitle,
} from "@forge-metal/ui/components/ui/page";
import { ExecutionSchedulesPanel } from "~/features/schedules/components";
import { loadExecutionSchedules } from "~/features/schedules/queries";

export const Route = createFileRoute("/_shell/_authenticated/schedules/")({
  loader: ({ context }) => loadExecutionSchedules(context.queryClient, context.auth),
  component: SchedulesPage,
});

function SchedulesPage() {
  return (
    <Page>
      <PageHeader>
        <PageHeaderContent>
          <PageTitle>Schedules</PageTitle>
          <PageDescription>Recurring VM canaries backed by Temporal schedules.</PageDescription>
        </PageHeaderContent>
        <PageActions>
          <Button render={<Link to="/schedules/new" />}>New schedule</Button>
        </PageActions>
      </PageHeader>
      <PageSections>
        <PageSection>
          <ExecutionSchedulesPanel />
        </PageSection>
      </PageSections>
    </Page>
  );
}
