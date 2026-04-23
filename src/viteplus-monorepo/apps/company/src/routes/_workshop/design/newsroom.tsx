import { createFileRoute } from "@tanstack/react-router";
import { useEffect } from "react";
import { emitSpan } from "~/lib/telemetry/browser";
import { SectionNewsroom } from "~/features/design/sections/newsroom";
import { AppliedFooter } from "~/features/design/sections/applied-footer";
import { ogMeta } from "~/lib/head";

// /design/newsroom — the Newsroom specimen, rendered inside Workshop chrome.
// The SectionNewsroom component handles its own internal scoping: a Paper
// body with a bounded Flare hero band, plus OG-card and billboard specimens
// in full Flare canvases. See features/design/sections/newsroom.tsx for the
// band-on-Paper composition.
//
// The Applied footer is cross-treatment teaching material — it renders on
// the inherited Workshop (iron) chrome below the Newsroom section.

export const Route = createFileRoute("/_workshop/design/newsroom")({
  component: DesignNewsroom,
  head: () => ({
    meta: ogMeta({
      slug: "design",
      title: "Newsroom — Guardian brand system",
      description:
        "The Newsroom treatment: Flare ground, wings inside a circular emboss, Fraunces at display weight. How Guardian appears in someone else's feed.",
    }),
  }),
});

function DesignNewsroom() {
  useEffect(() => {
    if (typeof window === "undefined") return;
    emitSpan("design.treatment.view", {
      treatment: "newsroom",
      referrer_route: document.referrer ?? "",
    });
  }, []);

  return (
    <>
      <SectionNewsroom />
      <div className="mx-auto w-full max-w-[96rem] px-4 py-10 md:px-6 md:py-14">
        <AppliedFooter />
      </div>
    </>
  );
}
