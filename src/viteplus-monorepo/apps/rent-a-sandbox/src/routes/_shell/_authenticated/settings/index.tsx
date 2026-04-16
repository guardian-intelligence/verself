import { createFileRoute, redirect } from "@tanstack/react-router";

// Bare /settings has no content of its own; always land on billing, which
// is the first settings section most users visit. A future /settings/profile
// would be a cheaper landing target once it exists.
export const Route = createFileRoute("/_shell/_authenticated/settings/")({
  beforeLoad: () => {
    throw redirect({ to: "/settings/billing" });
  },
});
