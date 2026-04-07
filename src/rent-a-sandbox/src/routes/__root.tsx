import {
  createRootRouteWithContext,
  HeadContent,
  Link,
  Outlet,
  Scripts,
} from "@tanstack/react-router";
import { QueryClientProvider, type QueryClient } from "@tanstack/react-query";
import { type ReactNode, useEffect, useState } from "react";
import { getUser, signIn, signOut, type User } from "~/lib/auth";
import "~/styles/globals.css";

declare const process: { env: Record<string, string | undefined> } | undefined;

export const Route = createRootRouteWithContext<{
  queryClient: QueryClient;
}>()({
  component: RootComponent,
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
  // Inject server env vars for the client. dangerouslySetInnerHTML is not
  // diffed during hydration so the server-rendered values persist.
  const envJson =
    typeof window === "undefined" && typeof process !== "undefined"
      ? JSON.stringify({
          AUTH_ISSUER_URL:
            process.env.AUTH_ISSUER_URL || "https://auth.anveio.com",
          AUTH_CLIENT_ID: process.env.AUTH_CLIENT_ID || "",
        })
      : "{}";

  return (
    <html lang="en">
      <head>
        <HeadContent />
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
          <main className="flex-1 max-w-6xl mx-auto w-full px-4 py-6">
            {children}
          </main>
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
            to="/jobs"
            className="text-muted-foreground hover:text-foreground [&.active]:text-foreground"
          >
            Sandboxes
          </Link>
          <Link
            to="/billing"
            className="text-muted-foreground hover:text-foreground [&.active]:text-foreground"
          >
            Billing
          </Link>
        </div>
        <div className="ml-auto">
          <AuthButton />
        </div>
      </div>
    </nav>
  );
}

/** Client-only auth button. Uses useState/useEffect instead of useQuery
 *  to avoid the SSR crash (QueryClientProvider not mounted in root). */
function AuthButton() {
  const [user, setUser] = useState<User | null>(null);
  const [mounted, setMounted] = useState(false);

  useEffect(() => {
    setMounted(true);
    getUser().then(setUser);
    // Re-check auth state when the tab regains focus (silent renew may have fired).
    const onFocus = () => getUser().then(setUser);
    window.addEventListener("focus", onFocus);
    return () => window.removeEventListener("focus", onFocus);
  }, []);

  if (!mounted) return null;

  if (!user) {
    return (
      <button
        onClick={() => signIn()}
        className="px-3 py-1.5 rounded-md border border-border hover:bg-accent text-sm"
      >
        Sign in
      </button>
    );
  }

  return (
    <div className="flex items-center gap-3 text-sm">
      <span className="text-muted-foreground truncate max-w-[200px]">
        {user.profile?.email ?? user.profile?.sub}
      </span>
      <button
        onClick={() => signOut()}
        className="text-muted-foreground hover:text-foreground"
      >
        Sign out
      </button>
    </div>
  );
}
