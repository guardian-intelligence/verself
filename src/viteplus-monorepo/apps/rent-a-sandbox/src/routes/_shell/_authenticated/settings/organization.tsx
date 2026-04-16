import { createFileRoute } from "@tanstack/react-router";
import { OrganizationProfile, loadOrganizationPage } from "@forge-metal/auth-web/components";
import { identityApiClient } from "~/lib/identity-api-client";

export const Route = createFileRoute("/_shell/_authenticated/settings/organization")({
  loader: ({ context }) =>
    loadOrganizationPage(context.queryClient, context.auth, identityApiClient),
  component: OrganizationProfile,
});
