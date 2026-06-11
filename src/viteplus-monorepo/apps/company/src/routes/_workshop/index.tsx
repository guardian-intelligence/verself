import { createFileRoute, useLocation } from "@tanstack/react-router";
import { GuardianGlowDemo } from "~/features/guardian-glow/GuardianGlowDemo";
import { parseGuardianGlowSearch } from "~/features/guardian-glow/guardian-glow-config";
import { ogMeta } from "~/lib/head";

export const Route = createFileRoute("/_workshop/")({
  component: LandingPage,
  head: () => ({
    meta: ogMeta({
      slug: "home",
      title: "Guardian — The world needs your business to succeed.",
      description:
        "Guardian is an American applied intelligence firm. We build the reference architecture for the systems every founder has to build before they can build what matters.",
    }),
  }),
});

function LandingPage() {
  const location = useLocation();
  const settings = parseGuardianGlowSearch(
    Object.fromEntries(new URLSearchParams(location.searchStr)),
  );

  return <GuardianGlowDemo settings={settings} />;
}
