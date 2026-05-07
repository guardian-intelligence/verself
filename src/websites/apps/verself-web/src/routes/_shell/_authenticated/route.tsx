import { createFileRoute, Outlet } from "@tanstack/react-router";
import { anonymousAuth, requireAuth } from "@verself/auth-web/isomorphic";

// Pathless auth gate nested inside _shell. All routes that require a
// signed-in user live under here. The shell layout (sidebar + top bar)
// is supplied by the parent _shell route, so this file only enforces
// authentication — no chrome, no providers.
export const Route = createFileRoute("/_shell/_authenticated")({
  beforeLoad: ({ context, location }) => ({
    auth: requireAuth(context?.auth ?? anonymousAuth, location.href),
  }),
  component: AuthenticatedOutlet,
});

function AuthenticatedOutlet() {
  return <Outlet />;
}
