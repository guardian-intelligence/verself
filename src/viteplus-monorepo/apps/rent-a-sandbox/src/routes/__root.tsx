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
import { getViewer } from "~/server-fns/auth";
import "~/styles/app.css";

export const Route = createRootRouteWithContext<{
  queryClient: QueryClient;
}>()({
  component: RootComponent,
  loader: () => getViewer(),
  head: () => ({
    meta: [
      { charSet: "utf-8" },
      { name: "viewport", content: "width=device-width, initial-scale=1" },
      { title: "Rent-a-Sandbox" },
    ],
  }),
});

function RootComponent() {
  const { queryClient } = Route.useRouteContext();
  return (
    <QueryClientProvider client={queryClient}>
      <RootDocument>
        <Outlet />
      </RootDocument>
    </QueryClientProvider>
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
        <Link
          to="/"
          search={{ purchased: false, subscribed: false }}
          className="font-semibold text-lg"
        >
          Rent-a-Sandbox
        </Link>
        <div className="flex gap-4 text-sm">
          <Link
            to="/"
            search={{ purchased: false, subscribed: false }}
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
            search={{ purchased: false, subscribed: false }}
            className="text-muted-foreground hover:text-foreground [&.active]:text-foreground"
          >
            Billing
          </Link>
        </div>
        <div className="ml-auto">
          <AuthButton viewer={Route.useLoaderData()} />
        </div>
      </div>
    </nav>
  );
}

function AuthButton({ viewer }: { viewer: Awaited<ReturnType<typeof getViewer>> }) {
  const currentLocation = useRouterState({
    select: (state) => state.location.href,
  });
  const loginHref = `/login?redirect=${encodeURIComponent(currentLocation)}`;

  if (!viewer) {
    return (
      <a href={loginHref} className="px-3 py-1.5 rounded-md border border-border hover:bg-accent text-sm">
        Sign in
      </a>
    );
  }

  return (
    <div className="flex items-center gap-3 text-sm">
      <span className="text-muted-foreground truncate max-w-[200px]">
        {viewer.email ?? viewer.sub}
      </span>
      <a href="/logout" className="text-muted-foreground hover:text-foreground">
        Sign out
      </a>
    </div>
  );
}
