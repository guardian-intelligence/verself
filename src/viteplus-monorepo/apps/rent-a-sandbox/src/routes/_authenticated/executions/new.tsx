import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
import { Callout } from "~/components/callout";
import { ExecutionSubmissionForm } from "~/features/executions/components";

export const Route = createFileRoute("/_authenticated/executions/new")({
  component: NewExecutionPage,
});

function NewExecutionPage() {
  const navigate = useNavigate();

  return (
    <div className="space-y-6">
      <div className="flex items-center gap-4">
        <Link
          to="/executions"
          className="font-mono text-xs uppercase tracking-wider text-muted-foreground hover:text-foreground"
        >
          &larr; Back
        </Link>
        <h1 className="text-2xl font-semibold">New execution</h1>
      </div>

      <Callout title="Manual execution">
        Direct executions run the submitted command in a fresh VM.
      </Callout>

      <ExecutionSubmissionForm
        onSuccess={(execution) => {
          void navigate({
            to: "/executions/$executionId",
            params: { executionId: execution.execution_id },
          });
        }}
      />
    </div>
  );
}
