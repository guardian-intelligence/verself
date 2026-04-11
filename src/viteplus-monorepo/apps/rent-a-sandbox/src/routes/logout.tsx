import { createFileRoute, redirect } from "@tanstack/react-router";
import { getSignOutRedirectURL } from "~/server-fns/auth";

async function getRouteSignOutRedirectURL(): Promise<string> {
  if (import.meta.env.SSR) {
    const [{ logout }, { getAuthConfig }] = await Promise.all([
      import("@forge-metal/auth-web/server"),
      import("../server/auth"),
    ]);
    return logout(await getAuthConfig());
  }
  return getSignOutRedirectURL();
}

export const Route = createFileRoute("/logout")({
  beforeLoad: async () => {
    const logoutURL = await getRouteSignOutRedirectURL();
    throw redirect({ href: logoutURL });
  },
  component: LogoutPage,
});

function LogoutPage() {
  return (
    <div className="flex items-center justify-center py-24">
      <p className="text-muted-foreground">Signing out...</p>
    </div>
  );
}
