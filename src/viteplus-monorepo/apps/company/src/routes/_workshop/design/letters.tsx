import { createFileRoute } from "@tanstack/react-router";
import { useEffect } from "react";
import { emitSpan } from "~/lib/telemetry/browser";
import { SectionLetters } from "~/features/design/sections/letters";
import { AppliedFooter } from "~/features/design/sections/applied-footer";
import { ogMeta } from "~/lib/head";

// /design/letters — the Letters specimen, rendered inside Workshop chrome.
// The SectionLetters component wraps its body in data-treatment="letters"
// so the editorial register (Paper ground, Fraunces body, Vellum colophon)
// applies to the specimen without flipping the page chrome.
//
// The Applied footer is cross-treatment teaching material — it renders on
// the inherited Workshop (iron) chrome below the Letters section.

export const Route = createFileRoute("/_workshop/design/letters")({
  component: DesignLetters,
  head: () => ({
    meta: ogMeta({
      slug: "design",
      title: "Letters — Guardian brand system",
      description:
        "The Letters treatment: Paper ground, Fraunces body, Bordeaux as the editorial accent. Where individual voices inside Guardian show their work.",
    }),
  }),
});

function DesignLetters() {
  useEffect(() => {
    if (typeof window === "undefined") return;
    emitSpan("design.treatment.view", {
      treatment: "letters",
      referrer_route: document.referrer ?? "",
    });
  }, []);

  return (
    <>
      <SectionLetters />
      <div className="mx-auto w-full max-w-[96rem] px-4 py-10 md:px-6 md:py-14">
        <AppliedFooter />
      </div>
    </>
  );
}
