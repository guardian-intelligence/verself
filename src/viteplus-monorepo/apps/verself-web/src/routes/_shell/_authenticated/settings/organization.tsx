import { createFileRoute } from "@tanstack/react-router";
import { OrganizationProfile, loadOrganizationPage } from "@verself/auth-web/components";
import { iamApiClient } from "~/lib/iam-api-client";

export const Route = createFileRoute("/_shell/_authenticated/settings/organization")({
  loader: ({ context }) => loadOrganizationPage(context.queryClient, context.auth, iamApiClient),
  component: OrganizationProfile,
});
