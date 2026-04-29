import { createFileRoute, redirect, useHydrated } from "@tanstack/react-router";
import { LogIn } from "lucide-react";
import { SignInButton } from "@verself/auth-web/components";
import { Button } from "@verself/ui/components/ui/button";

const defaultSignedInRedirect = "/executions";

function signedInRedirectTarget(redirectTo: string | undefined): string {
  if (!redirectTo) return defaultSignedInRedirect;
  try {
    const base = new URL("https://console.invalid");
    const parsed = new URL(redirectTo, base);
    if (parsed.origin !== base.origin) return defaultSignedInRedirect;
    if (["/login", "/callback", "/logout"].includes(parsed.pathname)) {
      return defaultSignedInRedirect;
    }
    return `${parsed.pathname}${parsed.search}${parsed.hash}`;
  } catch {
    return defaultSignedInRedirect;
  }
}

export const Route = createFileRoute("/login")({
  validateSearch: (search: Record<string, unknown>) => ({
    redirect: typeof search.redirect === "string" ? search.redirect : undefined,
  }),
  beforeLoad: ({ context, search }) => {
    if (context.auth.isAuthenticated) {
      throw redirect({ href: signedInRedirectTarget(search.redirect), replace: true });
    }
  },
  component: LoginPage,
});

function LoginPage() {
  const { redirect } = Route.useSearch();
  const hydrated = useHydrated();
  const buttonContent = (
    <>
      <LogIn aria-hidden="true" />
      <span>Continue to sign in</span>
    </>
  );

  return (
    <main className="grid min-h-svh place-items-center px-6 py-16">
      <section className="flex w-full max-w-sm flex-col items-center text-center">
        <div className="mb-5 flex size-10 items-center justify-center rounded-md border border-border bg-muted text-sm font-semibold">
          VS
        </div>
        <h1 className="text-2xl font-semibold tracking-tight">Sign in to Console</h1>
        <p className="mt-3 text-sm leading-6 text-muted-foreground">
          Authentication continues with the hosted identity service.
        </p>
        {hydrated ? (
          <SignInButton {...(redirect ? { redirectTo: redirect } : {})} className="mt-6">
            {buttonContent}
          </SignInButton>
        ) : (
          <Button type="button" disabled className="mt-6">
            {buttonContent}
          </Button>
        )}
      </section>
    </main>
  );
}
