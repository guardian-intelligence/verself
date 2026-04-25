import { createFileRoute, Link } from "@tanstack/react-router";
import {
  Page,
  PageDescription,
  PageEyebrow,
  PageHeader,
  PageHeaderContent,
  PageSection,
  PageSections,
  PageTitle,
} from "@forge-metal/ui/components/ui/page";
import { ExecutionScheduleForm } from "~/features/schedules/components";
import { loadSourceRepositories } from "~/features/source/queries";

export const Route = createFileRoute("/_shell/_authenticated/schedules/new")({
  loader: ({ context }) => loadSourceRepositories(context.queryClient, context.auth),
  component: NewSchedulePage,
});

function NewSchedulePage() {
  return (
    <Page variant="narrow">
      <PageHeader>
        <PageHeaderContent>
          <PageEyebrow>
            <Link to="/schedules" className="hover:text-foreground">
              ← Schedules
            </Link>
          </PageEyebrow>
          <PageTitle>New schedule</PageTitle>
          <PageDescription>
            Temporal triggers recurring source workflow dispatches on this interval.
          </PageDescription>
        </PageHeaderContent>
      </PageHeader>
      <PageSections>
        <PageSection>
          <ExecutionScheduleForm />
        </PageSection>
      </PageSections>
    </Page>
  );
}
