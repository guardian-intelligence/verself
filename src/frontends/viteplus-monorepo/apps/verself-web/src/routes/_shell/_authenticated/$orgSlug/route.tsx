import { createFileRoute, Outlet } from "@tanstack/react-router";
import { loadActiveOrganizationRoute } from "~/features/shell/org-route-loaders";

export const Route = createFileRoute("/_shell/_authenticated/$orgSlug")({
  beforeLoad: ({ context, location, params }) =>
    loadActiveOrganizationRoute({
      auth: context.auth,
      href: location.href,
      orgSlug: params.orgSlug,
      queryClient: context.queryClient,
    }),
  component: OrganizationOutlet,
});

function OrganizationOutlet() {
  return <Outlet />;
}
