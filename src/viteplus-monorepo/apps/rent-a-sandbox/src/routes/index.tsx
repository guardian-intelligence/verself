import { createFileRoute, redirect } from "@tanstack/react-router";

// `/` has no content of its own. Authenticated users land on Executions —
// the first (and currently only) product in the app shell. Guest users
// are forwarded to Zitadel via /login, which already stores the post-login
// redirect target.
export const Route = createFileRoute("/")({
  beforeLoad: ({ context, location }) => {
    if (context?.auth?.isAuthenticated) {
      throw redirect({ to: "/executions" });
    }
    throw redirect({
      to: "/login",
      search: { redirect: location.href },
    });
  },
});
