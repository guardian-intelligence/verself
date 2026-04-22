import { createFileRoute } from "@tanstack/react-router";
import { ExecutionScheduleDetailPanel } from "~/features/schedules/components";
import { loadExecutionScheduleDetail } from "~/features/schedules/queries";

export const Route = createFileRoute("/_shell/_authenticated/schedules/$scheduleId")({
  loader: ({ context, params }) =>
    loadExecutionScheduleDetail(context.queryClient, context.auth, params.scheduleId),
  component: ScheduleDetailPage,
});

function ScheduleDetailPage() {
  const { scheduleId } = Route.useParams();
  return <ExecutionScheduleDetailPanel scheduleId={scheduleId} />;
}
