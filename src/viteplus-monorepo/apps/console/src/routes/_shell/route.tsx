import { createFileRoute } from "@tanstack/react-router";
import { createServerFn } from "@tanstack/react-start";
import { deriveAppBaseURL } from "@forge-metal/web-env";
import { IdentityApiProvider } from "@forge-metal/auth-web/components";
import { identityApiClient } from "~/lib/identity-api-client";
import { AppShell } from "~/features/shell/app-shell";

// Resolve the platform docs origin server-side. Docs live on their own
// subdomain (platform.<domain>) and we never want the console
// bundle to hardcode the operator's domain — reading FORGE_METAL_DOMAIN
// through deriveAppBaseURL keeps the link portable across deployments.
const getPlatformOrigin = createServerFn({ method: "GET" }).handler(() =>
  deriveAppBaseURL("platform"),
);

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
