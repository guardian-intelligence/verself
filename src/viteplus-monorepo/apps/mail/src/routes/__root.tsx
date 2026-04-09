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
      <div className="px-4 flex items-center h-12 gap-6">
        <Link to="/" className="font-semibold text-lg">
          Webmail
        </Link>
        <div className="ml-auto">
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
