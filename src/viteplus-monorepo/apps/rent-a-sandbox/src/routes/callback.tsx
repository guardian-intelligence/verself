import { createFileRoute, redirect } from "@tanstack/react-router";
import { getSignInCallbackRedirectURL } from "~/server-fns/auth";

async function getRouteSignInCallbackRedirectURL(): Promise<string> {
  if (import.meta.env.SSR) {
    const [{ finishLogin }, { getAuthConfig }] = await Promise.all([
      import("@forge-metal/auth-web/server"),
      import("../server/auth"),
    ]);
    const { redirectTo } = await finishLogin(getAuthConfig());
    return redirectTo;
  }
  return getSignInCallbackRedirectURL();
}

export const Route = createFileRoute("/callback")({
  beforeLoad: async () => {
    const redirectTo = await getRouteSignInCallbackRedirectURL();
    throw redirect({ href: redirectTo });
  },
  component: CallbackPage,
});

function CallbackPage() {
  return (
    <div className="flex items-center justify-center py-24">
      <p className="text-muted-foreground">Completing sign in...</p>
    </div>
  );
}
