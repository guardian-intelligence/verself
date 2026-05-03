import { createFileRoute } from "@tanstack/react-router";
import { useRef } from "react";
import { FilmGrain } from "~/components/film-grain";
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
  const trailTargetRef = useRef<HTMLDivElement>(null);
  const wingsAnchorRef = useRef<HTMLDivElement>(null);

  return (
    <section className="relative isolate min-h-[calc(100svh-var(--header-h))] overflow-hidden">
      <div className="absolute inset-0 bg-[var(--treatment-ground)]" />
      <FilmGrain intensity={0.22} />
      <FirstLight trailTargetRef={trailTargetRef} wingsAnchorRef={wingsAnchorRef} />
      <div
        ref={trailTargetRef}
        aria-hidden="true"
        className="pointer-events-none absolute left-[18%] top-[58%] h-[12%] w-[42%] opacity-0"
      />
      <div
        ref={wingsAnchorRef}
        aria-hidden="true"
        className="pointer-events-none absolute left-[14%] top-[36%] h-[17%] w-[18%] opacity-0"
      />
    </section>
  );
}
