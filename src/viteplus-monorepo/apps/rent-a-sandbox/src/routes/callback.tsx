import { createFileRoute, redirect } from "@tanstack/react-router";
import { getSignInCallbackRedirectURL } from "~/server-fns/auth";

export const Route = createFileRoute("/callback")({
  beforeLoad: async () => {
    const redirectTo = await getSignInCallbackRedirectURL();
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
