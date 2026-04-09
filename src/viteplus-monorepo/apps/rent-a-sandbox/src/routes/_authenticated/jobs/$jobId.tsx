import { createFileRoute } from "@tanstack/react-router";
import { requireViewer } from "~/lib/protected-route";
import { executionQuery } from "~/features/jobs/queries";
import { ExecutionDetailPanel } from "~/features/jobs/components";
import { ensureOrNotFound } from "~/lib/query-loader";

export const Route = createFileRoute("/jobs/$jobId")({
  beforeLoad: ({ location }) => requireViewer(location.href),
  loader: ({ context, params }) =>
    ensureOrNotFound(context.queryClient, executionQuery(params.jobId) as any),
  component: JobDetailPage,
});

function JobDetailPage() {
  const { jobId } = Route.useParams();
  return <ExecutionDetailPanel jobId={jobId} />;
}
