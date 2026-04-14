import { createFileRoute } from "@tanstack/react-router";
import { anonymousAuth, requireAuth } from "@forge-metal/auth-web/isomorphic";
import { IdentityApiProvider } from "@forge-metal/auth-web/components";
import { identityApiClient } from "~/lib/identity-api-client";
import { AppShell } from "~/features/shell/app-shell";

export const Route = createFileRoute("/_authenticated")({
  beforeLoad: ({ context, location }) => ({
    auth: requireAuth(context?.auth ?? anonymousAuth, location.href),
  }),
  component: AuthenticatedLayout,
});

function AuthenticatedLayout() {
  return (
    <IdentityApiProvider client={identityApiClient}>
      <AppShell />
    </IdentityApiProvider>
  );
}
