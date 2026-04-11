import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
import { Callout } from "~/components/callout";
import { ExecutionSubmissionForm } from "~/features/jobs/components";

export const Route = createFileRoute("/_authenticated/jobs/new")({
  component: NewJobPage,
});

function NewJobPage() {
  const navigate = useNavigate();

  return (
    <div className="space-y-6">
      <div className="flex items-center gap-4">
        <Link to="/jobs" className="text-muted-foreground hover:text-foreground text-sm">
          &larr; Back
        </Link>
        <h1 className="text-2xl font-bold">Manual Execution</h1>
      </div>

      <Callout title="Manual execution">
        Direct executions run the submitted command in a fresh VM.
      </Callout>

      <ExecutionSubmissionForm
        onSuccess={(execution) => {
          void navigate({
            to: "/jobs/$jobId",
            params: { jobId: execution.execution_id },
          });
        }}
      />
    </div>
  );
}
