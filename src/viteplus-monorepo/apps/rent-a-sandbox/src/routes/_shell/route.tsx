import { createFileRoute } from "@tanstack/react-router";
import { IdentityApiProvider } from "@forge-metal/auth-web/components";
import { identityApiClient } from "~/lib/identity-api-client";
import { AppShell } from "~/features/shell/app-shell";

// Pathless layout that owns the app chrome (sidebar, top bar, command
// palette, account row) for every route — public and authenticated alike.
// Auth is NOT enforced here; protected leaves nest under _shell/_authenticated
// which carries only the auth gate. Public routes (/, /docs) render inside
// the same shell so a guest reading docs can always see the product's shape
// and a signed-in user reading docs can always jump back to Executions.
export const Route = createFileRoute("/_shell")({
  component: ShellLayout,
});

function ShellLayout() {
  return (
    <IdentityApiProvider client={identityApiClient}>
      <AppShell />
    </IdentityApiProvider>
  );
}
