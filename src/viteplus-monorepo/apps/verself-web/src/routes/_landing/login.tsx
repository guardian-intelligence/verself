import { createFileRoute, Outlet, redirect, useLocation } from "@tanstack/react-router";
import * as v from "valibot";

import { getClientAuthSnapshot } from "~/server-fns/auth";
import { resolveDefaultSignedInPath } from "~/features/shell/org-route-loaders";

type LoginSearch = {
  readonly authRequest?: string;
  readonly prompt?: "login" | "select_account";
  readonly redirect?: string;
  readonly purpose?: string;
  readonly login_hint?: string;
  readonly required_subject?: string;
  readonly required_email?: string;
  readonly required_org_id?: string;
};

const optionalSearchString = v.pipe(
  v.unknown(),
  v.transform((value) => (typeof value === "string" && value.trim() ? value.trim() : undefined)),
);

const loginPromptSearch = v.pipe(
  v.unknown(),
  v.transform((value) => (value === "login" || value === "select_account" ? value : undefined)),
);

const loginSearchSchema = v.pipe(
  v.object({
    authRequest: v.optional(optionalSearchString),
    auth_request: v.optional(optionalSearchString),
    prompt: v.optional(loginPromptSearch),
    redirect: v.optional(optionalSearchString),
    purpose: v.optional(optionalSearchString),
    login_hint: v.optional(optionalSearchString),
    required_subject: v.optional(optionalSearchString),
    required_email: v.optional(optionalSearchString),
    required_org_id: v.optional(optionalSearchString),
  }),
  v.transform(
    (parsed): LoginSearch =>
      withoutUndefined({
        authRequest: parsed.authRequest ?? parsed.auth_request,
        prompt: parsed.prompt,
        redirect: parsed.redirect,
        purpose: parsed.purpose,
        login_hint: parsed.login_hint,
        required_subject: parsed.required_subject,
        required_email: parsed.required_email,
        required_org_id: parsed.required_org_id,
      }),
  ),
);

function withoutUndefined<T extends Record<string, unknown>>(
  value: T,
): {
  readonly [K in keyof T]?: Exclude<T[K], undefined>;
} {
  return Object.fromEntries(Object.entries(value).filter(([, item]) => item !== undefined)) as {
    readonly [K in keyof T]?: Exclude<T[K], undefined>;
  };
}

export const Route = createFileRoute("/_landing/login")({
  validateSearch: loginSearchSchema,
  pendingComponent: () => null,
  beforeLoad: async ({ context, search }) => {
    const snapshot = await getClientAuthSnapshot();
    if (
      snapshot.auth.isAuthenticated &&
      !search.authRequest &&
      !search.prompt &&
      !hasLoginConstraint(search)
    ) {
      const fallback = await resolveDefaultSignedInPath(context.queryClient, snapshot.auth);
      throw redirect({
        href: fallback,
        replace: true,
      });
    }
  },
  component: LoginSheetStateRoute,
});

function LoginSheetStateRoute() {
  const { pathname } = useLocation();

  if (pathname === "/login") {
    return <LoginLandingContent />;
  }

  return <Outlet />;
}

function LoginLandingContent() {
  return (
    <section className="pb-2">
      <p className="text-sm text-muted-foreground">
        GitHub should be open in another tab or window. Finish sign in there and this page will
        update as soon as you&apos;re done.
      </p>
    </section>
  );
}

function hasLoginConstraint(search: LoginSearch): boolean {
  return Boolean(search.required_subject || search.required_email || search.required_org_id);
}
