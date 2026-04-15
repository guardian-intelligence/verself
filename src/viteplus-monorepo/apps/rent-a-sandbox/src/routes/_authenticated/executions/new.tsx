import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
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
import { ExecutionSubmissionForm } from "~/features/executions/components";

export const Route = createFileRoute("/_authenticated/executions/new")({
  component: NewExecutionPage,
});

function NewExecutionPage() {
  const navigate = useNavigate();

  return (
    <Page variant="narrow">
      <PageHeader>
        <PageHeaderContent>
          <PageEyebrow>
            <Link to="/executions" className="hover:text-foreground">
              ← Executions
            </Link>
          </PageEyebrow>
          <PageTitle>New execution</PageTitle>
          <PageDescription>
            Direct executions run the submitted command in a fresh VM.
          </PageDescription>
        </PageHeaderContent>
      </PageHeader>

      <PageSections>
        <PageSection>
          <ExecutionSubmissionForm
            onSuccess={(execution) => {
              void navigate({
                to: "/executions/$executionId",
                params: { executionId: execution.execution_id },
              });
            }}
          />
        </PageSection>
      </PageSections>
    </Page>
  );
}
