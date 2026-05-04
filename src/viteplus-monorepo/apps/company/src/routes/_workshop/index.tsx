import { createFileRoute } from "@tanstack/react-router";
import { FirstLight } from "~/features/first-light";
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
  return <LandingHero />;
}

function LandingHero() {
  return (
    <section className="relative isolate min-h-[calc(100svh-var(--header-h))] overflow-hidden">
      <div className="absolute inset-0 bg-[var(--treatment-ground)]" />
      <FirstLight />
    </section>
  );
}
