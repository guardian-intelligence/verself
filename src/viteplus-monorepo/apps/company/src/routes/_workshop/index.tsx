import { createFileRoute } from "@tanstack/react-router";
import { useMemo, useRef } from "react";
import { WingsArgent } from "@verself/brand";
import { FilmGrain } from "~/components/film-grain";
import { RevealSpan } from "~/components/reveal-span";
import { landing } from "~/content/landing";
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

const FIRST_LIGHT_TRAIL_WORD = "succeed";

function LandingPage() {
  return <LandingHero />;
}

function LandingHero() {
  const trailTargetRef = useRef<HTMLSpanElement>(null);
  const wingsAnchorRef = useRef<HTMLDivElement>(null);
  const heroCopy = useMemo(() => splitTrailWord(landing.hero, FIRST_LIGHT_TRAIL_WORD), []);

  return (
    <section className="relative isolate min-h-[calc(100svh-var(--header-h))] overflow-hidden">
      <div className="absolute inset-0 bg-[var(--treatment-ground)]" />
      <FilmGrain intensity={0.22} />
      <FirstLight trailTargetRef={trailTargetRef} wingsAnchorRef={wingsAnchorRef} />

      <RevealSpan
        spanName="company.landing.hero_view"
        attrs={{ "hero.variant": "first-light" }}
        className="relative z-10 mx-auto flex min-h-[calc(100svh-var(--header-h))] w-full max-w-6xl flex-col justify-center px-4 pb-20 pt-12 md:px-6 md:pb-24 md:pt-16"
      >
        <div ref={wingsAnchorRef} className="mb-10 w-fit md:mb-12">
          <WingsArgent
            viewBoxMode="cropped"
            style={{
              width: "clamp(96px, 13vw, 154px)",
              height: "auto",
              display: "block",
              filter: "brightness(1.04)",
            }}
          />
        </div>

        <p
          className="font-mono text-[11px] font-medium uppercase tracking-[0.16em]"
          style={{ color: "var(--treatment-muted-faint)" }}
        >
          {landing.kicker}
        </p>

        <h1 className="firstlight-headline mt-5" aria-label={landing.hero}>
          {heroCopy.before}
          <span ref={trailTargetRef} className="firstlight-target">
            {heroCopy.word}
          </span>
          {heroCopy.after}
        </h1>
      </RevealSpan>
    </section>
  );
}

interface HeroCopyParts {
  readonly before: string;
  readonly word: string;
  readonly after: string;
}

function splitTrailWord(copy: string, word: string): HeroCopyParts {
  const index = copy.indexOf(word);
  if (index < 0) {
    throw new Error(`Landing hero copy must contain First Light trail word "${word}".`);
  }
  return {
    before: copy.slice(0, index),
    word,
    after: copy.slice(index + word.length),
  };
}
