import { createFileRoute } from "@tanstack/react-router";
import { AuthProvider } from "@verself/auth-web/react";
import { parseAuthSnapshot, syncAuthPartitionedCache } from "@verself/auth-web/isomorphic";
import { IAMApiProvider } from "@verself/auth-web/components";
import { iamApiClient } from "~/lib/iam-api-client";
import { AppShell } from "~/features/shell/app-shell";
import { getClientAuthSnapshot } from "~/server-fns/auth";

const authNavigationClient = {
  getSignInRedirectURL: async ({ data }: { data: { redirectTo?: string | null } }) => {
    const params = new URLSearchParams();
    if (data.redirectTo) {
      params.set("redirect_to", data.redirectTo);
    }
    const query = params.toString();
    return `/api/v1/auth/login${query ? `?${query}` : ""}`;
  },
  getSignOutRedirectURL: async () => "/api/v1/auth/logout",
};

// Pathless layout for the signed-in console surface. The auth fetch lives
// here (not at __root) so public /docs and /policy refreshes do not pay a
// server round-trip for a snapshot they will never read. _shell/_authenticated
// nests under this and enforces the actual auth gate.
export const Route = createFileRoute("/_shell")({
  beforeLoad: async ({ context }) => {
    const authSnapshot = await getClientAuthSnapshot();
    syncAuthPartitionedCache(context.queryClient, authSnapshot);
    return authSnapshot;
  },
  component: ShellLayout,
});

function ShellLayout() {
  const snapshot = parseAuthSnapshot(Route.useRouteContext());
  // Keying IAMApiProvider on the cache partition remounts the shell subtree
  // when a user signs in/out, discarding signed-in component state alongside
  // the cache reset that syncAuthPartitionedCache performs in beforeLoad.
  const partitionKey = `auth:${snapshot.auth.cachePartition ?? "anonymous"}`;

  return (
    <AuthProvider client={authNavigationClient} snapshot={snapshot}>
      <IAMApiProvider key={partitionKey} client={iamApiClient}>
        <AppShell />
      </IAMApiProvider>
    </AuthProvider>
  );
}
