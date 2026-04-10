import { createFileRoute } from "@tanstack/react-router";
import { ExecutionDetailPanel } from "~/features/jobs/components";
import { loadExecutionDetail } from "~/features/jobs/queries";

export const Route = createFileRoute("/_authenticated/jobs/$jobId")({
  loader: ({ context, params }) =>
    loadExecutionDetail(context.queryClient, context.auth, params.jobId),
  component: JobDetailPage,
});

function JobDetailPage() {
  const { jobId } = Route.useParams();
  return <ExecutionDetailPanel jobId={jobId} />;
}
