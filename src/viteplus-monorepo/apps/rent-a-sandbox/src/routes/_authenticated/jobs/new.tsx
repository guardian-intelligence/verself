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
        Imported repos should normally be prepared and run from the{" "}
        <Link to="/repos" className="text-primary hover:underline">
          Repos
        </Link>{" "}
        flow so they execute against an active golden image.
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
