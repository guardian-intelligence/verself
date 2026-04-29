import { createFileRoute } from "@tanstack/react-router";
import { IdentityApiProvider } from "@verself/auth-web/components";
import { identityApiClient } from "~/lib/identity-api-client";
import { AppShell } from "~/features/shell/app-shell";

// Pathless layout that owns the app chrome (sidebar, top bar, command
// palette, account row) for the signed-in console surface. Auth is NOT
// enforced here; protected leaves nest under _shell/_authenticated which
// carries the auth gate. Public routes (/) render inside the same shell so
// a signed-in user can always jump back to Executions. Docs and policy live
// outside this layout, under the _workshop chrome (workshop register).
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
