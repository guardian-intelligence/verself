import { createFileRoute, Outlet } from "@tanstack/react-router";
import { requireAuthenticatedAuthState } from "@forge-metal/auth-web";

export const Route = createFileRoute("/_authenticated")({
  beforeLoad: ({ context, location }) => ({
    authState: requireAuthenticatedAuthState(context.authState, location.href),
  }),
  component: AuthenticatedLayout,
});

function AuthenticatedLayout() {
  return <Outlet />;
}
