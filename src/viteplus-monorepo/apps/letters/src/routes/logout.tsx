import { createFileRoute, redirect } from "@tanstack/react-router";
import { getLogoutRedirectURL } from "~/server-fns/auth";

export const Route = createFileRoute("/logout")({
  beforeLoad: async () => {
    const logoutURL = await getLogoutRedirectURL();
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
