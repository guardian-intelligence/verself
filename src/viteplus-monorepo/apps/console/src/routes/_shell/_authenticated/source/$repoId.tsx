import { createFileRoute, redirect } from "@tanstack/react-router";

export const Route = createFileRoute("/_shell/_authenticated/source/$repoId")({
  beforeLoad: () => {
    throw redirect({ to: "/source" });
  },
  component: SourceRepositoryRedirect,
});

function SourceRepositoryRedirect() {
  return null;
}
