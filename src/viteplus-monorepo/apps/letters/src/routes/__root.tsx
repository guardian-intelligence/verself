import {
  ClientOnly,
  createRootRouteWithContext,
  HeadContent,
  Link,
  Outlet,
  Scripts,
} from "@tanstack/react-router";
import { QueryClientProvider, useQuery, type QueryClient } from "@tanstack/react-query";
import { type ReactNode } from "react";
import { getUser, signIn, signOut } from "~/lib/auth";
import { keys } from "~/lib/query-keys";
import "~/styles/app.css";

declare const process: { env: Record<string, string | undefined> } | undefined;

export const Route = createRootRouteWithContext<{
  queryClient: QueryClient;
}>()({
  component: RootComponent,
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
  const envJson =
    typeof window === "undefined" && typeof process !== "undefined"
      ? JSON.stringify({
          AUTH_ISSUER_URL: process.env.AUTH_ISSUER_URL || "https://auth.anveio.com",
          AUTH_CLIENT_ID: process.env.AUTH_CLIENT_ID || "",
        })
      : "{}";

  return (
    <html lang="en">
      <head>
        <HeadContent />
        <link
          href="https://fonts.googleapis.com/css2?family=Playfair+Display:wght@400;700;900&display=swap"
          rel="stylesheet"
        />
        <script
          suppressHydrationWarning
          dangerouslySetInnerHTML={{
            __html: `window.__ENV__=${envJson}`,
          }}
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
  return (
    <nav className="border-b border-border">
      <div className="max-w-3xl mx-auto px-6 flex items-center h-14 gap-6">
        <Link to="/" className="text-xl font-bold tracking-tight" style={{ fontFamily: "'Playfair Display', serif" }}>
          Letters
        </Link>
        <div className="ml-auto flex items-center gap-4">
          <ClientOnly fallback={null}>
            <AuthButtonInner />
          </ClientOnly>
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

function AuthButtonInner() {
  const { data: user } = useQuery({
    queryKey: keys.user(),
    queryFn: getUser,
    staleTime: Infinity,
    refetchOnWindowFocus: true,
  });

  if (!user) {
    return (
      <button
        onClick={() => signIn()}
        className="px-3 py-1.5 rounded-md border border-border hover:bg-muted text-sm"
      >
        Sign in
      </button>
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
        {user.profile?.email ?? user.profile?.sub}
      </span>
      <button onClick={() => signOut()} className="text-muted-foreground hover:text-foreground">
        Sign out
      </button>
    </div>
  );
}
