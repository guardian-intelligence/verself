import { createFileRoute, Outlet } from "@tanstack/react-router";
import { requireAuth } from "@forge-metal/auth-web/isomorphic";

export const Route = createFileRoute("/_authenticated")({
  beforeLoad: ({ context, location }) => ({
    auth: requireAuth(context.auth, location.href),
  }),
  component: AuthenticatedLayout,
});

function AuthenticatedLayout() {
  return <Outlet />;
}
