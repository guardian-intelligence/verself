import { createFileRoute, redirect } from "@tanstack/react-router";
import { MarketingLandingPage } from "~/features/landing/marketing-page";
import { resolveDefaultSignedInPath } from "~/features/shell/org-route-loaders";
import { getClientAuthSnapshot } from "~/server-fns/auth";

// Top-level marketing landing — deliberately outside _shell so the unauthed
// surface doesn't pick up sidebar/topbar product chrome. Signed-in users are
// redirected straight to the working surface.
export const Route = createFileRoute("/")({
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
  return <MarketingLandingPage />;
}
