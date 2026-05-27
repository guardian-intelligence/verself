import { createFileRoute, Outlet, useLocation } from "@tanstack/react-router";
import { anonymousAuth, requireAuth } from "@verself/auth-web/isomorphic";
import { useAuth } from "@verself/auth-web/react";
import { LogIn } from "lucide-react";
import { Button } from "@verself/ui/components/ui/button";

// Pathless auth gate nested inside _shell. All routes that require a
// signed-in user live under here. The shell layout (sidebar + top bar)
// is supplied by the parent _shell route, so this file only enforces
// authentication — no chrome, no providers.
export const Route = createFileRoute("/_shell/_authenticated")({
  beforeLoad: ({ context, location }) => ({
    auth: requireAuth(context?.auth ?? anonymousAuth, location.href),
  }),
  component: AuthenticatedOutlet,
});

function AuthenticatedOutlet() {
  const auth = useAuth();
  const location = useLocation();
  if (!auth.isSignedIn) {
    const loginURL = `/login?redirect=${encodeURIComponent(location.href)}`;
    return (
      <main className="grid min-h-svh place-items-center px-6 py-16">
        <section className="flex w-full max-w-sm flex-col items-center text-center">
          <div className="mb-5 flex size-10 items-center justify-center rounded-md border border-border bg-muted text-sm font-semibold">
            VS
          </div>
          <h1 className="text-2xl font-semibold tracking-tight">Sign in</h1>
          <p className="mt-3 text-sm leading-6 text-muted-foreground">
            Your browser session is no longer signed in.
          </p>
          <Button type="button" className="mt-6" onClick={() => window.location.assign(loginURL)}>
            <LogIn aria-hidden="true" />
            <span>Sign in</span>
          </Button>
          <a
            href="/"
            className="mt-2 inline-flex h-9 items-center justify-center rounded-md px-4 text-sm font-medium text-muted-foreground hover:bg-muted hover:text-foreground"
          >
            Return home
          </a>
        </section>
      </main>
    );
  }
  return <Outlet />;
}
