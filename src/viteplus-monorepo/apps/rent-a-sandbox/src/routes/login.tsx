import { createFileRoute, redirect } from "@tanstack/react-router";
import { getSignInRedirectURL } from "~/server-fns/auth";

async function getRouteSignInRedirectURL(redirectTo: string | undefined): Promise<string> {
  if (import.meta.env.SSR) {
    const [{ beginLogin }, { getAuthConfig }] = await Promise.all([
      import("@forge-metal/auth-web/server"),
      import("../server/auth"),
    ]);
    return beginLogin(await getAuthConfig(), redirectTo);
  }
  return getSignInRedirectURL({
    data: redirectTo ? { redirectTo } : {},
  });
}

export const Route = createFileRoute("/login")({
  validateSearch: (search: Record<string, unknown>) => ({
    redirect: typeof search.redirect === "string" ? search.redirect : undefined,
  }),
  beforeLoad: async ({ search }) => {
    const authURL = await getRouteSignInRedirectURL(search.redirect);
    throw redirect({ href: authURL });
  },
  component: LoginPage,
});

function LoginPage() {
  return (
    <div className="flex flex-col items-center justify-center py-24 gap-4">
      <h1 className="text-2xl font-bold">Redirecting to sign in...</h1>
      <p className="text-muted-foreground">If you are not redirected, reload this page.</p>
    </div>
  );
}
