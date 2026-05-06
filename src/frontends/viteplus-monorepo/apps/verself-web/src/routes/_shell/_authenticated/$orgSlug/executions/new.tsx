import { createFileRoute, redirect } from "@tanstack/react-router";
import { orgPath } from "~/features/shell/org-routing";

export const Route = createFileRoute("/_shell/_authenticated/$orgSlug/executions/new")({
  beforeLoad: ({ params }) => {
    throw redirect({ href: orgPath(params.orgSlug, "/executions"), replace: true });
  },
  component: ExecutionsNewRedirect,
});

function ExecutionsNewRedirect() {
  return null;
}
