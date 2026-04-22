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

export const Route = createFileRoute("/_shell/_authenticated/schedules/new")({
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
            Temporal triggers a recurring direct execution on the interval you configure here.
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
