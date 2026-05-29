import { createFileRoute, redirect } from "@tanstack/react-router";

import { resolveDefaultSignedInPath } from "~/features/shell/org-route-loaders";
import { getClientAuthSnapshot } from "~/server-fns/auth";

export const Route = createFileRoute("/_landing/")({
  beforeLoad: async ({ context }) => {
    const snapshot = await getClientAuthSnapshot();
    if (snapshot.auth.isAuthenticated) {
      throw redirect({
        href: await resolveDefaultSignedInPath(context.queryClient, snapshot.auth),
        replace: true,
      });
    }
  },
  component: LandingPage,
  head: () => ({
    meta: [{ title: "Verself" }],
  }),
});

function LandingPage() {
  return <></>;
}
