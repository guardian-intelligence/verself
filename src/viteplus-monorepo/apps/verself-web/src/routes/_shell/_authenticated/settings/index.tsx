import { createFileRoute, redirect } from "@tanstack/react-router";

// Bare /settings has no content of its own; always land on the self-scoped
// profile settings surface.
export const Route = createFileRoute("/_shell/_authenticated/settings/")({
  beforeLoad: () => {
    throw redirect({ to: "/settings/profile" });
  },
});
