import { createFileRoute, redirect } from "@tanstack/react-router";
import { getSignOutRedirectURL } from "~/server-fns/auth";

async function getRouteSignOutRedirectURL(): Promise<string> {
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
