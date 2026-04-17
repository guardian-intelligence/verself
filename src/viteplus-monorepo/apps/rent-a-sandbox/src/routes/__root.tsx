import { createRootRouteWithContext, HeadContent, Outlet, Scripts } from "@tanstack/react-router";
import { QueryClientProvider, type QueryClient } from "@tanstack/react-query";
import { type ReactNode } from "react";
import { AuthProvider } from "@forge-metal/auth-web/react";
import {
  type Auth,
  type AuthSnapshot,
  authCacheKey,
  parseAuthSnapshot,
  syncAuthPartitionedCache,
} from "@forge-metal/auth-web/isomorphic";
import {
  getClientAuthSnapshot,
  getSignInRedirectURL,
  getSignOutRedirectURL,
} from "~/server-fns/auth";
import "~/styles/app.css";

async function loadAuthSnapshot(): Promise<AuthSnapshot> {
  if (import.meta.env.SSR) {
    const [{ getClientAuthSnapshot: readClientAuthSnapshot }, { getAuthConfig }] =
      await Promise.all([import("@forge-metal/auth-web/server"), import("../server/auth")]);
    return readClientAuthSnapshot(getAuthConfig());
  }
  return getClientAuthSnapshot();
}

export const Route = createRootRouteWithContext<{
  auth: Auth;
  queryClient: QueryClient;
}>()({
  component: RootComponent,
  beforeLoad: async ({ context }) => {
    const authSnapshot = await loadAuthSnapshot();
    syncAuthPartitionedCache(context.queryClient, authSnapshot);
    return authSnapshot;
  },
  head: () => ({
    meta: [
      { charSet: "utf-8" },
      { name: "viewport", content: "width=device-width, initial-scale=1" },
      { title: "Rent-a-Sandbox" },
    ],
  }),
});

// __root.tsx owns the document and global providers (auth + query). All app
// chrome (sidebar, command palette, top bar) lives inside
// _authenticated/route.tsx so that unauthenticated routes — login, callback,
// logout — render without any shell.
function RootComponent() {
  const routeContext = Route.useRouteContext();
  const authSnapshot = parseAuthSnapshot(routeContext);
  return (
    <AuthProvider client={{ getSignInRedirectURL, getSignOutRedirectURL }} snapshot={authSnapshot}>
      <QueryClientProvider client={routeContext.queryClient} key={authCacheKey(authSnapshot)}>
        <RootDocument>
          <Outlet />
        </RootDocument>
      </QueryClientProvider>
    </AuthProvider>
  );
}

function RootDocument({ children }: { children: ReactNode }) {
  return (
    <html lang="en">
      <head>
        <HeadContent />
      </head>
      <body className="bg-background text-foreground font-sans antialiased">
        {children}
        <Scripts />
      </body>
    </html>
  );
}
