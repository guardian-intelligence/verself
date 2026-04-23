import { createFileRoute, redirect } from "@tanstack/react-router";

export const Route = createFileRoute("/_shell/_authenticated/executions/new")({
  beforeLoad: () => {
    throw redirect({ to: "/executions" });
  },
  component: ExecutionsNewRedirect,
});

function ExecutionsNewRedirect() {
  return null;
}
