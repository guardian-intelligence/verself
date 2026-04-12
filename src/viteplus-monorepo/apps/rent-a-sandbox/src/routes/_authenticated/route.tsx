import { createFileRoute, Outlet } from "@tanstack/react-router";
import { anonymousAuth, requireAuth } from "@forge-metal/auth-web/isomorphic";
import { IdentityApiProvider } from "@forge-metal/auth-web/components";
import { identityApiClient } from "~/lib/identity-api-client";

export const Route = createFileRoute("/_authenticated")({
  beforeLoad: ({ context, location }) => ({
    auth: requireAuth(context?.auth ?? anonymousAuth, location.href),
  }),
  component: AuthenticatedLayout,
});

function AuthenticatedLayout() {
  return (
    <IdentityApiProvider client={identityApiClient}>
      <Outlet />
    </IdentityApiProvider>
  );
}
