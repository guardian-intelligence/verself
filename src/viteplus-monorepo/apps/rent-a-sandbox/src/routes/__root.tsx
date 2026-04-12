import {
  createRootRouteWithContext,
  HeadContent,
  Link,
  Outlet,
  Scripts,
  useRouterState,
} from "@tanstack/react-router";
import { QueryClientProvider, type QueryClient } from "@tanstack/react-query";
import { type ReactNode } from "react";
import { AuthProvider } from "@forge-metal/auth-web/react";
import { SignedIn, SignedOut, SignInButton, UserButton } from "@forge-metal/auth-web/components";
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
      <body className="font-sans antialiased">
        <div className="min-h-screen flex flex-col">
          <Nav />
          <main className="flex-1 max-w-6xl mx-auto w-full px-4 py-6">{children}</main>
        </div>
        <Scripts />
      </body>
    </html>
  );
}

function Nav() {
  return (
    <nav className="border-b border-border bg-card">
      <div className="max-w-6xl mx-auto px-4 flex items-center h-14 gap-6">
        <Link to="/" className="font-semibold text-lg">
          Rent-a-Sandbox
        </Link>
        <div className="flex gap-4 text-sm">
          <Link
            to="/"
            className="text-muted-foreground hover:text-foreground [&.active]:text-foreground"
          >
            Dashboard
          </Link>
          <Link
            to="/repos"
            className="text-muted-foreground hover:text-foreground [&.active]:text-foreground"
          >
            Repos
          </Link>
          <Link
            to="/jobs"
            className="text-muted-foreground hover:text-foreground [&.active]:text-foreground"
          >
            Executions
          </Link>
          <Link
            to="/billing"
            className="text-muted-foreground hover:text-foreground [&.active]:text-foreground"
          >
            Billing
          </Link>
          <Link
            to="/organization"
            className="text-muted-foreground hover:text-foreground [&.active]:text-foreground"
          >
            Organization
          </Link>
        </div>
        <div className="ml-auto">
          <AuthButton />
        </div>
      </div>
    </nav>
  );
}

function AuthButton() {
  const currentLocation = useRouterState().location.href;
  return (
    <>
      <SignedOut>
        <SignInButton redirectTo={currentLocation} variant="outline" />
      </SignedOut>
      <SignedIn>
        <div className="flex items-center gap-3">
          <UserButton />
          <a href="/logout" className="text-sm text-muted-foreground hover:text-foreground">
            Sign out
          </a>
        </div>
      </SignedIn>
    </>
  );
}
