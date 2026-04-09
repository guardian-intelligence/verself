import { createFileRoute, Outlet } from "@tanstack/react-router";
import { requireViewer } from "~/lib/protected-route";

export const Route = createFileRoute("/_authenticated")({
  beforeLoad: ({ location }) => requireViewer(location.href),
  component: AuthenticatedLayout,
});

function AuthenticatedLayout() {
  return <Outlet />;
}
