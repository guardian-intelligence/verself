import { createFileRoute } from "@tanstack/react-router";
import { SourceRepositoryDetail } from "~/features/source/components";
import { loadSourceRepositoryDetail } from "~/features/source/queries";

export const Route = createFileRoute("/_shell/_authenticated/source/$repoId")({
  loader: ({ context, params }) =>
    loadSourceRepositoryDetail(context.queryClient, context.auth, params.repoId),
  component: SourceRepositoryPage,
});

function SourceRepositoryPage() {
  const { repoId } = Route.useParams();
  return <SourceRepositoryDetail repoId={repoId} />;
}
