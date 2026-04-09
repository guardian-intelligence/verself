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
      { title: "Webmail" },
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
        <div className="h-screen flex flex-col">
          <Header />
          <main className="flex-1 overflow-hidden">{children}</main>
        </div>
        <Scripts />
      </body>
    </html>
  );
}

function Header() {
  const viewer = Route.useLoaderData();

  return (
    <header className="border-b border-border bg-card shrink-0">
      <div className="px-4 flex items-center h-12 gap-4">
        <Link to="/" className="flex items-center gap-2 shrink-0">
          <svg viewBox="0 0 24 24" className="w-6 h-6 text-primary" fill="none" stroke="currentColor" strokeWidth="1.5">
            <path d="M21.75 6.75v10.5a2.25 2.25 0 0 1-2.25 2.25h-15a2.25 2.25 0 0 1-2.25-2.25V6.75m19.5 0A2.25 2.25 0 0 0 19.5 4.5h-15a2.25 2.25 0 0 0-2.25 2.25m19.5 0v.243a2.25 2.25 0 0 1-1.07 1.916l-7.5 4.615a2.25 2.25 0 0 1-2.36 0L3.32 8.91a2.25 2.25 0 0 1-1.07-1.916V6.75" />
          </svg>
          <span className="font-semibold text-base hidden sm:inline">Mail</span>
        </Link>

        {/* Search bar (visual placeholder) */}
        <div className="flex-1 max-w-xl mx-auto">
          <div className="flex items-center gap-2 bg-muted rounded-lg px-3 py-1.5">
            <svg viewBox="0 0 20 20" className="w-4 h-4 text-muted-foreground shrink-0" fill="currentColor">
              <path fillRule="evenodd" d="M9 3.5a5.5 5.5 0 100 11 5.5 5.5 0 000-11zM2 9a7 7 0 1112.452 4.391l3.328 3.329a.75.75 0 11-1.06 1.06l-3.329-3.328A7 7 0 012 9z" clipRule="evenodd" />
            </svg>
            <span className="text-sm text-muted-foreground select-none">Search mail</span>
          </div>
        </div>

        <div className="shrink-0">
          <AuthButton viewer={viewer} />
        </div>
      </div>
    </header>
  );
}

function AuthButton({ viewer }: { viewer: Awaited<ReturnType<typeof getViewer>> }) {
  const currentLocation = useRouterState({
    select: (state) => state.location.href,
  });
  const loginHref = `/login?redirect=${encodeURIComponent(currentLocation)}`;

  if (!viewer) {
    return (
      <a href={loginHref} className="px-3 py-1.5 rounded-md bg-primary text-primary-foreground hover:opacity-90 text-sm font-medium">
        Sign in
      </a>
    );
  }

  const initials = viewer.name
    ? viewer.name.split(" ").map((w) => w[0]).join("").toUpperCase().slice(0, 2)
    : (viewer.email?.[0] ?? "?").toUpperCase();

  return (
    <div className="flex items-center gap-3">
      <a
        href="/logout"
        className="text-xs text-muted-foreground hover:text-foreground"
        title="Sign out"
      >
        Sign out
      </a>
      <div
        className="w-8 h-8 rounded-full bg-primary text-primary-foreground flex items-center justify-center text-xs font-medium"
        title={viewer.email ?? viewer.sub}
      >
        {initials}
      </div>
    </div>
  );
}
