import { createFileRoute } from "@tanstack/react-router";
import { createGuardianAuthorizationRedirect } from "~/features/guardian-oidc-canary/server";

export const Route = createFileRoute("/_canary/guardian-oidc/start")({
  server: {
    handlers: {
      GET: async ({ request }) => createGuardianAuthorizationRedirect(request),
    },
  },
});
