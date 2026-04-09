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
      { title: "Letters" },
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
        <link
          href="https://fonts.googleapis.com/css2?family=Playfair+Display:wght@400;700;900&display=swap"
          rel="stylesheet"
        />
      </head>
      <body className="font-sans antialiased">
        <div className="min-h-screen flex flex-col">
          <Nav />
          <main className="flex-1">{children}</main>
          <Footer />
        </div>
        <Scripts />
      </body>
    </html>
  );
}

function Nav() {
  const viewer = Route.useLoaderData();

  return (
    <nav className="border-b border-border">
      <div className="max-w-3xl mx-auto px-6 flex items-center h-14 gap-6">
        <Link to="/" className="text-xl font-bold tracking-tight" style={{ fontFamily: "'Playfair Display', serif" }}>
          Letters
        </Link>
        <div className="ml-auto flex items-center gap-4">
          <AuthButton viewer={viewer} />
        </div>
      </div>
    </nav>
  );
}

function Footer() {
  return (
    <footer className="border-t border-border mt-16">
      <div className="max-w-3xl mx-auto px-6 py-8 text-sm text-muted-foreground">
        <p>Self-hosted on forge-metal</p>
      </div>
    </footer>
  );
}

function AuthButton({ viewer }: { viewer: Awaited<ReturnType<typeof getViewer>> }) {
  const currentLocation = useRouterState({
    select: (state) => state.location.href,
  });
  const loginHref = `/login?redirect=${encodeURIComponent(currentLocation)}`;

  if (!viewer) {
    return (
      <a href={loginHref} className="px-3 py-1.5 rounded-md border border-border hover:bg-muted text-sm">
        Sign in
      </a>
    );
  }

  return (
    <div className="flex items-center gap-3 text-sm">
      <Link
        to="/editor"
        className="px-3 py-1.5 rounded-md bg-foreground text-background hover:bg-foreground/90 text-sm"
      >
        Write
      </Link>
      <span className="text-muted-foreground truncate max-w-[150px]">
        {viewer.email ?? viewer.sub}
      </span>
      <a href="/logout" className="text-muted-foreground hover:text-foreground">
        Sign out
      </a>
    </div>
  );
}
