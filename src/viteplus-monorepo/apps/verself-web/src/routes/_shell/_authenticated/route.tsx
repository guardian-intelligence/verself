import { createFileRoute, Outlet } from "@tanstack/react-router";
import { anonymousAuth, requireAuth } from "@verself/auth-web/isomorphic";
import { SignInButton } from "@verself/auth-web/components";
import { useAuth } from "@verself/auth-web/react";
import { LogIn } from "lucide-react";

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
  if (!auth.isSignedIn) {
    return (
      <main className="grid min-h-svh place-items-center px-6 py-16">
        <section className="flex w-full max-w-sm flex-col items-center text-center">
          <div className="mb-5 flex size-10 items-center justify-center rounded-md border border-border bg-muted text-sm font-semibold">
            VS
          </div>
          <h1 className="text-2xl font-semibold tracking-tight">Sign in to Console</h1>
          <p className="mt-3 text-sm leading-6 text-muted-foreground">
            Your browser session is no longer signed in.
          </p>
          <SignInButton className="mt-6">
            <LogIn aria-hidden="true" />
            <span>Continue to sign in</span>
          </SignInButton>
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
