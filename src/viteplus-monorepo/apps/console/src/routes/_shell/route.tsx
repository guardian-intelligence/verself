import { createFileRoute } from "@tanstack/react-router";
import { createServerFn } from "@tanstack/react-start";
import { deriveProductBaseURL } from "@verself/web-env";
import { IdentityApiProvider } from "@verself/auth-web/components";
import { identityApiClient } from "~/lib/identity-api-client";
import { AppShell } from "~/features/shell/app-shell";

// Resolve the product docs origin server-side. Docs live on the product apex,
// not in the console bundle, so deployments can move the hosted domain without
// rebuilding browser code.
const getPlatformOrigin = createServerFn({ method: "GET" }).handler(() => deriveProductBaseURL());

// Pathless layout that owns the app chrome (sidebar, top bar, command
// palette, account row) for every route — public and authenticated alike.
// Auth is NOT enforced here; protected leaves nest under _shell/_authenticated
// which carries only the auth gate. Public routes (/) render inside the
// same shell so a signed-in user can always jump back to Executions.
export const Route = createFileRoute("/_shell")({
  component: ShellLayout,
  loader: () => getPlatformOrigin(),
});

function ShellLayout() {
  const platformOrigin = Route.useLoaderData();
  return (
    <IdentityApiProvider client={identityApiClient}>
      <AppShell platformOrigin={platformOrigin} />
    </IdentityApiProvider>
  );
}
