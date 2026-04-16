import { createFileRoute } from "@tanstack/react-router";
import { ExecutionDetailPanel } from "~/features/executions/components";
import { loadExecutionDetail } from "~/features/executions/queries";

export const Route = createFileRoute("/_shell/_authenticated/executions/$executionId")({
  loader: ({ context, params }) =>
    loadExecutionDetail(context.queryClient, context.auth, params.executionId),
  component: ExecutionDetailPage,
});

function ExecutionDetailPage() {
  const { executionId } = Route.useParams();
  return <ExecutionDetailPanel executionId={executionId} />;
}
