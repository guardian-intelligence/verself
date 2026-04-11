import { createFileRoute } from "@tanstack/react-router";
import { OrganizationWidget } from "~/features/organization/components";
import { loadOrganizationPage } from "~/features/organization/queries";

export const Route = createFileRoute("/_authenticated/organization/")({
  loader: ({ context }) => loadOrganizationPage(context.queryClient, context.auth),
  component: OrganizationPage,
});

function OrganizationPage() {
  return <OrganizationWidget />;
}
