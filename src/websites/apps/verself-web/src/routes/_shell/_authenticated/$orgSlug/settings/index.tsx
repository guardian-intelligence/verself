import { createFileRoute, redirect } from "@tanstack/react-router";
import { orgPath } from "~/features/shell/org-routing";

// Bare /settings has no content of its own; always land on the self-scoped
// profile settings surface.
export const Route = createFileRoute("/_shell/_authenticated/$orgSlug/settings/")({
  beforeLoad: ({ params }) => {
    throw redirect({ href: orgPath(params.orgSlug, "/settings/profile"), replace: true });
  },
});
