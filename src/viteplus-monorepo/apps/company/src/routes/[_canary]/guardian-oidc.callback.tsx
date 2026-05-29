import { createFileRoute } from "@tanstack/react-router";
import { handleGuardianAuthorizationCallback } from "~/features/guardian-oidc-canary/server";

export const Route = createFileRoute("/_canary/guardian-oidc/callback")({
  server: {
    handlers: {
      GET: async ({ request }) => handleGuardianAuthorizationCallback(request),
    },
  },
});
