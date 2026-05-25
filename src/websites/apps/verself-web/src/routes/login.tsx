import { createFileRoute, redirect, useHydrated } from "@tanstack/react-router";
import { LogIn } from "lucide-react";
import { SignInButton } from "@verself/auth-web/components";
import { Button } from "@verself/ui/components/ui/button";
import { resolveDefaultSignedInPath } from "~/features/shell/org-route-loaders";
import { getClientAuthSnapshot } from "~/server-fns/auth";

type LoginSearch = {
  readonly prompt?: "login" | "select_account";
  readonly redirect?: string;
  readonly purpose?: string;
  readonly login_hint?: string;
  readonly required_subject?: string;
  readonly required_email?: string;
  readonly required_org_id?: string;
};

function loginSearch(search: Record<string, unknown>): LoginSearch {
  return {
    ...(search.prompt === "login" || search.prompt === "select_account"
      ? { prompt: search.prompt }
      : {}),
    ...(typeof search.redirect === "string" ? { redirect: search.redirect } : {}),
    ...(typeof search.purpose === "string" ? { purpose: search.purpose } : {}),
    ...(typeof search.login_hint === "string" ? { login_hint: search.login_hint } : {}),
    ...(typeof search.required_subject === "string"
      ? { required_subject: search.required_subject }
      : {}),
    ...(typeof search.required_email === "string" ? { required_email: search.required_email } : {}),
    ...(typeof search.required_org_id === "string"
      ? { required_org_id: search.required_org_id }
      : {}),
  };
}

function rawParam(params: URLSearchParams, key: string): string | undefined {
  const value = params.get(key)?.trim();
  return value ? value : undefined;
}

function loginSearchParams(params: URLSearchParams): LoginSearch {
  const prompt = rawParam(params, "prompt");
  const redirect = rawParam(params, "redirect");
  const purpose = rawParam(params, "purpose");
  const loginHint = rawParam(params, "login_hint");
  const requiredSubject = rawParam(params, "required_subject");
  const requiredEmail = rawParam(params, "required_email");
  const requiredOrgId = rawParam(params, "required_org_id");
  return {
    ...(prompt === "login" || prompt === "select_account" ? { prompt } : {}),
    ...(redirect ? { redirect } : {}),
    ...(purpose ? { purpose } : {}),
    ...(loginHint ? { login_hint: loginHint } : {}),
    ...(requiredSubject ? { required_subject: requiredSubject } : {}),
    ...(requiredEmail ? { required_email: requiredEmail } : {}),
    ...(requiredOrgId ? { required_org_id: requiredOrgId } : {}),
  };
}

function hydratedLoginSearch(fallback: LoginSearch): LoginSearch {
  if (typeof window === "undefined") {
    return fallback;
  }
  // TanStack's search parser can coerce numeric-looking IDs before validation.
  return loginSearchParams(new URLSearchParams(window.location.search));
}

export const Route = createFileRoute("/login")({
  validateSearch: loginSearch,
  beforeLoad: async ({ context, search }) => {
    const snapshot = await getClientAuthSnapshot();
    if (snapshot.auth.isAuthenticated && !search.prompt) {
      const fallback = await resolveDefaultSignedInPath(context.queryClient, snapshot.auth);
      throw redirect({
        href: fallback,
        replace: true,
      });
    }
  },
  component: LoginPage,
});

function LoginPage() {
  const hydrated = useHydrated();
  const routeSearch = Route.useSearch();
  const {
    prompt,
    redirect,
    purpose,
    login_hint,
    required_subject,
    required_email,
    required_org_id,
  } = hydrated ? hydratedLoginSearch(routeSearch) : routeSearch;
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
          Use your Verself account to continue.
        </p>
        {hydrated ? (
          <SignInButton
            {...(prompt === "login" ? { promptLogin: true } : {})}
            {...(prompt === "select_account" ? { promptSelectAccount: true } : {})}
            {...(redirect ? { redirectTo: redirect } : {})}
            {...(purpose ? { purpose } : {})}
            {...(login_hint ? { loginHint: login_hint } : {})}
            {...(required_subject ? { requiredSubject: required_subject } : {})}
            {...(required_email ? { requiredEmail: required_email } : {})}
            {...(required_org_id ? { requiredOrgId: required_org_id } : {})}
            className="mt-6"
          >
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
