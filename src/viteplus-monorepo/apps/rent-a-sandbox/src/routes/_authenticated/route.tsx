import { createFileRoute, Outlet } from "@tanstack/react-router";
import { anonymousAuth, requireAuth } from "@forge-metal/auth-web/isomorphic";

export const Route = createFileRoute("/_authenticated")({
  beforeLoad: ({ context, location }) => ({
    auth: requireAuth(context?.auth ?? anonymousAuth, location.href),
  }),
  component: AuthenticatedLayout,
});

function AuthenticatedLayout() {
  return <Outlet />;
}
